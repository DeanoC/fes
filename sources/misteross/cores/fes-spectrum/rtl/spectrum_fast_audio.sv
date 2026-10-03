// SPDX-License-Identifier: GPL-2.0-or-later
// Development audio, entirely in the 56 MHz system domain. The pin MCLK
// toggles on 384/875 rational enables: average 12.288 MHz, half-periods of
// 2 or 3 system clocks. Each MCLK rising event advances a serializer quarter;
// 256 quarters/frame gives exactly 48 kHz on average and 32-bit I2S slots.
// BCLK half-periods are 9 or 10 clocks; this is not a 12.288 MHz fabric clock.
module spectrum_fast_audio (
    input wire clk, reset, mute,
    input wire [15:0] left_sample, right_sample,
    output reg mclk = 1'b0,
    output wire sample_tick, sclk, lrclk,
    output reg sdata = 1'b0
);
    reg [9:0] fraction = 10'd0;
    reg [7:0] phase = 8'd0;
    reg [15:0] left = 16'd0, right = 16'd0;
    wire [10:0] next_fraction = {1'b0, fraction} + 11'd384;
    wire transition = next_fraction >= 11'd875;
    wire quarter = transition && !mclk;
    wire [4:0] bit_index = phase[6:2];
    assign sclk = phase[1];
    assign lrclk = phase[7];
    assign sample_tick = !reset && quarter && phase == 8'hff;
    always @(posedge clk or posedge reset) begin
        if (reset) begin
            fraction <= 10'd0;
            phase <= 8'd0;
            mclk <= 1'b0;
            left <= 16'd0;
            right <= 16'd0;
            sdata <= 1'b0;
        end else begin
            fraction <= transition ? 10'(next_fraction - 11'd875) : next_fraction[9:0];
            if (transition) mclk <= ~mclk;
            if (quarter) begin
                phase <= phase + 8'd1;
                if (sample_tick) begin
                    left <= mute ? 16'd0 : left_sample;
                    right <= mute ? 16'd0 : right_sample;
                end
                // Falling BCLK: finish the I2S delay bit, then sixteen data
                // bits MSB first; the remaining slot bits are zero padding.
                if (phase[1:0] == 2'b11)
                    sdata <= bit_index < 16 ?
                        (phase[7] ? right[15-bit_index[3:0]] : left[15-bit_index[3:0]]) : 1'b0;
            end
        end
    end
endmodule
