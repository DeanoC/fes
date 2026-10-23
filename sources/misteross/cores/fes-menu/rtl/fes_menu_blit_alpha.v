// SPDX-License-Identifier: GPL-2.0-or-later
// The blitter's alpha stage. An EXPAND pixel's source is the command colour
// with alpha * coverage / 255 as its alpha, exactly as gfx.expandOver; every
// other op's source passes through. Three clocks of latency, a new pixel
// every clock. The unsigned 8x8 product maps to a DSP 9x9 multiplier, as in
// fes_menu_blend. Division by 255 is exact for 0..65025: with y = x + 1,
// floor(x / 255) = (y + (y >> 8)) >> 8.
module fes_menu_blit_alpha (
    input wire clk,
    input wire expand,
    input wire [31:0] source,
    input wire [7:0] alpha,
    input wire [7:0] coverage,
    output wire [31:0] result
);
    // Stage 1 registers the operands, stage 2 the product, stage 3 the
    // product + 1; the quotient is formed on the way into the blend.
    reg expand1 = 1'b0, expand2 = 1'b0, expand3 = 1'b0;
    reg [31:0] source1 = 32'd0, source2 = 32'd0, source3 = 32'd0;
    reg [7:0] alpha1 = 8'd0, coverage1 = 8'd0;
    reg [15:0] product2 = 16'd0;
    reg [16:0] y = 17'd0;
    always @(posedge clk) begin
        expand1 <= expand; source1 <= source; alpha1 <= alpha; coverage1 <= coverage;
        product2 <= {8'd0, alpha1} * {8'd0, coverage1};
        expand2 <= expand1; source2 <= source1;
        y <= {1'b0, product2} + 17'd1;
        expand3 <= expand2; source3 <= source2;
    end
    wire [16:0] quotient = (y + (y >> 8)) >> 8;
    assign result = expand3 ? {quotient[7:0], source3[23:0]} : source3;
endmodule
