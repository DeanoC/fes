// The P3 inferred form: FES_RAM_TDP on the quartus-lite lane is (* ramstyle = "M10K, no_rw_check" *).
// Registered reads; a read during a write on the same port returns the new data.
module tdp_ram #(parameter int DW = 4, parameter int AW = 16, parameter int WORDS = 1 << AW) (
    input logic clk_a, input logic we_a, input logic [AW-1:0] addr_a, input logic [DW-1:0] wdata_a, output logic [DW-1:0] rdata_a,
    input logic clk_b, input logic we_b, input logic [AW-1:0] addr_b, input logic [DW-1:0] wdata_b, output logic [DW-1:0] rdata_b);
    (* ramstyle = "M10K, no_rw_check" *) logic [DW-1:0] mem [0:WORDS-1];
    always_ff @(posedge clk_a)
        if (we_a) begin mem[addr_a] <= wdata_a; rdata_a <= wdata_a; end
        else rdata_a <= mem[addr_a];
    always_ff @(posedge clk_b)
        if (we_b) begin mem[addr_b] <= wdata_b; rdata_b <= wdata_b; end
        else rdata_b <= mem[addr_b];
endmodule
