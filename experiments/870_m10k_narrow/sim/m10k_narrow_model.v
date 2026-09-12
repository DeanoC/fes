// Simulation only: 8192x1 TDP with a live A port and a tied-off B port.
module MISTRAL_M10K_TDP #(
    parameter INIT = 0,
    parameter CFG_ABITS = 13,
    parameter CFG_DBITS = 1
) (
    input wire CLK1,
    input wire CLK2,
    input wire [CFG_ABITS-1:0] A1ADDR,
    input wire [CFG_ABITS-1:0] B1ADDR,
    input wire A1DATA,
    input wire B1DATA,
    input wire A1EN,
    input wire B1EN,
    input wire A1WE,
    input wire B1WE,
    input wire ACLR0,
    input wire ACLR1,
    output reg A1Q,
    output reg B1Q
);
    reg words [0:8191];
    integer i;
    initial begin
        for (i = 0; i < 8192; i = i + 1)
            words[i] = INIT[i];
        A1Q = 1'b0;
        B1Q = 1'b0;
    end
    always @(posedge CLK1) begin
        if (A1EN && A1WE)
            words[A1ADDR] <= A1DATA;
        if (A1EN)
            A1Q <= A1WE ? A1DATA : words[A1ADDR];
    end
    always @(posedge CLK2)
        if (B1EN)
            B1Q <= words[B1ADDR];
endmodule
