// SPDX-License-Identifier: GPL-2.0-or-later
// One B, G, R, X destination pixel under a straight-alpha B, G, R, A source
// pixel, exactly as gfx.blendOver: each channel is (s*a + d*(255-a)) / 255,
// alpha 0 keeps the destination and alpha 255 takes the source. The result's
// X byte is 0. Two clocks of latency, a new pixel every clock.
// s*a + d*(255-a) = d*255 + a*(s-d), so each channel takes one unsigned 8x8
// product of a and |s-d|. Division by 255 is exact for 0..65025: with
// y = x + 1, floor(x / 255) = (y + (y >> 8)) >> 8.
module fes_menu_blend (
    input wire clk,
    input wire [31:0] source,
    input wire [31:0] destination,
    output reg [31:0] result = 32'd0
);
    wire [7:0] a = source[31:24];
    reg [23:0] s1 = 24'd0, d1 = 24'd0;
    reg zero = 1'b0, full = 1'b0;
    wire [23:0] blended;
    genvar c;
    generate for (c = 0; c < 3; c = c + 1) begin : channel
        wire [7:0] s = source[c * 8 +: 8], d = destination[c * 8 +: 8];
        wire [7:0] difference = s >= d ? s - d : d - s;
        reg [15:0] product = 16'd0, base = 16'd0;
        reg below = 1'b0;
        always @(posedge clk) begin
            product <= {8'd0, a} * {8'd0, difference};
            base <= {d, 8'd0} - {8'd0, d};
            below <= s < d;
        end
        wire [16:0] x = (below ? {1'b0, base} - {1'b0, product} : {1'b0, base} + {1'b0, product}) + 17'd1;
        wire [16:0] quotient = (x + (x >> 8)) >> 8;
        assign blended[c * 8 +: 8] = quotient[7:0];
    end endgenerate
    always @(posedge clk) begin
        s1 <= source[23:0]; d1 <= destination[23:0];
        zero <= a == 8'd0; full <= a == 8'd255;
        result <= {8'd0, zero ? d1 : full ? s1 : blended};
    end
endmodule
