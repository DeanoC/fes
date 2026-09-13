// Simulation only: 1024x10 combinational B-port ROM.
/* verilator lint_off UNUSEDSIGNAL */
module MISTRAL_M10K #(
    parameter INIT = 0,
    parameter CFG_ABITS = 10,
    parameter CFG_DBITS = 10,
    parameter CFG_ASYNC_READ = 1
) (
    input wire CLK1,
    input wire [CFG_ABITS-1:0] A1ADDR,
    input wire [CFG_ABITS-1:0] B1ADDR,
    input wire [CFG_DBITS-1:0] A1DATA,
    input wire A1EN,
    input wire ACLR0,
    input wire ACLR1,
    output wire [CFG_DBITS-1:0] B1DATA
);
    reg [9:0] words [0:1023];
    integer i;
    initial begin
        for (i = 0; i < 1024; i = i + 1)
            words[i] = INIT[i * 10 +: 10];
    end
    assign B1DATA = words[B1ADDR];
endmodule
