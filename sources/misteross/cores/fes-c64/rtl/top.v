// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
`include "c64_bus.vh"
`ifndef FES_C64_BUILD_ID
`define FES_C64_BUILD_ID 128'h00000000000000000000000000000000
`endif

// DE10-Nano shell for the FES Commodore 64 (fes.computer 1.0). The mailbox,
// machine, 1541 and cartridge sockets run in the 52.224 MHz system domain;
// video is scanned in the 74.25 MHz HDMI domain; audio leaves through the
// shared 12.288 MHz I2S serializer. Socket 1 is the ROM window and socket 2
// is the I/O window. Both are vacant here; a linked card replaces the
// vacant response.
module top #(
    parameter [127:0] BUILD_ID = `FES_C64_BUILD_ID
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA,
    output wire        HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire clk_sys;
    wire pixel_clk, pixel_locked;
    wire audio_clk, audio_locked;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;

    c64_system_pll system_clock (
        .refclk(FPGA_CLK1_50), .rst(1'b0),
        .outclk_0(clk_sys), .audio_clk(audio_clk), .locked(audio_locked)
    );
    c64_pixel_pll video_clock (
        .refclk(FPGA_CLK1_50), .rst(1'b0), .outclk_0(pixel_clk), .locked(pixel_locked)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps), .gp_out(hps_to_fpga)
    );

    wire hdmi_scl_low;
    wire hdmi_sda_low;
    wire hdmi_scl_in;
    wire hdmi_sda_in;
    MISTRAL_IO hdmi_scl_pad (.I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO hdmi_sda_pad (.I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in),
        .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );

    wire exec_reset;
    wire [143:0] keyboard_rows;
    wire [15:0] controller_buttons;
    wire [17:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable;
    wire [1:0] unit0_state;
    wire display_request, display_response_valid, display_response_error, display_quiesced;
    wire [6:0] display_opcode;
    wire [7:0] display_index;
    wire [15:0] display_argument, display_response_data;
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1), .ENABLE_C64_DISK(1),
        .ENABLE_SESSION_DISPLAY(1),
        .MEDIA_AW(18),
        .UNIT0_MIN(`FES_COMPUTER_C64_DISK_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_C64_DISK_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(controller_buttons),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_busy(1'b0), .media_changed(1'b0), .media_frozen(),
        .media_read_req(), .media_read_addr(), .media_read_ready(1'b0), .media_read_data(8'd0),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_ready(1'b1), .unit0_state(unit0_state),
        .unit0_size(), .media_write_unit(), .unit1_state(), .unit1_size(),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid),
        .display_response_error(display_response_error),
        .display_response_data(display_response_data), .display_quiesced(display_quiesced)
    );

    wire [`C64_BUS_REQ-1:0] slot1_request, slot2_request;
    wire [`C64_BUS_RSP-1:0] slot1_response, slot2_response;
    wire [7:0] red, green, blue;
    wire de, hsync, vsync;
    wire signed [15:0] audio_sample;
    /* verilator lint_off PINCONNECTEMPTY */
    c64_machine machine (
        .clk_sys(clk_sys), .reset(exec_reset),
        .keyboard_rows(keyboard_rows), .controller_buttons(controller_buttons),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable),
        .disk_ready(unit0_state == 2'(`FES_COMPUTER_MEDIA_STATE_READY)),
        .slot1_request(slot1_request), .slot2_request(slot2_request),
        .slot1_response(slot1_response), .slot2_response(slot2_response),
        .video_clk(pixel_clk),
        .red(red), .green(green), .blue(blue), .de(de), .hsync(hsync), .vsync(vsync),
        .audio_sample(audio_sample),
        .debug_stage(), .debug_error(), .debug_iec(), .debug_pc()
    );
    /* verilator lint_on PINCONNECTEMPTY */

    (* keep *) wire [`C64_BUS_REQ-1:0] slot1_plug_request;
    (* keep *) wire [`C64_BUS_REQ-1:0] slot2_plug_request;
    c64_slot_socket1 slot1 (
        .clock(clk_sys), .request(slot1_request), .response(slot1_response),
        .plug_request(slot1_plug_request), .plug_response(`C64_BUS_RSP'd0)
    );
    c64_slot_socket2 slot2 (
        .clock(clk_sys), .request(slot2_request), .response(slot2_response),
        .plug_request(slot2_plug_request), .plug_response(`C64_BUS_RSP'd0)
    );

    // The shared plane reads launcher frames from HPS DDR and blends them
    // over the finished machine word. Opening/closing never resets the C64.
    wire [27:0] hdmi_video;
    wire ddr_hold, ddr0_reset, ddr0_drained, ddr0_read, ddr0_waitrequest, ddr0_readdatavalid;
    wire [27:0] ddr0_address;
    wire [7:0] ddr0_burstcount;
    wire [127:0] ddr0_readdata;
    fes_display_plane #(
        .PICTURE_LEFT(16'd160), .PICTURE_TOP(16'd0),
        .PICTURE_WIDTH(16'd960), .PICTURE_HEIGHT(16'd720)
    ) display (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(~pixel_locked),
        .exec_reset(exec_reset), .request(display_request), .opcode(display_opcode),
        .index(display_index), .argument(display_argument),
        .response_valid(display_response_valid), .response_error(display_response_error),
        .response_data(display_response_data), .quiesced_sys(display_quiesced),
        .machine({1'b1, vsync, hsync, de, red, green, blue}),
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

    fes_audio_output audio (
        .source_clk(clk_sys), .audio_clk(audio_clk), .locked(audio_locked), .hold(exec_reset),
        .left_sample(audio_sample), .right_sample(audio_sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
    assign HDMI_MCLK = audio_clk;
endmodule
