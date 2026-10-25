// SPDX-License-Identifier: MIT
// Test wrapper for dag_tb.cpp: D's address operands (fes_i386_amode,
// fes_i386_dfwd) feeding AG (fes_i386_agu) directly. The D/AG register is
// left out, so late_m1, late_ex and late_wb carry the values those registers
// hold one cycle later, when the uop is in AG.
module dag_tb_top (
    input  logic [7:0]        modrm,
    input  logic [7:0]        sib,
    input  logic              addr32,
    input  logic [31:0]       seg_base,
    input  logic [31:0]       disp,
    input  logic [7:0][31:0]  gpr,
    input  logic [4:0]        prod_valid,
    input  logic [4:0][2:0]   prod_dst,
    input  logic [4:0]        prod_ready,
    input  logic [4:0]        prod_late,
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
    fes_i386_dfwd dfwd (
        .has_base, .base, .has_index, .index, .scale, .addr32, .gpr, .prod_valid, .prod_dst,
        .prod_ready, .prod_late, .stage_value, .row, .last, .late, .late_hi, .stall
    );
    fes_i386_agu agu (
        .addr32, .seg_term(addr32 ? seg_base : {16'b0, seg_base[15:0]}), .seg_hi(seg_base[31:16]),
        .seg_hi_plus1(seg_base[31:16] + 16'd1), .row, .last, .disp, .late, .late_hi, .late_m1, .late_ex,
        .late_wb, .linear, .offset
    );
endmodule
