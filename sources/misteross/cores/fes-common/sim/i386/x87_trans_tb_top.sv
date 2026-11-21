// SPDX-License-Identifier: MIT
// The transcendental engine with the backend it drives, for x87_trans_tb.cpp.
module x87_trans_tb_top (
    input  logic        clk,
    input  logic        reset,
    input  logic        start,
    input  logic [3:0]  instruction,
    input  logic [1:0]  rc,
    input  logic        xs,
    input  logic [17:0] xe,
    input  logic [63:0] xm,
    input  logic        ys,
    input  logic [17:0] ye,
    input  logic [63:0] ym,
    input  logic [71:0] bcd,
    output logic        busy,
    output logic        done,
    output logic        fin_zero,
    output logic        fin_sign,
    output logic [17:0] fin_exp,
    output logic [66:0] fin_sig,
    output logic [1:0]  second_mode,
    output logic [79:0] second,
    output logic [2:0]  prem_q,
    output logic        prem_part,
    output logic        rnd_pe,
    output logic        rnd_c1
);
    logic              req_valid, req_ready, raw_valid;
    logic [2:0]        req_op;
    logic [79:0]       req_a, req_b;
    fes_x87_pkg::raw_t raw;
    fes_x87_trans engine (
        .clk, .reset, .cancel(1'b0), .start, .instruction, .rc, .xs, .xe, .xm, .ys, .ye, .ym, .bcd,
        .req_valid, .req_ready, .req_op, .req_a, .req_b, .raw_valid, .raw,
        .busy, .done, .fin_zero, .fin_sign, .fin_exp, .fin_sig, .second_mode, .second,
        .prem_q, .prem_part, .rnd_pe, .rnd_c1
    );
    // Round to nearest and extended precision: an intermediate's rounding is
    // the engine's own, and a zero's sign follows the nearest rule.
    fes_x87_core core (
        .clk, .reset, .flush(1'b0), .req_valid, .ext_ready(1'b1), .req_ready,
        .req_op, .req_a, .req_b, .req_pc(2'd3), .req_rc(2'd0), .req_slot(4'd0),
        .raw_valid, .raw
    );
endmodule
