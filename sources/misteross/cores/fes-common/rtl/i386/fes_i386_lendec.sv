// SPDX-License-Identifier: MIT
// L1: the 16 per-byte length decoders of the fetch buffer
// (docs/i386-cpu-design.md, "Length decode and alignment"). Decoder k
// classifies the instruction that would start at w[k], reading w[k] to
// w[k+4]: one prefix, the 0Fh escape, the opcode, ModRM and SIB. It reports
//   prefix[k]  w[k] is a prefix
//   run[k]     ... and w[k+1] is a prefix too
//   opsize[k]  w[k] is 66h;  adsize[k]  w[k] is 67h
// and the length classes of the instruction (fes_i386_oplen), its one prefix
// included:
//   head[k]    the prefix, opcode and fixed immediate bytes, 1-6
//   mb16[k], mb32[k]  ModRM, SIB and displacement bytes for a 16- and a
//              32-bit address size
//   z[k]       2 or 4 more bytes by operand size; moffs[k] by address size
// Neighbouring decoders share opcode units, since a prefixed instruction at k
// is the unprefixed one at k + 1 with one more byte. L2 (fes_i386_lstage)
// adds the classes up for each size.
module fes_i386_lendec (
    input  logic [19:0][7:0]  w,
    output logic [15:0]       prefix,
    output logic [15:0]       run,
    output logic [15:0]       opsize,
    output logic [15:0]       adsize,
    output logic [15:0][2:0]  head,
    output logic [15:0][1:0]  mb16,
    output logic [15:0][2:0]  mb32,
    output logic [15:0]       z,
    output logic [15:0]       moffs
);
    logic [16:0][2:0]      unit_head;
    logic [16:0][1:0][2:0] unit_mb;
    logic [16:0]           unit_z, unit_moffs;

    for (genvar k = 0; k < 17; k++) begin : g_oplen
        fes_i386_oplen oplen (
            .o(w[k + 3 -: 4]), .head(unit_head[k]), .z(unit_z[k]), .moffs(unit_moffs[k]),
            .modrm_bytes(unit_mb[k])
        );
    end

    for (genvar k = 0; k < 16; k++) begin : g_byte
        logic [4:0] with_prefix;
        assign prefix[k] = fes_i386_pkg::is_prefix(w[k]);
        assign run[k] = prefix[k] && fes_i386_pkg::is_prefix(w[k + 1]);
        assign opsize[k] = w[k] == 8'h66;
        assign adsize[k] = w[k] == 8'h67;
        assign with_prefix = fes_i386_pkg::small_add({2'b0, unit_head[k + 1]}, 5'd1);
        assign head[k] = prefix[k] ? with_prefix[2:0] : unit_head[k];
        assign mb16[k] = prefix[k] ? unit_mb[k + 1][0][1:0] : unit_mb[k][0][1:0];
        assign mb32[k] = prefix[k] ? unit_mb[k + 1][1] : unit_mb[k][1];
        assign z[k] = prefix[k] ? unit_z[k + 1] : unit_z[k];
        assign moffs[k] = prefix[k] ? unit_moffs[k + 1] : unit_moffs[k];
    end
endmodule
