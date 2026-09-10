// Simulation only: 512x20 TDP with the B port disabled. It does not model
// analog BRAM delay or TCLK folding.
module MISTRAL_M10K_TDP #(
    parameter INIT = 0,
    parameter CFG_ABITS = 9,
    parameter CFG_DBITS = 20,
    parameter CFG_BYTE_ENABLE = 0,
    parameter CFG_MIXED_WIDTH = 0,
    parameter CFG_RD_ABITS = CFG_ABITS,
    parameter CFG_RD_DBITS = CFG_DBITS
) (
    input wire CLK1,
    input wire CLK2,
    input wire [CFG_ABITS-1:0] A1ADDR,
    input wire [CFG_RD_ABITS-1:0] B1ADDR,
    input wire [CFG_DBITS-1:0] A1DATA,
    input wire [CFG_RD_DBITS-1:0] B1DATA,
    input wire A1EN,
    input wire B1EN,
    input wire A1WE,
    input wire B1WE,
    input wire ACLR0,
    input wire ACLR1,
    output reg [CFG_DBITS-1:0] A1Q,
    output reg [CFG_RD_DBITS-1:0] B1Q
);
    reg [19:0] words [0:511];
    integer i;
    initial begin
        for (i = 0; i < 512; i = i + 1)
            words[i] = (i[19:0] * 20'd73) ^ (i[19:0] >> 1) ^ 20'h00A6;
        A1Q = 20'd0;
        B1Q = 20'd0;
    end
    always @(posedge CLK1)
        if (A1EN) begin
            if (A1WE)
                words[A1ADDR] <= A1DATA;
            A1Q <= A1WE ? A1DATA : words[A1ADDR];
        end
    always @(posedge CLK2)
        if (B1EN) begin
            if (B1WE)
                words[B1ADDR] <= B1DATA;
            B1Q <= B1WE ? B1DATA : words[B1ADDR];
        end
endmodule
