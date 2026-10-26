// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
`ifdef FES_COLECO_VIDEO_PART_DEV
`include "fes_video_part.vh"
`endif
`ifndef FES_COLECO_BUILD_ID
`define FES_COLECO_BUILD_ID 128'h00000000000000000000000000000000
`endif

// DE10-Nano shell for the reduced ColecoVision machine. The mailbox and
// machine remain in the 52.224 MHz system domain; the logical frame buffer crosses
// to the independent 74.25 MHz HDMI pixel domain in coleco_video_720p.
// FES_COLECO_NATIVE_VIDEO_DEV is the native-pixel shell. Its finished video
// word, after any linked part, passes through the shared
// fes.video.session-display plane before the HDMI pins.
module top #(
    parameter [127:0] BUILD_ID = `FES_COLECO_BUILD_ID
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
`ifdef FES_COLECO_NATIVE_VIDEO_PART_DEV
    // The frozen part loader selects this clock by its public net name.
    (* keep *)
`endif
    wire pixel_clk;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire exec_reset;
    wire [15:0] controller_buttons;
    wire [23:0] controller_keypad;
    wire media_ready;
    wire [15:0] media_size;
    wire [14:0] media_addr;
    wire [7:0] media_data;
    wire [12:0] firmware_write_addr;
    wire [15:0] firmware_write_data;
    wire [1:0] firmware_write_enable;
    wire [7:0] logical_x;
    wire [7:0] logical_y;
    wire [3:0] logical_pixel;
    wire logical_blank;
    wire [15:0] audio_sample;
    wire [30:0] bus_request;
    wire pixel_locked;
    wire display_request, display_response_valid, display_response_error, display_quiesced;
    wire [6:0] display_opcode;
    wire [7:0] display_index;
    wire [15:0] display_argument, display_response_data;
`ifdef FES_COLECO_NATIVE_VIDEO_DEV
    localparam bit SESSION_DISPLAY = 1;
`else
    localparam bit SESSION_DISPLAY = 0;
    assign display_response_valid = 1'b0;
    assign display_response_error = 1'b0;
    assign display_response_data = 16'd0;
    assign display_quiesced = 1'b1;
`endif
`ifdef FES_COLECO_EXPANSION_V2_DEV
    wire [27:0] bus_response;
    (* keep *) wire [30:0] plug_request;
    wire [27:0] plug_response = 28'b0;
    coleco_expansion_socket_v2 socket (
        .clock(clk_sys), .request(bus_request), .response(bus_response),
        .plug_request(plug_request), .plug_response(plug_response)
    );
`else
    wire [10:0] bus_response;
`ifdef FES_COLECO_EXPANSION_DEV
    (* keep *) wire [30:0] plug_request;
    wire [10:0] plug_response = 11'b0;
    coleco_expansion_socket socket (
        .clock(clk_sys), .request(bus_request), .response(bus_response),
        .plug_request(plug_request), .plug_response(plug_response)
    );
`else
    assign bus_response = 11'b0;
`endif
`endif
    wire audio_clk, audio_locked;
    fes_audio_output audio (
        .source_clk(clk_sys), .audio_clk(audio_clk), .locked(audio_locked), .hold(exec_reset),
        .left_sample(audio_sample), .right_sample(audio_sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
    assign HDMI_MCLK = audio_clk;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    wire hdmi_scl_low;
    wire hdmi_sda_low;

    // Quartus uses the hard I2C block's open-drain outputs directly. The OSS
    // lane keeps the same boundary through explicit MISTRAL_IO pads and the
    // fixed Cyclone-V I2C BEL.
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

    coleco_system_pll system_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(clk_sys), .audio_clk(audio_clk), .locked(audio_locked)
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk),
        .locked(pixel_locked)
    );

    coleco_application_gp #(.ENABLE_SESSION_DISPLAY(SESSION_DISPLAY)) gp_mailbox (
        .clk(clk_sys),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .gpi(fpga_to_hps),
        .exec_reset(exec_reset),
        .controller_buttons(controller_buttons),
        .controller_keypad(controller_keypad),
        .media_ready(media_ready),
        .media_size(media_size),
        .media_addr(media_addr),
        .media_q(media_data),
        .firmware_write_addr(firmware_write_addr),
        .firmware_write_data(firmware_write_data),
        .firmware_write_enable(firmware_write_enable),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid), .display_response_error(display_response_error),
        .display_response_data(display_response_data), .display_quiesced(display_quiesced)
    );

    /* verilator lint_off PINCONNECTEMPTY */
    coleco_machine machine (
        .clk_sys(clk_sys),
        .reset(exec_reset),
        .controller_buttons(controller_buttons),
        .controller_keypad(controller_keypad),
        .media_ready(media_ready),
        .media_size(media_size),
        .media_data(media_data),
        .media_addr(media_addr),
        .peek_addr(16'h0000),
        .peek_data(),
        .controller1_value(),
        .controller2_value(),
        .logical_x(logical_x),
        .logical_y(logical_y),
        .logical_pixel(logical_pixel),
        .logical_blank(logical_blank),
        .vdp_status(),
        .cpu_addr_debug(),
        .cpu_halt_n(),
        .audio_sample(audio_sample),
        .firmware_we_a(firmware_write_enable[0]),
        .firmware_we_b(firmware_write_enable[1]),
        .firmware_addr(firmware_write_addr),
        .firmware_data(firmware_write_data),
        .bus_request(bus_request), .bus_response(bus_response)
    );
    /* verilator lint_on PINCONNECTEMPTY */

`ifdef FES_COLECO_NATIVE_VIDEO_DEV
    wire [31:0] native_source_request, native_request;
    wire [27:0] native_response;
    coleco_native_video native_source (
        .clk_sys(clk_sys), .hold(exec_reset),
        .logical_x(logical_x), .logical_y(logical_y),
        .logical_pixel(logical_pixel), .logical_blank(logical_blank),
        .request(native_source_request)
    );
    fes_native_cdc native_crossing (
        .source_clk(clk_sys), .pixel_clk(pixel_clk),
        .source_request(native_source_request), .request(native_request)
    );
`ifdef FES_COLECO_NATIVE_VIDEO_PART_DEV
    // This empty shell owns only the native transport. The selected part
    // supplies frame RAM and continuous HDMI timing; it must be composed
    // before the package is run.
    (* keep *) wire [31:0] video_request = native_request;
    (* keep *) wire [31:0] video_plug_request;
    wire [27:0] video_plug_response = 28'b0;
    (* keep *) wire [27:0] video_response;
    coleco_native_video_socket video_socket (
        .clock(pixel_clk), .request(video_request), .response(video_response),
        .plug_request(video_plug_request), .plug_response(video_plug_response)
    );
    assign native_response = video_response;
`else
    fes_native_video #(
`ifdef FES_NATIVE_SCANLINES
        .SCANLINES(1),
`else
        .SCANLINES(0),
`endif
        .WIDTH(256), .HEIGHT(192)
    ) native_output (
        .clock(pixel_clk), .request(native_request), .response(native_response)
    );
`endif
    wire [27:0] hdmi_video;
    wire ddr_hold, ddr0_reset, ddr0_drained, ddr0_read, ddr0_waitrequest, ddr0_readdatavalid;
    wire [27:0] ddr0_address;
    wire [7:0] ddr0_burstcount;
    wire [127:0] ddr0_readdata;
    // Shipped HDMI contract: the shared fes.video.session-display plane,
    // backed by fes.memory.hps-ddr, after the part. It locks to the part's
    // CTA 720p60 word, blends launcher frames over it and delays RGB and sync
    // together. A vacant socket, a closed plane or a fault returns the part's
    // pixels and never stops or reprograms the machine. The shell supplies
    // the plane's valid bit: the cart merge drops the part's constant CE and
    // the boundary reads 0 (DeanoC/nextpnr#297), so the plane never locked.
    fes_display_plane #(
        .PICTURE_LEFT(16'd384), .PICTURE_TOP(16'd168),
        .PICTURE_WIDTH(16'd512), .PICTURE_HEIGHT(16'd384)
    ) display (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(~pixel_locked),
        .exec_reset(exec_reset), .request(display_request), .opcode(display_opcode),
        .index(display_index), .argument(display_argument),
        .response_valid(display_response_valid), .response_error(display_response_error),
        .response_data(display_response_data), .quiesced_sys(display_quiesced),
        .machine({1'b1, native_response[26:0]}),
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
    assign {HDMI_TX_VS, HDMI_TX_HS, HDMI_TX_DE, HDMI_TX_D} = hdmi_video[26:0];
`else
    wire [23:0] machine_rgb;
    wire machine_de, machine_hs, machine_vs, raster_sof, raster_eol;
    coleco_video_720p video (
        .clk_sys(clk_sys),
        .pixel_clk(pixel_clk),
        .raster_ce(1'b1),
        .logical_x(logical_x),
        .logical_y(logical_y),
        .logical_pixel(logical_pixel),
        .logical_blank(logical_blank),
        .red(machine_rgb[23:16]),
        .green(machine_rgb[15:8]),
        .blue(machine_rgb[7:0]),
        .de(machine_de), .hsync(machine_hs), .vsync(machine_vs),
        .frame_tick(), .raster_sof(raster_sof), .raster_eol(raster_eol)
    );

`ifdef FES_COLECO_VIDEO_PART_DEV
    // The machine and capture are unchanged. Both socket boundaries run in
    // the HDMI domain; pixels, sync and enable always incur the same latency.
    (* keep *) wire [31:0] video_request = {1'b0, 1'b0, raster_eol, raster_sof,
                               1'b1, machine_vs, machine_hs, machine_de, machine_rgb};
    (* keep *) wire [31:0] video_plug_request;
    wire [27:0] video_plug_response = 28'b0;
    wire [27:0] video_response;
    reg [27:0] video_fallback_1 = 0, video_fallback_2 = 0;
    always @(posedge pixel_clk) begin
        video_fallback_1 <= video_request[27:0];
        video_fallback_2 <= video_fallback_1;
    end
    coleco_video_socket video_socket (
        .clock(pixel_clk), .request(video_request), .response(video_response),
        .plug_request(video_plug_request), .plug_response(video_plug_response)
    );
    wire [27:0] presented = video_response[27] ? video_response : video_fallback_2;
    assign {HDMI_TX_VS, HDMI_TX_HS, HDMI_TX_DE, HDMI_TX_D} = presented[26:0];
`else
    assign HDMI_TX_D = machine_rgb;
    assign HDMI_TX_DE = machine_de;
    assign HDMI_TX_HS = machine_hs;
    assign HDMI_TX_VS = machine_vs;
`endif
`endif

    assign HDMI_TX_CLK = pixel_clk;
endmodule
