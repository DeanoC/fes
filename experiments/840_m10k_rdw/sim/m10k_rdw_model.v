// Simulation only: 512x20 TDP with same-port NEW_DATA write-through.
module MISTRAL_M10K_TDP #(
    parameter INIT = 0,
    parameter CFG_ABITS = 9,
    parameter CFG_DBITS = 20,
    parameter CFG_RDW_MODE_A = "NEW_DATA_NO_NBE_READ",
    parameter CFG_RDW_MODE_B = "NEW_DATA_NO_NBE_READ",
    parameter CFG_RDW_MODE_MIXED = "DONT_CARE"
) (
    input wire CLK1,
    input wire CLK2,
    input wire [CFG_ABITS-1:0] A1ADDR,
    input wire [CFG_ABITS-1:0] B1ADDR,
    input wire [CFG_DBITS-1:0] A1DATA,
    input wire [CFG_DBITS-1:0] B1DATA,
    input wire A1EN,
    input wire B1EN,
    input wire A1WE,
    input wire B1WE,
    input wire ACLR0,
    input wire ACLR1,
    output reg [CFG_DBITS-1:0] A1Q,
    output reg [CFG_DBITS-1:0] B1Q
);
    reg [19:0] words [0:511];
    integer i;
    initial begin
        for (i = 0; i < 512; i = i + 1)
            words[i] = (i[19:0] * 20'd73) ^ (i[19:0] >> 1) ^ 20'h00A6;
        A1Q = 20'd0;
        B1Q = 20'd0;
    end
    always @(posedge CLK1) begin
        if (A1EN && A1WE)
            words[A1ADDR] <= A1DATA;
        if (A1EN)
            A1Q <= (A1WE) ? A1DATA : words[A1ADDR];
    end
    always @(posedge CLK2)
        if (B1EN)
            B1Q <= words[B1ADDR];
endmodule
