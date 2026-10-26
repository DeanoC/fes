// SPDX-License-Identifier: MIT
// WB: a shift's CF and OF, from the merged result in the WB register and
// the bits EX kept beside it (fes_i386_ex): the bit out at the right
// (out[0]), the one out at the left by size (out[n+1]), and an 8-bit SHL or
// SHR's CF at a count of 16 or 24 (wide).
//
// The flags follow the 386EX where the manual leaves them undefined (Phase 0
// divergences shift-rotate-of and shift-cf-wide-count): OF is the result's
// top bit XOR CF after a left shift or rotate, and its top two bits XORed
// after a right one, at every count; an 8-bit SHL or SHR by 16 or 24 sets CF
// to the operand's bit 0 or bit 7. AF, SF, ZF and PF are the caller's.
module fes_i386_shflags (
    input  fes_i386_pkg::shctl_t c,
    input  logic        hi8,     // the destination is a high byte: the result is in bits 15:8
    input  logic [31:0] result,
    input  logic        out0,
    input  logic        outn,
    input  logic        cfw,     // the wide count's CF ...
    input  logic        wide,    // ... and whether it applies
    output logic        cf,
    output logic        of
);
    logic h, lsb, msb, msb2;
    assign h = c.z8 && hi8;
    assign lsb = h ? result[8] : result[0];
    assign msb = c.z8 ? (h ? result[15] : result[7]) : c.z16 ? result[15] : result[31];
    assign msb2 = c.z8 ? (h ? result[14] : result[6]) : c.z16 ? result[14] : result[30];
    assign cf = c.cf_rol ? lsb : c.cf_ror ? msb : wide ? cfw : c.left ? outn : out0;
    assign of = c.left ? msb ^ cf : msb ^ msb2;
endmodule
