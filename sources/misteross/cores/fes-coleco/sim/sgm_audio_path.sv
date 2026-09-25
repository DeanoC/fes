// SPDX-License-Identifier: GPL-2.0-or-later
module sgm_audio_path (
    input wire source_clk, audio_clk, locked, hold,
    input wire signed [15:0] sn_sample, ay_sample,
    output wire signed [15:0] mixed_sample,
    output wire sclk, lrclk, mixed_data, reference_data,
    output wire reference_sclk, reference_lrclk
);
    coleco_audio_mix mix (.sn_sample(sn_sample), .ay_sample(ay_sample),
                          .mixed_sample(mixed_sample));
    fes_audio_output mixed (
        .source_clk(source_clk), .audio_clk(audio_clk), .locked(locked), .hold(hold),
        .left_sample(mixed_sample), .right_sample(mixed_sample),
        .sclk(sclk), .lrclk(lrclk), .sdata(mixed_data)
    );
    fes_audio_output reference (
        .source_clk(source_clk), .audio_clk(audio_clk), .locked(locked), .hold(hold),
        .left_sample(sn_sample), .right_sample(sn_sample),
        .sclk(reference_sclk), .lrclk(reference_lrclk), .sdata(reference_data)
    );
endmodule
