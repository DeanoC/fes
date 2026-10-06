// SPDX-License-Identifier: GPL-2.0-or-later
// fes.riscv DE10-Nano shell: fes.application 1.0 mailbox with the gamepad,
// the fixed 74.25 MHz raster and the RV32I system. BUILD_ID is overridden
// with the build-record ID.
module top #(
    parameter [127:0] BUILD_ID = 128'h00000000000000000000000000000000
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
    wire hdmi_scl_in;
    wire hdmi_sda_in;
    wire hdmi_scl_low;
    wire hdmi_sda_low;

    // Linux controls the ADV7513 through HPS I2C at this exact hard-block
    // site. Explicit buffers preserve open-drain low-or-release behavior
    // through OSS synthesis, including feedback from an external device.
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

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire mailbox_reset;
    wire [7:0] mailbox_buttons;
    wire pixel_clk;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    /* verilator lint_off PINCONNECTEMPTY */
    fes_application_gp #(.ENABLE_GAMEPAD(1)) endpoint (
        .clk(pixel_clk),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .menu_request(), .menu_opcode(), .menu_index(), .menu_argument(),
        .menu_response_valid(1'b0), .menu_response_error(1'b0),
        .menu_response_data(16'd0), .menu_quiesced(1'b1),
        .gpi(fpga_to_hps),
        .exec_reset(mailbox_reset),
        .buttons(mailbox_buttons), .controller_buttons(), .controller_keypad(),
        .media_ready(), .media_size(),
        .media_byte0(), .media_byte1(), .media_byte2(),
        .media_write_addr(), .media_write_data(), .media_write_enable(),
        .firmware_write_addr(), .firmware_write_data(), .firmware_write_enable(),
        .firmware_ready()
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk)
    );

    fes_riscv_system system (
        .pixel_clk(pixel_clk), .exec_reset(mailbox_reset), .buttons(mailbox_buttons),
        .hdmi_rgb(HDMI_TX_D), .hdmi_de(HDMI_TX_DE), .hdmi_hs(HDMI_TX_HS), .hdmi_vs(HDMI_TX_VS),
        .cpu_step(), .cpu_pc()
    );
    /* verilator lint_on PINCONNECTEMPTY */

    assign HDMI_TX_CLK = pixel_clk;
endmodule
