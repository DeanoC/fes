// SPDX-License-Identifier: GPL-2.0-or-later
// 12.288 MHz domain; 48 kHz stereo, 32-bit I2S slots, signed 16-bit samples.
// Sample inputs belong to clk. Both channels are captured together at tick.
module fes_audio_i2s (
    input wire clk,
    input wire reset,
    input wire mute,
    input wire [15:0] left_sample,
    input wire [15:0] right_sample,
    output wire sample_tick,
    output wire sclk,
    output wire lrclk,
    output reg sdata = 0
);
    reg [7:0] phase = 0;
    reg [15:0] left = 0;
    reg [15:0] right = 0;
    // Each bit occupies four MCLK cycles. LRCLK changes one bit before MSB.
    assign sclk = phase[1];
    assign lrclk = phase[7];
    assign sample_tick = !reset && phase == 8'hff;
    wire [4:0] bit_index = phase[6:2];
    always @(posedge clk or posedge reset) begin
        if (reset) begin
            phase <= 0;
            left <= 0;
            right <= 0;
            sdata <= 0;
        end else begin
            phase <= phase + 1'b1;
            if (sample_tick) begin
                left <= mute ? 16'd0 : left_sample;
                right <= mute ? 16'd0 : right_sample;
            end
            // The old phase ends in 3 on the falling BCLK edge. Index zero
            // ends the I2S delay bit; then emit sixteen MSB-first data bits.
            if (phase[1:0] == 2'b11)
                sdata <= bit_index < 16 ?
                    (phase[7] ? right[15-bit_index[3:0]] : left[15-bit_index[3:0]]) : 1'b0;
        end
    end
endmodule
