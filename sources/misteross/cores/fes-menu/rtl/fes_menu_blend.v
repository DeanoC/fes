// SPDX-License-Identifier: GPL-2.0-or-later
// One B, G, R, X destination pixel under a straight-alpha B, G, R, A source
// pixel, exactly as gfx.blendOver: each channel is (s*a + d*(255-a)) / 255,
// alpha 0 keeps the destination and alpha 255 takes the source. The result's
// X byte is 0. Four clocks of latency, a new pixel every clock.
// s*a + d*(255-a) = d*255 + a*(s-d), so each channel takes one unsigned 8x8
// product of a and |s-d|, which maps to a DSP 9x9 multiplier: both operands
// are zero-extended, so the multiplier's signedness cannot change it.
// Division by 255 is exact for 0..65025: with y = x + 1,
// floor(x / 255) = (y + (y >> 8)) >> 8.
module fes_menu_blend (
    input wire clk,
    input wire [31:0] source,
    input wire [31:0] destination,
    output reg [31:0] result = 32'd0
);
    // Stage 1 registers the operands and each channel's |s-d|, stage 2 the
    // product, stage 3 x = d*255 +/- product + 1, stage 4 the result.
    reg [7:0] a1 = 8'd0;
    reg [23:0] s1 = 24'd0, d1 = 24'd0, s2 = 24'd0, d2 = 24'd0, s3 = 24'd0, d3 = 24'd0;
    reg zero2 = 1'b0, full2 = 1'b0, zero3 = 1'b0, full3 = 1'b0;
    wire [23:0] blended;
    genvar c;
    generate for (c = 0; c < 3; c = c + 1) begin : channel
        wire [7:0] s = source[c * 8 +: 8], d = destination[c * 8 +: 8];
        reg [7:0] difference = 8'd0;
        reg below1 = 1'b0, below2 = 1'b0;
        reg [15:0] base2 = 16'd0, product2 = 16'd0;
        reg [16:0] x = 17'd0;
        always @(posedge clk) begin
            difference <= s >= d ? s - d : d - s;
            below1 <= s < d;
            product2 <= {8'd0, difference} * {8'd0, a1};
            base2 <= {d1[c * 8 +: 8], 8'd0} - {8'd0, d1[c * 8 +: 8]};
            below2 <= below1;
            x <= (below2 ? {1'b0, base2} - {1'b0, product2} : {1'b0, base2} + {1'b0, product2}) + 17'd1;
        end
        wire [16:0] quotient = (x + (x >> 8)) >> 8;
        assign blended[c * 8 +: 8] = quotient[7:0];
    end endgenerate
    always @(posedge clk) begin
        a1 <= source[31:24]; s1 <= source[23:0]; d1 <= destination[23:0];
        s2 <= s1; d2 <= d1; zero2 <= a1 == 8'd0; full2 <= a1 == 8'd255;
        s3 <= s2; d3 <= d2; zero3 <= zero2; full3 <= full2;
        result <= {8'd0, zero3 ? d3 : full3 ? s3 : blended};
    end
endmodule
