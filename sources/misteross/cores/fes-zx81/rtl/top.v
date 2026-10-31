// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
`ifndef FES_ZX81_BUILD_ID
`define FES_ZX81_BUILD_ID 128'h00000000000000000000000000000000
`endif

// Quartus DE10-Nano shell: 52.224 MHz ZX81, 12.288 MHz audio, 74.25 MHz HDMI.
// BUILD_ID is overridden from the canonical build-input record.
module top #(
    parameter [127:0] BUILD_ID = `FES_ZX81_BUILD_ID,
    parameter EXPANSION_SOCKET = 0
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA,
    output wire        HDMI_I2S0,
    output wire        HDMI_MCLK,
    output wire        HDMI_LRCLK,
    output wire        HDMI_SCLK
);
    wire clk_sys;
    wire pixel_clk;
    wire pixel_locked;
    wire audio_clk;
    wire audio_locked;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire exec_reset;
    wire [39:0] keyboard;
    wire tape_ready;
    wire tape_busy;
    wire [14:0] tape_size;
    wire [13:0] tape_addr;
    wire [7:0] tape_data;
    wire save_ready, save_busy, save_changed;
    wire [14:0] save_size;
    wire [7:0] save_q;
    wire media_read_req;
    wire [19:0] media_read_addr;
    reg save_read_ready;
    always @(posedge clk_sys) save_read_ready <= media_read_req;
    wire ce_6m5;
    wire video_pixel;
    wire hblank;
    wire vblank;
    wire [7:0] red;
    wire [7:0] green;
    wire [7:0] blue;
    wire [143:0] keyboard_rows;
    wire [13:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable;
    wire [1:0] unit0_state;
    wire [31:0] unit0_size;
    wire machine_de, machine_hs, machine_vs;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    wire hdmi_scl_low;
    wire hdmi_sda_low;

    // HPS I2C to ADV7513 at X52/Y60. Quartus uses assign-to-Z; OSS uses
    // MISTRAL_IO open-drain pads like FES Pong.
`ifdef QUARTUS
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .out_clk(hdmi_scl_low),
        .scl(HDMI_I2C_SCL),
        .out_data(hdmi_sda_low),
        .sda(HDMI_I2C_SDA)
    );
    assign HDMI_I2C_SCL = hdmi_scl_low ? 1'b0 : 1'bz;
    assign HDMI_I2C_SDA = hdmi_sda_low ? 1'b0 : 1'bz;
`else
    wire hdmi_scl_in;
    wire hdmi_sda_in;
    MISTRAL_IO hdmi_scl_pad (
        .I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL)
    );
    MISTRAL_IO hdmi_sda_pad (
        .I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA)
    );
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in),
        .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );
`endif

    sys_pll system_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(clk_sys), .audio_clk(audio_clk), .locked(audio_locked)
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk), .locked(pixel_locked)
    );

    wire display_request, display_response_valid, display_response_error, display_quiesced;
    wire [6:0] display_opcode;
    wire [7:0] display_index;
    wire [15:0] display_argument, display_response_data;
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1),
        .ENABLE_ZX81_TAPE(1),
        // ZX80's cassette entry is the ZX81 SAVE completion address.
`ifdef ZX81_MODEL_ZX80
        .ENABLE_ZX81_TAPE_WRITE(0),
`else
        .ENABLE_ZX81_TAPE_WRITE(1),
`endif
        .ENABLE_SESSION_DISPLAY(1), .MEDIA_AW(14),
        .UNIT0_MIN(`FES_COMPUTER_ZX81_TAPE_MIN_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_ZX81_TAPE_MAX_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_busy(tape_busy), .media_changed(save_changed), .media_frozen(),
        .media_read_req(media_read_req), .media_read_addr(media_read_addr),
        .media_read_ready(save_read_ready), .media_read_data(save_q),
        .save_ready(save_ready), .save_busy(save_busy), .save_size(save_size),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_ready(1'b1),
        .unit0_state(unit0_state), .unit0_size(unit0_size),
        .media_write_unit(), .unit1_state(), .unit1_size(),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid),
        .display_response_error(display_response_error),
        .display_response_data(display_response_data),
        .display_quiesced(display_quiesced),
        .storage_request(), .storage_opcode(), .storage_index(), .storage_argument(),
        .storage_response_valid(1'b0), .storage_response_error(1'b0), .storage_response_data(16'd0),
        .execution_accepted()
    );
    zx81_keyboard keyboard_map (
        .clk(clk_sys), .reset(exec_reset), .rows(keyboard_rows), .matrix(keyboard)
    );
    zx81_tape_store tape (
        .clk(clk_sys), .unit_state(unit0_state), .unit_size(unit0_size),
        .write_addr(media_write_addr), .write_data(media_write_data),
        .write_enable(media_write_enable), .tape_addr(tape_addr),
        .tape_ready(tape_ready), .tape_size(tape_size), .tape_data(tape_data)
    );

    /* verilator lint_off PINCONNECTEMPTY */
    wire [13:0] ram_address;
    wire [7:0] ram_write_data;
    wire ram_write_enable;
    wire [15:0] bus_addr;
    wire [7:0] bus_wdata, bus_rdata, bus_peek_data;
    wire bus_mreq_n, bus_iorq_n, bus_rd_n, bus_wr_n, bus_m1_n, bus_rfsh_n;
    wire bus_cpu_clock;
    wire bus_dsel, bus_romcs, bus_wait, bus_ram_present;
    (* keep *) wire [45:0] plug_addr;
    wire [19:0] plug_rdata;
    generate if (EXPANSION_SOCKET) begin : expansion
        zx81_expansion_socket socket (
            .clock(clk_sys),
            .cpu_clock(bus_cpu_clock), .cpu_reset_n(~exec_reset),
            .cpu_addr(bus_addr),
            .cpu_wdata(bus_wdata),
            .cpu_mreq_n(bus_mreq_n),
            .cpu_iorq_n(bus_iorq_n),
            .cpu_rd_n(bus_rd_n),
            .cpu_wr_n(bus_wr_n),
            .cpu_m1_n(bus_m1_n),
            .cpu_rfsh_n(bus_rfsh_n),
            .peek_address(14'b0),
            .bus_rdata(bus_rdata),
            .bus_peek_data(bus_peek_data),
            .bus_dsel(bus_dsel),
            .bus_romcs(bus_romcs),
            .bus_wait(bus_wait),
            .bus_ram_present(bus_ram_present),
            .plug_addr(plug_addr),
            .plug_rdata_in(20'b0),
            .plug_rdata(plug_rdata)
        );
    end else begin : fixed_memory
        assign bus_rdata = 8'b0;
        assign bus_peek_data = 8'b0;
        assign bus_dsel = 1'b0;
        assign bus_romcs = 1'b0;
        assign bus_wait = 1'b0;
        assign bus_ram_present = 1'b0;
        assign plug_addr = 46'b0;
        assign plug_rdata = 20'b0;
    end endgenerate
    zx81_machine #(.EXTERNAL_RAM(EXPANSION_SOCKET)) machine (
        .clk_sys(clk_sys),
        .reset(exec_reset),
        .keyboard(keyboard),
        .tape_ready(tape_ready),
        .tape_size(tape_size),
        .tape_data(tape_data),
        .tape_addr_out(tape_addr),
        .tape_busy(tape_busy),
        .save_ready(save_ready), .save_busy(save_busy), .save_changed(save_changed),
        .save_size(save_size), .save_addr(media_read_addr[13:0]), .save_q(save_q),
        .ce_6m5(ce_6m5),
        .video_pixel(video_pixel),
        .hblank(hblank),
        .vblank(vblank),
        .hsync_out(),
        .vsync_out(),
        .halt_n(),
        .cpu_addr(),
        .peek_addr(16'h0000),
        .peek_data(),
        .ram_address(ram_address), .ram_write_data(ram_write_data),
        .ram_write_enable(ram_write_enable),
        .external_ram_data(8'b0), .external_peek_data(8'b0),
        .bus_addr(bus_addr), .bus_wdata(bus_wdata),
        .bus_mreq_n(bus_mreq_n), .bus_iorq_n(bus_iorq_n),
        .bus_rd_n(bus_rd_n), .bus_wr_n(bus_wr_n),
        .bus_m1_n(bus_m1_n), .bus_rfsh_n(bus_rfsh_n),
        .bus_cpu_clock(bus_cpu_clock),
        .bus_rdata(bus_rdata), .bus_peek_data(bus_peek_data),
        .bus_dsel(bus_dsel), .bus_romcs(bus_romcs),
        .bus_wait(bus_wait), .bus_ram_present(bus_ram_present)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    wire signed [15:0] psg_sample = bus_ram_present ? 16'sd0
        : $signed({1'b0, bus_peek_data, 7'b0});
    fes_audio_output audio (
        .source_clk(clk_sys), .audio_clk(audio_clk), .locked(audio_locked),
        .hold(exec_reset), .left_sample(psg_sample), .right_sample(psg_sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S0)
    );
    assign HDMI_MCLK = audio_clk;

    zx81_video_720p video (
        .clk_sys(clk_sys),
        .ce_6m5(ce_6m5),
        .zx_pixel(video_pixel),
        .hblank(hblank),
        .vblank(vblank),
        .pixel_clk(pixel_clk),
        .red(red),
        .green(green),
        .blue(blue),
        .de(machine_de),
        .hsync(machine_hs),
        .vsync(machine_vs),
        .frame_tick(), .raster_h(), .raster_v(),
        .src_x_max(),
        .src_y_max()
    );

    wire [27:0] hdmi_video;
    wire ddr_hold, ddr0_reset, ddr0_drained, ddr0_read, ddr0_waitrequest, ddr0_readdatavalid;
    wire [27:0] ddr0_address;
    wire [7:0] ddr0_burstcount;
    wire [127:0] ddr0_readdata;
    // Shipped HDMI contract: the shared fes.video.session-display plane,
    // backed by fes.memory.hps-ddr. It blends launcher frames over the machine
    // word and delays RGB and sync together. A closed plane returns machine
    // pixels on the next frame boundary and does not reprogram the core.
    fes_display_plane #(
        .PICTURE_LEFT(16'd312), .PICTURE_TOP(16'd90),
        .PICTURE_WIDTH(16'd656), .PICTURE_HEIGHT(16'd540)
    ) display (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(~pixel_locked),
        .exec_reset(exec_reset), .request(display_request), .opcode(display_opcode),
        .index(display_index), .argument(display_argument),
        .response_valid(display_response_valid), .response_error(display_response_error),
        .response_data(display_response_data), .quiesced_sys(display_quiesced),
        .machine({1'b1, machine_vs, machine_hs, machine_de, red, green, blue}),
        .video(hdmi_video), .ui_active(), .locked(), .displayed_sequence(), .underflows(), .faulted(),
        .ddr_hold(ddr_hold), .ddr_reset(ddr0_reset), .ddr_drained(ddr0_drained),
        .ddr_address(ddr0_address), .ddr_burstcount(ddr0_burstcount), .ddr_read(ddr0_read),
        .ddr_waitrequest(ddr0_waitrequest), .ddr_readdata(ddr0_readdata),
        .ddr_readdatavalid(ddr0_readdatavalid)
    );
    // The plane is the only DDR client: port 0 reads, ports 1 and 2 are off.
    fes_hps_ddr #(.P0_WRITE_ENABLE(1'b0), .P1_ENABLE(1'b0), .P2_ENABLE(1'b0)) memory (
        .hold(ddr_hold),
        .p0_clk(pixel_clk), .p0_reset(ddr0_reset), .p0_drained(ddr0_drained),
        .p0_address(ddr0_address), .p0_burstcount(ddr0_burstcount),
        .p0_waitrequest(ddr0_waitrequest), .p0_readdata(ddr0_readdata),
        .p0_readdatavalid(ddr0_readdatavalid), .p0_read(ddr0_read),
        .p0_writedata(128'd0), .p0_byteenable(16'd0), .p0_write(1'b0),
        .p1_clk(pixel_clk), .p1_reset(), .p1_drained(), .p1_address(29'd0), .p1_burstcount(8'd0),
        .p1_waitrequest(), .p1_readdata(), .p1_readdatavalid(), .p1_read(1'b0),
        .p1_writedata(64'd0), .p1_byteenable(8'd0), .p1_write(1'b0),
        .p2_clk(pixel_clk), .p2_reset(), .p2_drained(), .p2_address(29'd0), .p2_burstcount(8'd0),
        .p2_waitrequest(), .p2_readdata(), .p2_readdatavalid(), .p2_read(1'b0),
        .p2_writedata(64'd0), .p2_byteenable(8'd0), .p2_write(1'b0)
    );
    assign HDMI_TX_D = hdmi_video[23:0];
    assign HDMI_TX_DE = hdmi_video[24];
    assign HDMI_TX_HS = hdmi_video[25];
    assign HDMI_TX_VS = hdmi_video[26];
    assign HDMI_TX_CLK = pixel_clk;
endmodule
