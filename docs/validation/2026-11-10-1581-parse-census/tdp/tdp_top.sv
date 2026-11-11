// Harness for the P3 comparison: one tdp_ram (inferred or altsyncram) with every port registered
// at the pins, so Fmax covers the RAM's own register-to-register paths. The 720p framebuffer shape:
// 49,152 words of 4 bits on a 16-bit address, two independent clocks.
module tdp_top #(parameter int DW = 4, parameter int AW = 16, parameter int WORDS = 49152) (
    input logic clk_a, input logic we_a, input logic [AW-1:0] addr_a, input logic [DW-1:0] wdata_a, output logic [DW-1:0] rdata_a,
    input logic clk_b, input logic we_b, input logic [AW-1:0] addr_b, input logic [DW-1:0] wdata_b, output logic [DW-1:0] rdata_b);
    logic we_a_r, we_b_r; logic [AW-1:0] addr_a_r, addr_b_r; logic [DW-1:0] wdata_a_r, wdata_b_r, q_a, q_b;
    always_ff @(posedge clk_a) begin we_a_r <= we_a; addr_a_r <= addr_a; wdata_a_r <= wdata_a; rdata_a <= q_a; end
    always_ff @(posedge clk_b) begin we_b_r <= we_b; addr_b_r <= addr_b; wdata_b_r <= wdata_b; rdata_b <= q_b; end
    tdp_ram #(.DW(DW), .AW(AW), .WORDS(WORDS)) ram (
        .clk_a, .we_a(we_a_r), .addr_a(addr_a_r), .wdata_a(wdata_a_r), .rdata_a(q_a),
        .clk_b, .we_b(we_b_r), .addr_b(addr_b_r), .wdata_b(wdata_b_r), .rdata_b(q_b));
endmodule
