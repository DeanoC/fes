// SPDX-License-Identifier: MIT
// L1: the 16 per-byte length decoders of the fetch buffer
// (docs/i386-cpu-design.md, "Length decode and alignment"). Decoder k
// classifies the instruction that would start at w[k], reading w[k] to
// w[k+4]: one prefix, the 0Fh escape, the opcode, ModRM and SIB. cls[k]
// (fes_i386_pkg::lclass_t) holds its prefix flags and its length classes
// (fes_i386_oplen), its one prefix included. Neighbouring decoders share
// opcode units, since a prefixed instruction at k is the unprefixed one at
// k + 1 with one more byte. L2 (fes_i386_lstage) adds the classes up for each
// size.
module fes_i386_lendec (
    input  logic [19:0][7:0]              w,
    output fes_i386_pkg::lclass_t [15:0] cls
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

    // A record is built whole and then stored: Yosys reads cls[k].field as a
    // generate-block name (DeanoC/yosys#52).
    for (genvar k = 0; k < 16; k++) begin : g_byte
        fes_i386_pkg::lclass_t c;
        logic                  prefix;
        logic [4:0]            with_prefix;
        assign prefix = fes_i386_pkg::is_prefix(w[k]);
        assign with_prefix = fes_i386_pkg::small_add({2'b0, unit_head[k + 1]}, 5'd1);
        assign c.prefix = prefix;
        assign c.run = prefix && fes_i386_pkg::is_prefix(w[k + 1]);
        assign c.opsize = w[k] == 8'h66;
        assign c.adsize = w[k] == 8'h67;
        assign c.head = prefix ? with_prefix[2:0] : unit_head[k];
        assign c.mb16 = prefix ? unit_mb[k + 1][0][1:0] : unit_mb[k][0][1:0];
        assign c.mb32 = prefix ? unit_mb[k + 1][1] : unit_mb[k][1];
        assign c.z = prefix ? unit_z[k + 1] : unit_z[k];
        assign c.moffs = prefix ? unit_moffs[k + 1] : unit_moffs[k];
        assign cls[k] = c;
    end
endmodule
