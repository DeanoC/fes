// SPDX-License-Identifier: GPL-2.0-or-later
// The XT speaker filter holds each 48 kHz PCM value until its next update.
// The common output crosses that held word coherently into the I2S domain.
module pcxt_board_audio (
    input wire source_clk, audio_clk, audio_locked, hold,
    input wire signed [15:0] sample,
    output wire sclk, lrclk, sdata
);
    fes_audio_output output_stage (
        .source_clk(source_clk), .audio_clk(audio_clk), .locked(audio_locked), .hold(hold),
        .left_sample(sample), .right_sample(sample),
        .sclk(sclk), .lrclk(lrclk), .sdata(sdata)
    );
endmodule
