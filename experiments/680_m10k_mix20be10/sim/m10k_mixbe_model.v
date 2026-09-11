// Simulation only: mixed-width M10K with 20-bit byte-masked writes.
// It does not model analog BRAM delay or collision timing.
module MISTRAL_M10K #(
    parameter INIT = 0,
    parameter CFG_ABITS = 9,
    parameter CFG_DBITS = 20,
    parameter CFG_RD_ABITS = 10,
    parameter CFG_RD_DBITS = 10,
    parameter CFG_MIXED_WIDTH = 1,
    parameter CFG_DUAL_CLOCK = 1,
    parameter CFG_BYTE_ENABLE = 1
) (
    input wire CLK1,
    input wire CLK2,
    input wire [CFG_ABITS-1:0] A1ADDR,
    input wire [CFG_RD_ABITS-1:0] B1ADDR,
    input wire [CFG_DBITS-1:0] A1DATA,
    input wire A1EN,
    input wire [1:0] A1BE,
    input wire B1EN,
    output reg [CFG_RD_DBITS-1:0] B1DATA
);
    reg [9:0] words [0:1023];
    integer i;
    initial begin
        for (i = 0; i < 1024; i = i + 1)
            words[i] = (i[9:0] * 10'd73) ^ (i[9:0] >> 1) ^ 10'h0A6;
        B1DATA = 10'd0;
    end
    always @(posedge CLK1)
        if (A1EN) begin
            if (A1BE[0])
                words[{A1ADDR, 1'b0}] <= A1DATA[9:0];
            if (A1BE[1])
                words[{A1ADDR, 1'b1}] <= A1DATA[19:10];
        end
    always @(posedge CLK2)
        if (B1EN)
            B1DATA <= words[B1ADDR];
endmodule
