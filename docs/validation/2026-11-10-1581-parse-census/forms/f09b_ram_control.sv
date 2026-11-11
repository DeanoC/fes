// f09 control: the same memory with the macro expanding to nothing, so it should map to block RAM.
`define CENSUS_RAMSTYLE
module f09b_ram_control (input logic clk, input logic we, input logic [7:0] wa, input logic [7:0] wd,
                           input logic [7:0] ra, output logic [7:0] rd, output logic ok);
    `CENSUS_RAMSTYLE logic [7:0] mem [0:255];
    always_ff @(posedge clk) begin
        if (we) mem[wa] <= wd;
        rd <= mem[ra];
    end
    assign ok = 1'b1;
endmodule
