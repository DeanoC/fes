// SPDX-License-Identifier: MIT
// AG address unit (docs/i386-cpu-design.md, "Registers, address generation
// and the early ALU"):
//   linear = seg_base + base + (index << scale) + disp
//   offset =            base + (index << scale) + disp
// Each sum is carry-save compressed to two operands (4:2 for linear) and
// finished by one carry chain; the two chains run in parallel.
//
// With a 16-bit address size the offset wraps at 64 KiB but the linear sum
// must not see the wrap. The low halves are the same either way, so the
// linear chain adds seg_base[15:0] to the 16-bit operands, the offset chain
// gives the wrap count in its bits 17:16, and linear bits 31:16 are
// seg_base[31:16] or seg_base_hi_plus1 by the carry left once the wrap is
// removed. No second chain runs after the first. The 16-bit form has no
// scaled index; scale must be 0.
module fes_i386_agu (
    input  logic        addr32,
    input  logic [31:0] seg_base,
    input  logic [15:0] seg_base_hi_plus1,  // seg_base[31:16] + 1, kept with the segment
    input  logic [31:0] base,
    input  logic [31:0] index,
    input  logic [1:0]  scale,
    input  logic [31:0] disp,
    output logic [31:0] linear,
    output logic [31:0] offset
);
    logic [31:0] a, b, c, d;
    logic [31:0] sum_bd, carry_bd, sum_abd, carry_abd;
    logic [31:0] sum_offset, carry_offset, sum_linear, carry_linear;
    logic [31:0] linear_sum, offset_sum;

    assign a = addr32 ? seg_base : {16'b0, seg_base[15:0]};
    assign b = addr32 ? base : {16'b0, base[15:0]};
    assign c = addr32 ? index << scale : {16'b0, index[15:0]};
    assign d = addr32 ? disp : {16'b0, disp[15:0]};

    // The scaled index arrives last, so it enters the last 3:2 row of each
    // sum. offset: (b + d) + c. linear: (a + b + d) + c, with a + b + d as a
    // 3:2 row of its own.
    assign sum_bd = b ^ d;
    assign carry_bd = (b & d) << 1;
    assign sum_abd = a ^ b ^ d;
    assign carry_abd = ((a & b) | (a & d) | (b & d)) << 1;
    assign sum_offset = sum_bd ^ carry_bd ^ c;
    assign carry_offset = ((sum_bd & carry_bd) | (sum_bd & c) | (carry_bd & c)) << 1;
    assign sum_linear = sum_abd ^ carry_abd ^ c;
    assign carry_linear = ((sum_abd & carry_abd) | (sum_abd & c) | (carry_abd & c)) << 1;

    assign offset_sum = sum_offset + carry_offset;
    assign linear_sum = sum_linear + carry_linear;

    assign offset = addr32 ? offset_sum : {16'b0, offset_sum[15:0]};
    assign linear[15:0] = linear_sum[15:0];
    assign linear[31:16] = addr32 ? linear_sum[31:16]
                         : linear_sum[17:16] != offset_sum[17:16] ? seg_base_hi_plus1 : seg_base[31:16];
endmodule
