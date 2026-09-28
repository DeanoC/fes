// SPDX-License-Identifier: GPL-2.0-or-later
// Described menu firmware. Runtime owns activation and DDR buffer contents.
module top #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire FPGA_CLK1_50,
    output wire HDMI_TX_CLK, HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    inout wire HDMI_I2C_SCL, HDMI_I2C_SDA
);
    wire scl_in, sda_in, scl_low, sda_low, pixel_clk, locked;
    MISTRAL_IO hdmi_scl_pad (.I(1'b0), .OE(scl_low), .O(scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO hdmi_sda_pad (.I(1'b0), .OE(sda_low), .O(sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(scl_in), .sda(sda_in), .out_clk(scl_low), .out_data(sda_low)
    );
    pixel_pll video_clock (.refclk(FPGA_CLK1_50), .rst(1'b0), .outclk_0(pixel_clk), .locked(locked));
    wire [31:0] gpo, gpi;
    wire [23:0] rgb;
    wire de, hs, vs;
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    fes_menu_endpoint endpoint (
        .clk(pixel_clk), .reset_hold(!locked), .gpo(gpo), .build_id(BUILD_ID), .gpi(gpi),
        .displayed_sequence(), .underflows(), .exec_reset(), .enable(), .quiesced(), .faulted(),
        .rgb(rgb), .de(de), .hs(hs), .vs(vs)
    );
    assign HDMI_TX_CLK = pixel_clk;
    assign HDMI_TX_DE = locked && de;
    assign HDMI_TX_HS = locked && hs;
    assign HDMI_TX_VS = locked && vs;
    assign HDMI_TX_D = locked ? rgb : 24'd0;
endmodule
