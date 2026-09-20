// Simulation only: 512x20 TDP. ADDRSTALLA=0 holds the A-port address.
module MISTRAL_M10K_TDP #(
    parameter INIT = 0,
    parameter CFG_ABITS = 9,
    parameter CFG_DBITS = 20
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
    input wire ADDRSTALLA,
    output reg [CFG_DBITS-1:0] A1Q,
    output reg [CFG_DBITS-1:0] B1Q
);
    reg [19:0] words [0:511];
    reg [8:0] a_addr;
    integer i;
    initial begin
        for (i = 0; i < 512; i = i + 1)
            words[i] = (i[19:0] * 20'd73) ^ (i[19:0] >> 1) ^ 20'h00A6;
        A1Q = 20'd0;
        B1Q = 20'd0;
        a_addr = 9'd0;
    end
    always @(posedge CLK1) begin
        if (ADDRSTALLA)
            a_addr <= A1ADDR;
        if (A1EN && A1WE)
            words[a_addr] <= A1DATA;
        if (A1EN)
            A1Q <= words[a_addr];
    end
    always @(posedge CLK2)
        if (B1EN)
            B1Q <= words[B1ADDR];
endmodule
