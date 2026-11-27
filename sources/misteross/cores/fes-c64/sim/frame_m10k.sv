// SPDX-License-Identifier: MIT
// Model the dual-clock M10K port configuration used by c64_frame_store.
module MISTRAL_M10K #(
    parameter CFG_ABITS=11, CFG_DBITS=5, CFG_DUAL_CLOCK=1, CFG_ASYNC_READ=0,
    parameter [10239:0] INIT=0
) (
    input wire CLK1, CLK2,
    input wire [CFG_ABITS-1:0] A1ADDR, B1ADDR,
    input wire [CFG_DBITS-1:0] A1DATA,
    input wire A1EN, B1EN, ACLR0, ACLR1,
    output reg [CFG_DBITS-1:0] B1DATA
);
    reg [CFG_DBITS-1:0] memory [0:(1<<CFG_ABITS)-1];
    always @(posedge CLK1) if (!A1EN) memory[A1ADDR] <= A1DATA;
    always @(posedge CLK2) if (B1EN) B1DATA <= memory[B1ADDR];
endmodule
