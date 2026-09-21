// SPDX-License-Identifier: GPL-2.0-or-later
// Board-firmware splash shell: HDMI pixels plus HPS I2C to the ADV7513.
// No HPS GP mailbox and no MiSTer user-io.

module top (
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
    wire pixel_clk;

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

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk)
    );

    fes_splash_core splash (
        .pixel_clk(pixel_clk),
        .hdmi_rgb(HDMI_TX_D),
        .hdmi_de(HDMI_TX_DE),
        .hdmi_hs(HDMI_TX_HS),
        .hdmi_vs(HDMI_TX_VS),
        .frame_tick(),
        .phase()
    );

    assign HDMI_TX_CLK = pixel_clk;
endmodule
