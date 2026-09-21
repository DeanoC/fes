// Simulation only: 1024x10 reads, optional 512x20 byte-enable writes.
/* verilator lint_off UNUSEDSIGNAL */
module MISTRAL_M10K #(
    parameter INIT = 0,
    parameter CFG_ABITS = 10,
    parameter CFG_DBITS = 10,
    parameter CFG_RD_ABITS = 10,
    parameter CFG_RD_DBITS = 10,
    parameter CFG_MIXED_WIDTH = 0,
    parameter CFG_DUAL_CLOCK = 0,
    parameter CFG_BYTE_ENABLE = 0,
    parameter CFG_ASYNC_READ = 1
) (
    input wire CLK1,
    input wire CLK2,
    input wire [CFG_ABITS-1:0] A1ADDR,
    input wire [CFG_RD_ABITS-1:0] B1ADDR,
    input wire [CFG_DBITS-1:0] A1DATA,
    input wire A1EN,
    input wire [1:0] A1BE,
    input wire ACLR0,
    input wire ACLR1,
    output wire [CFG_RD_DBITS-1:0] B1DATA
);
    reg [9:0] words [0:1023];
    integer i;
    initial begin
        for (i = 0; i < 1024; i = i + 1)
            words[i] = INIT[i * 10 +: 10];
    end
    always @(posedge CLK1) begin
        if (A1EN) begin
            if (CFG_BYTE_ENABLE) begin
                if (A1BE[0])
                    words[{A1ADDR, 1'b0}] <= A1DATA[9:0];
                if (A1BE[1])
                    words[{A1ADDR, 1'b1}] <= A1DATA[19:10];
            end else
                words[A1ADDR] <= A1DATA[CFG_RD_DBITS-1:0];
        end
    end
    assign B1DATA = words[B1ADDR];
endmodule
