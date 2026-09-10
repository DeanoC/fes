// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_simple_computer.vh"
`ifndef FES_ZX81_BUILD_ID
`define FES_ZX81_BUILD_ID 128'h00000000000000000000000000000000
`endif

// Quartus DE10-Nano shell: 52 MHz ZX81 + 74.25 MHz HDMI, FES GP mailbox.
// BUILD_ID is overridden from the canonical build-input record.
module top #(
    parameter [127:0] BUILD_ID = `FES_ZX81_BUILD_ID
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA
);
    wire clk_sys;
    wire pixel_clk;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire exec_reset;
    wire [39:0] keyboard;
    wire tape_ready;
    wire [14:0] tape_size;
    wire [13:0] tape_addr;
    wire [7:0] tape_data;
    wire ce_6m5;
    wire video_pixel;
    wire hblank;
    wire vblank;
    wire [7:0] red;
    wire [7:0] green;
    wire [7:0] blue;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .out_clk(),
        .scl(HDMI_I2C_SCL),
        .out_data(),
        .sda(HDMI_I2C_SDA)
    );

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

    fes_computer_gp mailbox (
        .clk(clk_sys),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .gpi(fpga_to_hps),
        .exec_reset(exec_reset),
        .keyboard(keyboard),
        .media_ready(tape_ready),
        .media_size(tape_size),
        .media_byte0(),
        .media_byte1(),
        .media_byte2(),
        .media_addr(tape_addr),
        .media_q(tape_data)
    );

    /* verilator lint_off PINCONNECTEMPTY */
    zx81_machine machine (
        .clk_sys(clk_sys),
        .reset(exec_reset),
        .keyboard(keyboard),
        .tape_ready(tape_ready),
        .tape_size(tape_size),
        .tape_data(tape_data),
        .tape_addr_out(tape_addr),
        .ce_6m5(ce_6m5),
        .video_pixel(video_pixel),
        .hblank(hblank),
        .vblank(vblank),
        .hsync_out(),
        .vsync_out(),
        .halt_n(),
        .cpu_addr(),
        .peek_addr(16'h0000),
        .peek_data()
    );
    /* verilator lint_on PINCONNECTEMPTY */

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
        .de(HDMI_TX_DE),
        .hsync(HDMI_TX_HS),
        .vsync(HDMI_TX_VS),
        .frame_tick(),
        .src_x_max(),
        .src_y_max()
    );

    assign HDMI_TX_D = {red, green, blue};
    assign HDMI_TX_CLK = pixel_clk;
endmodule
