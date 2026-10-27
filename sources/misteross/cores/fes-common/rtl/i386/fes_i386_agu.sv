// SPDX-License-Identifier: MIT
// AG address unit (docs/i386-cpu-design.md, "Registers, address generation
// and the early ALU"):
//   linear = segment base + base + (index << scale) + displacement
//   offset =                base + (index << scale) + displacement
// D has selected and scaled the operands (fes_i386_dfwd) and registered
// them, so AG adds the segment term, row, the displacement and one more
// operand: last from D, or a late value from the M1, EX or WB register. The
// late value is the only one that is not a register output here, and it
// enters the last 3:2 row. Each sum has its 3:2 rows, one carry chain and one
// select after it.
//
// With a 16-bit address size every operand is zero-extended, the offset wraps
// at 64 KiB and the linear sum must not see the wrap. Linear bits 31:16 are
// seg_hi or seg_hi_plus1 by the carry left once the wrap is removed, which
// bits 17:16 of the two sums give, so only the address-size select follows
// the 32-bit chain.
module fes_i386_agu (
    input  logic        addr32,
    input  logic [31:0] seg_term,           // segment base, zero-extended from 16 bits when !addr32
    input  logic [15:0] seg_hi,             // segment base bits 31:16
    input  logic [15:0] seg_hi_plus1,       // ... plus 1, kept with the segment
    input  logic [31:0] row,
    input  logic [31:0] last,
    input  logic [31:0] disp,               // zero-extended from 16 bits when !addr32
    input  logic [1:0]  late,               // last-row operand: 0 last, 1 M1, 2 EX, 3 WB
    input  logic [1:0]  late_hi,            // the same for bits 31:16
    input  logic [31:0] late_m1,
    input  logic [31:0] late_ex,
    input  logic [31:0] late_wb,
    output logic [31:0] linear,
    output logic [31:0] offset,
    output logic [31:0] base_op             // the last-row operand: last, or the late value
);
    logic [31:0] x;
    logic [31:0] sum_a, carry_a, sum_l, carry_l, sum_o, carry_o;
    logic [31:0] linear_sum, offset_sum;
    logic        wrap_carry;

    function automatic logic [15:0] pick(input logic [1:0] sel, input logic [15:0] d, input logic [15:0] m1,
                                         input logic [15:0] ex, input logic [15:0] wb);
        case (sel)
            2'd1: pick = m1;
            2'd2: pick = ex;
            2'd3: pick = wb;
            default: pick = d;
        endcase
    endfunction

    assign x = {pick(late_hi, last[31:16], late_m1[31:16], late_ex[31:16], late_wb[31:16]),
                pick(late, last[15:0], late_m1[15:0], late_ex[15:0], late_wb[15:0])};

    assign base_op = x;
    // First row from registers only: seg_term + row + disp.
    assign sum_a = seg_term ^ row ^ disp;
    assign carry_a = ((seg_term & row) | (seg_term & disp) | (row & disp)) << 1;
    // Last rows take x.
    assign sum_l = sum_a ^ carry_a ^ x;
    assign carry_l = ((sum_a & carry_a) | (sum_a & x) | (carry_a & x)) << 1;
    assign sum_o = row ^ disp ^ x;
    assign carry_o = ((row & disp) | (row & x) | (disp & x)) << 1;

    assign linear_sum = sum_l + carry_l;
    assign offset_sum = sum_o + carry_o;

    assign wrap_carry = linear_sum[17:16] != offset_sum[17:16];
    assign offset = addr32 ? offset_sum : {16'b0, offset_sum[15:0]};
    assign linear = {addr32 ? linear_sum[31:16] : wrap_carry ? seg_hi_plus1 : seg_hi, linear_sum[15:0]};
endmodule
