// An attribute inside a `define, as a header RAM attribute macro would be. Without the attribute
// the 256 x 8 registered-read memory maps to a block RAM; with ramstyle "logic" it must not.
// Quartus: read the RAM count in the report. Yosys: the attribute must reach the memory.
`define CENSUS_RAMSTYLE (* ramstyle = "logic" *)
module f09_attr_in_define (input logic clk, input logic we, input logic [7:0] wa, input logic [7:0] wd,
                           input logic [7:0] ra, output logic [7:0] rd, output logic ok);
    `CENSUS_RAMSTYLE logic [7:0] mem [0:255];
    always_ff @(posedge clk) begin
        if (we) mem[wa] <= wd;
        rd <= mem[ra];
    end
    assign ok = 1'b1;
endmodule
