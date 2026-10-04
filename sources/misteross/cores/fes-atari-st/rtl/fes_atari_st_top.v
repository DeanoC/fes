// SPDX-License-Identifier: GPL-3.0-or-later
`include "fes_computer.vh"
// Native ST shell: one system/SDRAM domain, coherent pixel adapter, shared
// audio output, linked firmware and an independently replaceable expansion.
module top #(
    parameter [127:0] BUILD_ID = 128'd0,
    parameter integer VIDEO_SCANLINES = 0
) (
    input wire FPGA_CLK1_50,
    output wire HDMI_TX_CLK, HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire HDMI_TX_HS, HDMI_TX_VS,
    inout wire HDMI_I2C_SCL, HDMI_I2C_SDA,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S,
    output wire SDRAM_CLK, SDRAM_CKE, SDRAM_nCS, SDRAM_nRAS, SDRAM_nCAS, SDRAM_nWE,
    output wire SDRAM_DQML, SDRAM_DQMH,
    output wire [1:0] SDRAM_BA,
    output wire [12:0] SDRAM_A,
    inout wire [15:0] SDRAM_DQ
);
    wire clk_sys, pixel_clk, audio_clk, system_locked, pixel_locked;
    c64_system_pll system_clock (
        .refclk(FPGA_CLK1_50), .rst(1'b0), .outclk_0(clk_sys),
        .audio_clk(audio_clk), .locked(system_locked)
    );
    // Expose the pixel PLL lock so both domains receive coordinated cold
    // resets. Warm host Hold never resets the SDRAM controller or refresh.
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"), .number_of_clocks(1),
        .output_clock_frequency0("74.25 MHz"), .phase_shift0("0 ps"),
        .duty_cycle0(50), .operation_mode("direct"), .fractional_vco_multiplier("true")
    ) video_clock (.refclk(FPGA_CLK1_50), .rst(1'b0), .outclk(pixel_clk), .locked(pixel_locked));
    wire clocks_locked = system_locked && pixel_locked;
    reg [1:0] system_reset_sync = 2'b11, pixel_reset_sync = 2'b11;
    always @(posedge clk_sys or negedge clocks_locked)
        if (!clocks_locked) system_reset_sync <= 2'b11;
        else system_reset_sync <= {system_reset_sync[0],1'b0};
    always @(posedge pixel_clk or negedge clocks_locked)
        if (!clocks_locked) pixel_reset_sync <= 2'b11;
        else pixel_reset_sync <= {pixel_reset_sync[0],1'b0};
    wire cold_reset = system_reset_sync[1];
    wire pixel_reset = pixel_reset_sync[1];

    wire [31:0] fpga_to_hps, hps_to_fpga;
    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps), .gp_out(hps_to_fpga)
    );
    wire hdmi_scl_low, hdmi_sda_low, hdmi_scl_in, hdmi_sda_in;
    MISTRAL_IO hdmi_scl_pad (.I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO hdmi_sda_pad (.I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in), .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );

    wire exec_reset;
    wire [143:0] keyboard;
    wire [15:0] controller_buttons;
    wire mouse_valid, mouse_ready;
    wire signed [15:0] mouse_dx, mouse_dy;
    wire [1:0] mouse_buttons;
    wire [19:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable, unit0_state;
    wire [31:0] unit0_size;
    wire media_source_ready;
    wire media_frozen, floppy_write_busy, floppy_changed;
    wire snapshot_req, snapshot_ready;
    wire [19:0] snapshot_addr;
    wire [7:0] snapshot_data;
    fes_computer_mailbox #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1), .ENABLE_MOUSE(1),
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_ATARI_ST_FLOPPY_WRITE(1), .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(20),
        .UNIT0_MIN(`FES_COMPUTER_ATARI_ST_FLOPPY_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_ATARI_ST_FLOPPY_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
        .exec_reset(exec_reset), .keyboard_rows(keyboard), .controller_buttons(controller_buttons),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_ready(media_source_ready),
        .media_write_busy(floppy_write_busy), .media_changed(floppy_changed), .media_frozen(media_frozen),
        .media_read_req(snapshot_req), .media_read_addr(snapshot_addr),
        .media_read_ready(snapshot_ready), .media_read_data(snapshot_data),
        .unit0_state(unit0_state), .unit0_size(unit0_size)
    );

    wire initialized;
    wire machine_reset = exec_reset || cold_reset || !initialized;
    wire rom_req, rom_ready, ram_req, ram_ready, ram_write;
    wire [17:1] rom_addr;
    wire [18:1] ram_addr;
    wire [15:0] rom_rdata, ram_rdata, ram_wdata;
    wire [1:0] ram_byte_enable;
    wire exp_req, exp_write, exp_reset, exp_phi1, exp_phi2, irq_ack;
    wire [23:1] exp_addr;
    wire [15:0] exp_wdata;
    wire [1:0] exp_byte_enable;
    wire [2:0] exp_fc, irq_level;
    wire [55:0] expansion_request = {3'd0, irq_level, irq_ack, exp_phi2, exp_phi1,
        exp_fc, exp_reset, exp_req, exp_write, exp_byte_enable, exp_wdata, exp_addr};
    wire [31:0] expansion_response;
    (* keep *) wire [55:0] expansion_plug_request;
    st_expansion_socket expansion (
        .clock(clk_sys), .request(expansion_request), .response(expansion_response),
        .plug_request(expansion_plug_request), .plug_response(32'd0)
    );
    wire signed [15:0] audio_pcm;
    wire audio_valid, media_req, media_valid;
    wire [19:0] media_addr;
    wire [7:0] media_data;
    wire dma_req, dma_ready, dma_write;
    wire [15:0] dma_rdata;
    wire floppy_write_req, floppy_write_ready;
    wire [19:1] floppy_write_addr;
    wire [15:0] floppy_write_data;
    wire [23:0] dma_addr;
    wire [15:0] dma_wdata;
    wire [1:0] dma_byte_enable;
    wire [23:0] screen_base;
    wire [1:0] resolution;
    wire [143:0] palette;
    wire [7:0] sync_mode;
    wire [23:0] debug_addr;
    wire debug_bus_error, debug_overlay, debug_halted, vblank, hblank;
    generate begin : machine
        st_rom rom (.clk(clk_sys), .reset(machine_reset), .req(rom_req), .address(rom_addr),
                    .rdata(rom_rdata), .ready(rom_ready));
        // Slang imports this root already specialized with writable media.
        // Its RTLIL module is no longer parametric; other readers elaborate it.
`ifdef FES_ST_SLANG_IMPORT
        st_system system (
`else
        st_system #(.ENABLE_FLOPPY_WRITE(1)) system (
`endif
            .clk_sys(clk_sys), .reset(machine_reset), .cold_reset(cold_reset),
            .rom_req(rom_req), .rom_addr(rom_addr), .rom_rdata(rom_rdata), .rom_ready(rom_ready),
            .ram_req(ram_req), .ram_addr(ram_addr), .ram_wdata(ram_wdata),
            .ram_byte_enable(ram_byte_enable), .ram_write(ram_write), .ram_rdata(ram_rdata), .ram_ready(ram_ready),
            .exp_req(exp_req), .exp_addr(exp_addr), .exp_wdata(exp_wdata), .exp_byte_enable(exp_byte_enable),
            .exp_write(exp_write), .exp_fc(exp_fc), .exp_reset(exp_reset), .exp_phi1(exp_phi1), .exp_phi2(exp_phi2),
            .exp_present(expansion_response[21]), .exp_ack(expansion_response[16]),
            .exp_berr(expansion_response[17]), .exp_rdata(expansion_response[15:0]), .exp_irq(expansion_response[20:18]),
            .irq_ack(irq_ack), .irq_level(irq_level), .keyboard(keyboard), .controller_buttons(controller_buttons),
            .monochrome(1'b0), .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
            .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready), .audio_pcm(audio_pcm), .audio_valid(audio_valid),
            .media_ready(unit0_state == 2'(`FES_COMPUTER_MEDIA_STATE_READY)),
            .media_req(media_req), .media_addr(media_addr), .media_data(media_data), .media_valid(media_valid),
            .media_frozen(media_frozen), .media_write_req(floppy_write_req), .media_write_addr(floppy_write_addr),
            .media_write_data(floppy_write_data), .media_write_ready(floppy_write_ready),
            .media_write_busy(floppy_write_busy), .media_changed(floppy_changed),
            .dma_req(dma_req), .dma_write(dma_write), .dma_rdata(dma_rdata), .dma_addr(dma_addr), .dma_wdata(dma_wdata), .dma_byte_enable(dma_byte_enable),
            .dma_ready(dma_ready), .screen_base(screen_base), .resolution(resolution), .palette(palette),
            .sync_mode(sync_mode), .debug_addr(debug_addr), .debug_bus_error(debug_bus_error),
            .debug_overlay(debug_overlay), .debug_halted(debug_halted), .vblank(vblank), .hblank(hblank)
        );
    end endgenerate

    wire video_req, video_ready;
    wire [18:1] video_addr;
    wire [15:0] video_rdata;
    wire [31:0] video_request, debug_underruns, debug_frame;
    st_video_adapter video (
        .clk_sys(clk_sys), .clk_pixel(pixel_clk), .reset_sys(cold_reset), .reset_pixel(pixel_reset),
        .hold(machine_reset), .screen_base(screen_base), .resolution(resolution), .palette(palette),
        .video_req(video_req), .video_addr(video_addr), .video_ready(video_ready), .video_rdata(video_rdata),
        .video_request(video_request), .debug_underruns(debug_underruns), .debug_frame(debug_frame)
    );
    reg [31:0] video_request_q = 0;
    reg [27:0] video_response_q = 0;
    (* keep *) wire [31:0] video_plug_request;
    wire [27:0] video_response;
    st_video_socket video_socket (
        .clock(pixel_clk), .request(video_request), .response(video_response),
        .plug_request(video_plug_request), .plug_response(28'd0)
    );
    wire [27:0] selected_video_response = video_response[27] ? video_response : video_response_q;
    wire [27:0] video_result;
    generate if (VIDEO_SCANLINES != 0) begin : scanlines
        fes_video_part_scanlines part (.clock(pixel_clk), .video_request(video_request_q), .video_response(video_result));
    end else begin : direct
        fes_video_part_direct part (.video_request(video_request_q), .video_response(video_result));
    end endgenerate
    always @(posedge pixel_clk) begin video_request_q <= video_request; video_response_q <= video_result; end
    assign HDMI_TX_D = selected_video_response[23:0];
    assign HDMI_TX_DE = selected_video_response[24];
    assign HDMI_TX_HS = selected_video_response[25];
    assign HDMI_TX_VS = selected_video_response[26];
    assign HDMI_TX_CLK = pixel_clk;
    fes_audio_output audio (
        .source_clk(clk_sys), .audio_clk(audio_clk), .locked(system_locked), .hold(machine_reset),
        .left_sample(audio_pcm), .right_sample(audio_pcm),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
    assign HDMI_MCLK = audio_clk;

    wire media_write_req, media_memory_ready;
    wire [19:1] media_memory_addr;
    wire [15:0] media_memory_wdata;
    wire [1:0] media_memory_enable;
    st_media_writer writer (
        .clk(clk_sys), .cold_reset(cold_reset), .source_addr(media_write_addr), .source_data(media_write_data),
        .source_enable(media_write_enable), .source_ready(media_source_ready),
        .memory_req(media_write_req), .memory_addr(media_memory_addr), .memory_wdata(media_memory_wdata),
        .memory_byte_enable(media_memory_enable), .memory_ready(media_memory_ready)
    );
    wire shared_write_req, shared_write_ready, shared_read_req, shared_read_ready;
    wire [18:0] shared_write_addr;
    wire [19:0] shared_read_addr;
    wire [15:0] shared_write_data;
    wire [1:0] shared_write_enable, write_ready, read_ready;
    wire [7:0] shared_read_data;
    st_media_port #(.ADDR_BITS(19)) media_writes (
        .clk(clk_sys), .cold_reset(cold_reset), .source_req({floppy_write_req,media_write_req}),
        .source_addr0(media_memory_addr), .source_addr1(floppy_write_addr),
        .source_data0(media_memory_wdata), .source_data1(floppy_write_data),
        .source_enable0(media_memory_enable), .source_enable1(2'b11), .source_ready(write_ready),
        .memory_req(shared_write_req), .memory_addr(shared_write_addr), .memory_data(shared_write_data),
        .memory_enable(shared_write_enable), .memory_ready(shared_write_ready)
    );
    assign media_memory_ready = write_ready[0];
    assign floppy_write_ready = write_ready[1];
    st_media_port #(.ADDR_BITS(20)) media_reads (
        .clk(clk_sys), .cold_reset(cold_reset), .source_req({snapshot_req,media_req}),
        .source_addr0(media_addr), .source_addr1(snapshot_addr),
        .source_data0(16'd0), .source_data1(16'd0),
        .source_enable0(2'd0), .source_enable1(2'd0), .source_ready(read_ready),
        .memory_req(shared_read_req), .memory_addr(shared_read_addr), .memory_data(), .memory_enable(),
        .memory_ready(shared_read_ready)
    );
    assign media_valid = read_ready[0];
    assign snapshot_ready = read_ready[1];
    assign media_data = shared_read_data;
    assign snapshot_data = shared_read_data;
    wire [15:0] dq_out, dq_rise, dq_fall;
    wire dq_oe;
    st_memory memory (
        .clk(clk_sys), .clk_pin(clk_sys), .cold_reset(cold_reset), .reset(1'b0), .initialized(initialized),
        .cpu_req(ram_req), .cpu_addr(ram_addr), .cpu_write(ram_write), .cpu_wdata(ram_wdata),
        .cpu_byte_enable(ram_byte_enable), .cpu_ready(ram_ready), .cpu_rdata(ram_rdata),
        .video_req(video_req), .video_addr(video_addr), .video_ready(video_ready), .video_rdata(video_rdata),
        .dma_req(dma_req), .dma_addr(dma_addr), .dma_write(dma_write), .dma_wdata(dma_wdata),
        .dma_byte_enable(dma_byte_enable), .dma_ready(dma_ready), .dma_rdata(dma_rdata),
        .media_write_req(shared_write_req), .media_write_addr(shared_write_addr), .media_write_wdata(shared_write_data),
        .media_write_byte_enable(shared_write_enable), .media_write_ready(shared_write_ready),
        .media_read_req(shared_read_req), .media_read_addr(shared_read_addr), .media_read_ready(shared_read_ready), .media_read_rdata(shared_read_data),
        .sdram_clk(SDRAM_CLK), .sdram_cke(SDRAM_CKE), .sdram_ncs(SDRAM_nCS), .sdram_nras(SDRAM_nRAS),
        .sdram_ncas(SDRAM_nCAS), .sdram_nwe(SDRAM_nWE), .sdram_ba(SDRAM_BA), .sdram_a(SDRAM_A),
        .sdram_dqml(SDRAM_DQML), .sdram_dqmh(SDRAM_DQMH), .dq_out(dq_out), .dq_oe(dq_oe),
        .dq_rise(dq_rise), .dq_fall(dq_fall)
    );
    generate for (genvar bit_index=0;bit_index<16;bit_index=bit_index+1) begin : sdram_pads
        wire dq_pin;
        altiobuf_bidir #(.number_of_channels(1), .enable_bus_hold("OFF")) pad (
            .dataio(SDRAM_DQ[bit_index]), .oe(dq_oe), .datain(dq_out[bit_index]), .dataout(dq_pin)
        );
        // Rate 0 consumes only the rising-edge sample. The native packer
        // cannot combine input DDIO and a bidirectional pad, so capture on
        // the same edge in fabric before st_memory's existing second stage.
        reg dq_sample;
        always @(posedge clk_sys) dq_sample <= dq_pin;
        assign dq_rise[bit_index] = dq_sample;
        assign dq_fall[bit_index] = dq_sample;
    end endgenerate
    wire unused_diagnostics = ^{unit0_size, audio_valid, sync_mode, debug_addr, debug_bus_error,
        debug_overlay, debug_halted, vblank, hblank, debug_underruns, debug_frame,
        expansion_plug_request, expansion_response[31:22], video_response_q[27], video_plug_request};
endmodule
