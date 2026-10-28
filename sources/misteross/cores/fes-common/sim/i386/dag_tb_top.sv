// SPDX-License-Identifier: MIT
// Test wrapper for dag_tb.cpp: D's address operands (fes_i386_amode,
// fes_i386_dfwd) feeding AG (fes_i386_agu) directly. fes_i386_dfwd keeps its
// forwarding table across cycles, so dag_tb.cpp clocks this wrapper and moves
// the producers through AG, M1, M2, EX and WB itself. The D/AG register is left
// out, so late_m1, late_ex and late_wb carry the values those registers hold
// one cycle later, when the uop is in AG. The uop in D leaves when D holds one
// and does not stall.
module dag_tb_top (
    input  logic              clk,
    input  logic              reset,
    input  logic              d_valid,
    input  logic [7:0]        modrm,
    input  logic [7:0]        sib,
    input  logic              addr32,
    input  logic              d_writes,
    input  logic [2:0]        d_dst,
    input  logic [1:0]        d_kind,
    input  logic [31:0]       seg_base,
    input  logic [31:0]       disp,
    input  logic [7:0][31:0]  gpr,
    input  logic [3:0]        prod_valid,
    input  logic [3:0][2:0]   prod_dst,
    input  logic [3:0][1:0]   prod_kind,
    input  logic [3:0][31:0]  stage_value,
    input  logic [31:0]       late_m1,
    input  logic [31:0]       late_ex,
    input  logic [31:0]       late_wb,
    output logic              stall,
    output logic [1:0]        late,
    output logic [31:0]       linear,
    output logic [31:0]       offset
);
    logic        has_base, has_index, stack;
    logic [2:0]  base, index;
    logic [1:0]  scale, late_hi;
    logic [31:0] row, last;

    fes_i386_amode amode (
        .modrm, .sib, .addr32, .has_base, .base, .has_index, .index, .scale, .stack
    );
    // Stack operations are not part of this bench.
    logic [15:0] unused_sp_hi;
    logic        unused_sp_wait;
    logic [31:0] unused_base_op;
    fes_i386_dfwd dfwd (
        .clk, .reset, .has_base, .base, .has_index, .index, .index8(1'b0), .scale, .addr32,
        .d_writes(d_valid && d_writes), .d_dst, .d_kind, .d_sp(1'b0), .advance(d_valid && !stall), .hold(1'b0),
        .prod_valid, .prod_dst, .prod_kind, .prod_sp(4'b0), .gpr, .stage_value, .wb_sp(32'd0), .row, .last, .late,
        .late_hi, .stall, .sp_hi(unused_sp_hi), .sp_wait(unused_sp_wait)
    );
    fes_i386_agu agu (
        .addr32, .seg_term(addr32 ? seg_base : {16'b0, seg_base[15:0]}), .seg_hi(seg_base[31:16]),
        .seg_hi_plus1(seg_base[31:16] + 16'd1), .row, .last, .disp, .late, .late_hi, .late_m1, .late_ex,
        .late_wb, .linear, .offset, .base_op(unused_base_op)
    );
endmodule
