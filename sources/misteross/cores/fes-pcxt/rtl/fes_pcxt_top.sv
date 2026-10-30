// SPDX-License-Identifier: GPL-2.0-or-later
// The default profile is autonomous. The package producer enables the minimal
// runtime mailbox so the target can start the built-in diagnostic ROM.
module top #(
    parameter bit MDA = 0, SCANLINES = 0, HALF_RATE_BOARD = 0,
    parameter bit HARDWARE_DOS = 0,
    parameter bit PHASED_HDMI_CLOCK = 0,
    parameter bit ENABLE_FES_COMPUTER = 0,
    parameter bit ENABLE_PCXT_MEDIA = 0,
    parameter [127:0] BUILD_ID = 128'h00000000000000000000000000000000
) (
    input wire FPGA_CLK1_50,
    output wire HDMI_TX_CLK, HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    inout wire HDMI_I2C_SCL, HDMI_I2C_SDA,
    output wire SDRAM_CLK, SDRAM_CKE, SDRAM_nCS, SDRAM_nRAS, SDRAM_nCAS, SDRAM_nWE,
    output wire SDRAM_DQML, SDRAM_DQMH,
    output wire [1:0] SDRAM_BA,
    output wire [12:0] SDRAM_A,
    inout wire [15:0] SDRAM_DQ
);
    wire clk, clk_memory, clk_capture, clk_pixel, clk_hdmi, memory_locked, video_locked;
    // Rate-3 consumes the falling-edge sample. A 180 degree fabric capture
    // clock provides that edge without unsupported DDIO on bidirectional IO.
    wire [(HALF_RATE_BOARD ? 4 : 2)-1:0] clocks;
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"), .number_of_clocks(HALF_RATE_BOARD ? 4 : 2),
        .output_clock_frequency0("100.0 MHz"), .output_clock_frequency1(HALF_RATE_BOARD ? "50.0 MHz" : "100.0 MHz"),
        .output_clock_frequency2(HALF_RATE_BOARD ? "50.0 MHz" : "0 MHz"),
        .output_clock_frequency3(HALF_RATE_BOARD ? "50.0 MHz" : "0 MHz"),
        .phase_shift0("0 ps"), .phase_shift1(HALF_RATE_BOARD ? "10000 ps" : "5000 ps"),
        .phase_shift2("0 ps"), .phase_shift3(HALF_RATE_BOARD ? "18000 ps" : "0 ps"),
        .duty_cycle0(50), .duty_cycle1(50), .duty_cycle2(50), .duty_cycle3(50), .operation_mode("direct"),
        .fractional_vco_multiplier("false")
    ) memory_clock (.refclk(FPGA_CLK1_50), .rst(1'b0), .outclk(clocks), .locked(memory_locked));
    assign clk = clocks[0];
    assign clk_memory = HALF_RATE_BOARD ? clocks[2] : clk;
    assign clk_capture = clocks[1];
    localparam bit USE_PHASED_HDMI_CLOCK = HALF_RATE_BOARD || PHASED_HDMI_CLOCK;
    wire [(USE_PHASED_HDMI_CLOCK ? 2 : 1)-1:0] video_clocks;
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"), .number_of_clocks(USE_PHASED_HDMI_CLOCK ? 2 : 1),
        .output_clock_frequency0("74.25 MHz"), .output_clock_frequency1(USE_PHASED_HDMI_CLOCK ? "74.25 MHz" : "0 MHz"),
        .phase_shift0("0 ps"), .phase_shift1(USE_PHASED_HDMI_CLOCK ? "8979 ps" : "0 ps"), .duty_cycle0(50), .duty_cycle1(50),
        .operation_mode("direct"), .fractional_vco_multiplier("true")
    ) video_clock (.refclk(FPGA_CLK1_50), .rst(1'b0), .outclk(video_clocks), .locked(video_locked));
    assign clk_pixel = video_clocks[0];
    // The forwarded edge is 8.979 ns after a phase-zero pixel update and
    // 4.489 ns before the next one. Pixel state stays on phase 0.
    generate if (USE_PHASED_HDMI_CLOCK) begin: phased_hdmi_clock
        MISTRAL_CLKBUF clock_buffer (.A(video_clocks[1]), .Q(clk_hdmi));
    end else begin: common_hdmi_clock
        assign clk_hdmi = clk_pixel;
    end endgenerate
    wire locked = memory_locked && video_locked;
    (* async_reg = "true" *) reg [1:0] sys_reset = 2'b11, pixel_reset = 2'b11;
    always @(posedge clk or negedge locked)
        if (!locked) sys_reset <= 2'b11;
        else sys_reset <= {sys_reset[0], 1'b0};
    always @(posedge clk_pixel or negedge locked)
        if (!locked) pixel_reset <= 2'b11;
        else pixel_reset <= {pixel_reset[0], 1'b0};
    wire board_reset_live;
    wire controller_cs;
    generate if (HALF_RATE_BOARD) begin: early_chip_select
        // Source CS is ready a board cycle before the command pads. Capture
        // it 2ns before their final stage, giving the addon inverter headroom.
        reg cs;
        always @(posedge clocks[3]) cs <= controller_cs;
        assign SDRAM_nCS = cs;
    end else begin: common_chip_select
        assign SDRAM_nCS = controller_cs;
    end endgenerate
    generate if (HALF_RATE_BOARD) begin: board_reset_domain
        (* async_reg = "true" *) reg [1:0] board_reset = 2'b11;
        always @(posedge clk_memory or negedge locked)
            if (!locked) board_reset <= 2'b11;
            else board_reset <= {board_reset[0], 1'b0};
        assign board_reset_live = board_reset[1];
    end else begin: common_reset_domain
        assign board_reset_live = sys_reset[1];
    end endgenerate
    wire exec_reset;
    localparam bit LIVE_MEDIA = ENABLE_FES_COMPUTER && ENABLE_PCXT_MEDIA && !HARDWARE_DOS;
    wire media_present, disk_restart, disk_load_valid, disk_load_ready, disk_load_done, disk_loaded;
    wire [18:0] disk_load_address;
    wire [7:0] disk_load_data;
    generate if (ENABLE_FES_COMPUTER) begin: fes_computer_runtime
        wire [31:0] fpga_to_hps, hps_to_fpga;
        cyclonev_hps_interface_mpu_general_purpose hps_gp (
            .gp_in(fpga_to_hps), .gp_out(hps_to_fpga));
        if (LIVE_MEDIA) begin: pcxt_floppy
            wire [18:0] source_addr;
            wire [15:0] source_data;
            wire [1:0] source_enable, unit_state;
            wire source_ready;
            fes_computer_mailbox_pcxt gp_mailbox (
                .clk(clk_memory), .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
                .exec_reset(exec_reset), .media_reset(board_reset_live),
                .media_write_addr(source_addr), .media_write_data(source_data),
                .media_write_enable(source_enable), .media_write_ready(source_ready),
                .media_write_busy(1'b0), .unit0_state(unit_state), .unit0_size());
            pcxt_media media (
                .clk(clk_memory), .cold_reset(board_reset_live), .unit_state(unit_state),
                .source_addr(source_addr), .source_data(source_data), .source_enable(source_enable),
                .source_ready(source_ready), .disk_restart(disk_restart), .media_present(media_present),
                .disk_loaded(disk_loaded), .disk_valid(disk_load_valid), .disk_address(disk_load_address),
                .disk_data(disk_load_data), .disk_ready(disk_load_ready), .disk_done(disk_load_done));
        end else begin: diagnostic_mailbox
            fes_computer_mailbox gp_mailbox (
                .clk(clk), .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
                .exec_reset(exec_reset), .keyboard_rows(), .controller_buttons(),
                .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
                .media_write_addr(), .media_write_data(), .media_write_enable(),
                .media_write_ready(1'b1), .media_write_busy(1'b0), .media_changed(1'b0),
                .media_frozen(), .media_read_req(), .media_read_addr(),
                .media_read_ready(1'b0), .media_read_data(8'd0),
                .unit0_state(), .unit0_size());
        end
    end else begin: standalone_diagnostic
        assign exec_reset = 1'b0;
    end endgenerate
    wire firmware_valid, firmware_ready, firmware_done, firmware_loaded;
    wire [15:0] firmware_address;
    wire [7:0] firmware_data;
    pcxt_boot #(.ROM_PREFIX(HARDWARE_DOS ? "pcxt_glabios" : "pcxt_diagnostic")) boot (
        .clk(clk_memory), .cold_reset(board_reset_live), .enable(!exec_reset),
        .valid(firmware_valid), .ready(firmware_ready), .done(firmware_done),
        .address(firmware_address), .data(firmware_data));
    generate if (HARDWARE_DOS) begin: dos_disk_boot
        assign media_present = 1'b1;
        assign disk_restart = 1'b0;
        pcxt_disk_boot disk_boot (
            .clk(clk_memory), .cold_reset(board_reset_live), .enable(firmware_loaded),
            .valid(disk_load_valid), .ready(disk_load_ready), .done(disk_load_done),
            .address(disk_load_address), .data(disk_load_data), .finished());
    end else if (!LIVE_MEDIA) begin: no_disk_boot
        assign media_present = 1'b0;
        assign disk_restart = 1'b0;
        assign disk_load_valid = 1'b0;
        assign disk_load_address = 19'b0;
        assign disk_load_data = 8'b0;
    end endgenerate
    wire [31:0] video_request;
    wire [27:0] video_response;
    wire [15:0] dq_out, dq_sample;
    wire dq_oe;
    /* verilator lint_off PINCONNECTEMPTY */
    wire [11:0] cga_font_address, mda_font_address;
    wire [7:0] cga_font_data, mda_font_data;
    wire machine_reset, keyboard_clock, keyboard_data;
    wire keyboard_clock_low, keyboard_data_low, keyboard_clear;
    generate if (HARDWARE_DOS) begin: dos_keyboard
        pcxt_xt_keyboard_bat keyboard (
            .clk(clk_memory), .reset(machine_reset),
            .clock_low(keyboard_clock_low), .data_low(keyboard_data_low),
            .keyboard_clear(keyboard_clear),
            .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data)
        );
    end else begin: no_keyboard
        assign keyboard_clock = 1'b1;
        assign keyboard_data = 1'b0;
        wire unused_keyboard = &{1'b0, machine_reset, keyboard_clock_low, keyboard_data_low, keyboard_clear};
    end endgenerate
    generate if (HARDWARE_DOS) begin: dos_font
        reg [7:0] glyphs [0:4095];
        initial $readmemh("pcxt_glabios_font.hex", glyphs);
        assign cga_font_data = glyphs[cga_font_address];
        assign mda_font_data = glyphs[mda_font_address];
    end else begin: diagnostic_font
        assign cga_font_data = 8'h81;
        assign mda_font_data = 8'h81;
    end endgenerate
    pcxt_system #(.MDA(MDA), .SCANLINES(SCANLINES), .HALF_RATE_BOARD(HALF_RATE_BOARD),
                  .EXTERNAL_CS_REGISTER(HALF_RATE_BOARD), .FLOPPY(HARDWARE_DOS || LIVE_MEDIA),
                  .DIAGNOSTIC_PROBE(!(HARDWARE_DOS || LIVE_MEDIA))) system (
        .media_present(media_present), .disk_restart(disk_restart), .disk_load_valid(disk_load_valid), .disk_load_address(disk_load_address),
        .disk_load_data(disk_load_data), .disk_load_ready(disk_load_ready), .disk_load_done(disk_load_done),
        .disk_loaded(disk_loaded),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data),
        .keyboard_clock_low(keyboard_clock_low), .keyboard_data_low(keyboard_data_low), .keyboard_clear(keyboard_clear),
        .cga_font_address(cga_font_address), .mda_font_address(mda_font_address),
        .cga_font_data(cga_font_data), .mda_font_data(mda_font_data),
        .clk(clk_memory), .clk_cpu(clk), .clk_pixel(clk_pixel), .clk_pin(clk_memory), .cold_reset(board_reset_live),
        .reset_pixel(pixel_reset[1]), .warm_reset(exec_reset), .hold(HARDWARE_DOS && !disk_loaded),
        .firmware_valid(firmware_valid), .firmware_ready(firmware_ready),
        .firmware_done(firmware_done), .firmware_address(firmware_address), .firmware_data(firmware_data),
        .firmware_loaded(firmware_loaded), .initialized(), .machine_reset(machine_reset), .fault(), .halted(),
        .dma_owner(), .refresh_done(), .audio_valid(), .audio_sample(),
        .video_request(video_request), .video_response(video_response),
        .sdram_clk(SDRAM_CLK), .sdram_cke(SDRAM_CKE), .sdram_ncs(controller_cs),
        .sdram_nras(SDRAM_nRAS), .sdram_ncas(SDRAM_nCAS), .sdram_nwe(SDRAM_nWE),
        .sdram_ba(SDRAM_BA), .sdram_a(SDRAM_A), .sdram_dqml(SDRAM_DQML), .sdram_dqmh(SDRAM_DQMH),
        .dq_out(dq_out), .dq_oe(dq_oe), .dq_rise(dq_sample), .dq_fall(dq_sample)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    generate for (genvar n=0; n<16; n=n+1) begin: sdram_pads
        wire pin_value;
        altiobuf_bidir #(.number_of_channels(1), .enable_bus_hold("OFF")) pad (
            .dataio(SDRAM_DQ[n]), .oe(dq_oe), .datain(dq_out[n]), .dataout(pin_value));
        reg captured;
        always @(posedge clk_capture) captured <= pin_value;
        assign dq_sample[n] = captured;
    end endgenerate
    wire scl_low, sda_low, scl_in, sda_in;
    MISTRAL_IO scl (.I(1'b0), .OE(scl_low), .O(scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO sda (.I(1'b0), .OE(sda_low), .O(sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(scl_in), .sda(sda_in), .out_clk(scl_low), .out_data(sda_low));
    assign HDMI_TX_CLK = clk_hdmi;
    assign HDMI_TX_D = video_response[23:0];
    assign HDMI_TX_DE = video_response[24];
    assign HDMI_TX_HS = video_response[25];
    assign HDMI_TX_VS = video_response[26];
    wire unused_video = ^{video_request, video_response[27]};
endmodule
