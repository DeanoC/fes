// Simulation only: 512x20 SDP with a registered B-port read.
module MISTRAL_M10K #(
    parameter INIT = 0,
    parameter CFG_ABITS = 9,
    parameter CFG_DBITS = 20,
    parameter CFG_BYTE_ENABLE = 1,
    parameter CFG_DUAL_CLOCK = 1,
    parameter CFG_OUT_REG_B = 1
) (
    input wire CLK1,
    input wire CLK2,
    input wire [CFG_ABITS-1:0] A1ADDR,
    input wire [CFG_ABITS-1:0] B1ADDR,
    input wire [CFG_DBITS-1:0] A1DATA,
    input wire A1EN,
    input wire [1:0] A1BE,
    input wire B1EN,
    input wire ACLR0,
    input wire ACLR1,
    output reg [CFG_DBITS-1:0] B1DATA
);
    reg [19:0] words [0:511];
    reg [8:0] b_addr;
    integer i;
    initial begin
        for (i = 0; i < 512; i = i + 1)
            words[i] = (i[19:0] * 20'd73) ^ (i[19:0] >> 1) ^ 20'h00A6;
        B1DATA = 20'd0;
        b_addr = 9'd0;
    end
    always @(posedge CLK1)
        if (A1EN) begin
            if (A1BE[0])
                words[A1ADDR][9:0] <= A1DATA[9:0];
            if (A1BE[1])
                words[A1ADDR][19:10] <= A1DATA[19:10];
        end
    always @(posedge CLK2)
        if (B1EN) begin
            b_addr <= B1ADDR;
            B1DATA <= words[b_addr];
        end
endmodule
