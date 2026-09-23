// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_simple_computer.vh"
`ifndef FES_SMS_BUILD_ID
`define FES_SMS_BUILD_ID 128'h00000000000000000000000000000000
`endif

// DE10-Nano shell for the reduced Master System machine. The mailbox and
// machine remain in the 52 MHz system domain; the six-bit logical frame buffer
// crosses to the independent 74.25 MHz HDMI pixel domain in the SMS 720p shell.
// Package id is fes.sms.
module top #(
    parameter [127:0] BUILD_ID = `FES_SMS_BUILD_ID
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    output wire        HDMI_I2S0,
    output wire        HDMI_MCLK,
    output wire        HDMI_LRCLK,
    output wire        HDMI_SCLK,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA
);
    wire clk_sys;
    wire pixel_clk;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire exec_reset;
    wire [39:0] keyboard;
`ifndef FES_SMS_ROM_LINK
    wire media_ready;
    wire [15:0] media_size;
    wire [14:0] media_addr;
    wire [7:0] media_data;
`endif
    wire [7:0] logical_x;
    wire [7:0] logical_y;
    wire [5:0] logical_color;
    wire logical_blank;
    wire signed [15:0] psg_sample;

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

    sys_pll system_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(clk_sys)
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk)
    );

`ifdef FES_SMS_ROM_LINK
    fes_computer_gp gp_mailbox (
`else
    fes_computer_gp #(.ENABLE_MEDIA_STREAM(1)) gp_mailbox (
`endif
        .clk(clk_sys),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .gpi(fpga_to_hps),
        .exec_reset(exec_reset),
        .keyboard(keyboard),
`ifdef FES_SMS_ROM_LINK
        .media_ready(),
        .media_size(),
        .media_byte0(),
        .media_byte1(),
        .media_byte2(),
        .media_addr(14'd0),
        .media_q()
`else
        .media_ready(media_ready),
        .media_size(media_size),
        .media_byte0(),
        .media_byte1(),
        .media_byte2(),
        .media_addr(media_addr),
        .media_q(media_data)
`endif
    );

    /* verilator lint_off PINCONNECTEMPTY */
    sms_machine machine (
        .clk_sys(clk_sys),
        .reset(exec_reset),
        .keyboard(keyboard),
`ifdef FES_SMS_ROM_LINK
        .media_ready(1'b0),
        .media_size(16'd0),
        .media_data(8'hff),
        .media_addr(),
`else
        .media_ready(media_ready),
        .media_size(media_size),
        .media_data(media_data),
        .media_addr(media_addr),
`endif
        .peek_addr(16'h0000),
        .peek_data(),
        .controller1_value(),
        .controller2_value(),
        .port_dc(),
        .port_dd(),
        .logical_x(logical_x),
        .logical_y(logical_y),
        .logical_pixel(logical_color),
        .logical_blank(logical_blank),
        .vdp_status(),
        .cpu_addr_debug(),
        .cpu_halt_n(),
        .psg_tone0_period(),
        .psg_tone0_atten(),
        .psg_tone1_atten(),
        .psg_tone2_atten(),
        .psg_noise_atten(),
        .psg_tone0(),
        .psg_sample(psg_sample)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    sms_video_720p video (
        .clk_sys(clk_sys),
        .pixel_clk(pixel_clk),
        .raster_ce(1'b1),
        .logical_x(logical_x),
        .logical_y(logical_y),
        .logical_color(logical_color),
        .logical_blank(logical_blank),
        .red(HDMI_TX_D[23:16]),
        .green(HDMI_TX_D[15:8]),
        .blue(HDMI_TX_D[7:0]),
        .de(HDMI_TX_DE),
        .hsync(HDMI_TX_HS),
        .vsync(HDMI_TX_VS),
        .frame_tick()
    );

    sms_hdmi_i2s hdmi_audio (
        .pixel_clk(pixel_clk),
        .sample(psg_sample),
        .mclk(HDMI_MCLK),
        .sclk(HDMI_SCLK),
        .lrclk(HDMI_LRCLK),
        .i2s(HDMI_I2S0)
    );

    assign HDMI_TX_CLK = pixel_clk;
endmodule
