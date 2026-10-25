// SPDX-License-Identifier: MIT
// Test wrapper for lendec_tb.cpp: L1's decoders (fes_i386_lendec) feeding
// L2's sums (fes_i386_lstage) directly. The L1/L2 register is left out, so the
// sweep covers both stages' logic in one evaluation.
module lendec_tb_top (
    input  logic [19:0][7:0]  w,
    input  logic              code32,
    output logic [15:0]       prefix,
    output logic [15:0]       run,
    output logic [15:0]       opsize,
    output logic [15:0]       adsize,
    output logic [15:0][15:0] len,
    output logic [15:0][19:0] fin,
    output logic [15:0][3:0]  past_half  // lstage's far; "far" is a C++ word
);
    logic [15:0]      z, moffs;
    logic [15:0][2:0] head, mb32;
    logic [15:0][1:0] mb16;

    fes_i386_lendec l1 (.w, .prefix, .run, .opsize, .adsize, .head, .mb16, .mb32, .z, .moffs);
    fes_i386_lstage l2 (.code32, .run, .opsize, .adsize, .head, .mb16, .mb32, .z, .moffs, .len, .fin, .far(past_half));
endmodule
