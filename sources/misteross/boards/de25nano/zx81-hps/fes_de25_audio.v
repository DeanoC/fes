// SPDX-License-Identifier: GPL-2.0-or-later
// Terasic HDMI_ASx4's dedicated av_pll: CLOCK1_50 -> nominal 12.288 MHz.
// Keep this separate from the pixel PLL, using the standard coherent PCM/I2S
// path. The board supplies its reference reset; guest HOLD only mutes samples.
module fes_de25_audio (
    input wire CLOCK1_50, source_clk, reset_n, hold,
    input wire [15:0] left_sample, right_sample,
    output wire locked,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire audio_clk;
    av_pll clock (
        .refclk(CLOCK1_50), .rst(!reset_n), .outclk_0(audio_clk), .locked(locked)
    );
    assign HDMI_MCLK = audio_clk;
    fes_audio_output audio (
        .source_clk(source_clk), .audio_clk(audio_clk), .locked(locked), .hold(hold),
        .left_sample(left_sample), .right_sample(right_sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
endmodule
