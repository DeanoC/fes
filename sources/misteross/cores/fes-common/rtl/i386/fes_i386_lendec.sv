// SPDX-License-Identifier: MIT
// The 16 per-byte length decoders of the fetch buffer
// (docs/i386-cpu-design.md, "Length decode and alignment"). Decoder k works
// out the instruction that would start at w[k], reading w[k] to w[k+4]: one
// prefix, the 0Fh escape, the opcode, ModRM and SIB. It reports
//   prefix[k]  w[k] is a prefix
//   run[k]     ... and w[k+1] is a prefix too
//   opsize[k]  w[k] is 66h;  adsize[k]  w[k] is 67h
//   len[k]     the length in bytes, that one prefix included, for each
//              effective operand and address size: len[k][4*{os32, as32} +: 4]
// Neighbouring decoders share opcode-length units, since a prefixed
// instruction at k is the unprefixed one at k + 1 with one more byte.
module fes_i386_lendec (
    input  logic [19:0][7:0]  w,
    output logic [15:0]       prefix,
    output logic [15:0]       run,
    output logic [15:0]       opsize,
    output logic [15:0]       adsize,
    output logic [15:0][15:0] len
);
    logic [16:0][3:0][3:0] unprefixed;

    for (genvar k = 0; k < 17; k++) begin : g_oplen
        fes_i386_oplen oplen (.o(w[k + 3 -: 4]), .len(unprefixed[k]));
    end

    for (genvar k = 0; k < 16; k++) begin : g_byte
        assign prefix[k] = fes_i386_pkg::is_prefix(w[k]);
        assign run[k] = prefix[k] && fes_i386_pkg::is_prefix(w[k + 1]);
        assign opsize[k] = w[k] == 8'h66;
        assign adsize[k] = w[k] == 8'h67;
        for (genvar i = 0; i < 4; i++) begin : g_len
            assign len[k][4 * i +: 4] = prefix[k] ? unprefixed[k + 1][i] + 4'd1 : unprefixed[k][i];
        end
    end
endmodule
