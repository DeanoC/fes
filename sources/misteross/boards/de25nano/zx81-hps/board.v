// SPDX-License-Identifier: GPL-2.0-or-later
// HDMI shell for the preserved GHRD; the computer/control stays on its 100MHz clock.
module fes_de25_zx81_board (
    input wire CLOCK0_50,
    input wire ninit_done,
    output wire pixel_clk,
    input wire [23:0] video_rgb,
    input wire video_de, video_hs, video_vs,
    input wire HDMI_TX_INT,
    output wire HDMI_TX_CLK, HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    output wire HDMI_I2C_SCL,
    inout wire HDMI_I2C_SDA,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire pll_locked, config_reset_n, pixel_reset_n;
    sys_pll video_clock (.refclk(CLOCK0_50), .rst(ninit_done),
                         .outclk_0(pixel_clk), .locked(pll_locked));
    fes_de25_top_reset config_reset (.clk(CLOCK0_50), .ninit_done(ninit_done),
                                     .pll_locked(1'b1), .reset_n(config_reset_n));
    fes_de25_top_reset pixel_reset (.clk(pixel_clk), .ninit_done(ninit_done),
                                    .pll_locked(pll_locked), .reset_n(pixel_reset_n));
    I2C_HDMI_Config hdmi_config (.iCLK(CLOCK0_50), .iRST_N(config_reset_n),
        .I2C_SCLK(HDMI_I2C_SCL), .I2C_SDAT(HDMI_I2C_SDA), .HDMI_TX_INT(HDMI_TX_INT), .READY());
    assign {HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S} = 4'b0000;
    assign HDMI_TX_CLK = pixel_clk;
    assign HDMI_TX_DE = pixel_reset_n && video_de;
    assign HDMI_TX_HS = pixel_reset_n && video_hs;
    assign HDMI_TX_VS = pixel_reset_n && video_vs;
    assign HDMI_TX_D = pixel_reset_n ? video_rgb : 24'd0;
endmodule
