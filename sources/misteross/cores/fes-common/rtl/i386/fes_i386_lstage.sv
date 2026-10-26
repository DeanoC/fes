// SPDX-License-Identifier: MIT
// L2: each byte's length and next start (docs/i386-cpu-design.md, "Length
// decode and alignment"), from the length classes L1 (fes_i386_lendec)
// registered.
//
// L1's window held the previous block's bytes 12-15 and then the block.
// Counting its first byte as 12, slot k sits at 12 + k: the previous half's
// bytes 12-15 are 12-15 and the new block's bytes are 16-31. For each
// prefix-accumulator variant v = {66h seen, 67h seen} the slot reports
// (fes_i386_pkg::lbyte_t)
//   len[v]        the instruction's length, its one prefix included
//   fin[v]        where it ends, 12 + k + length (12 + k + 1 for a prefix
//                 that another prefix follows), modulo 32
//   past_half[v]  it ends past its own half: above 16 for slots 0-3, above
//                 32 for the rest
// The end is summed beside the length, so no add follows the length.
// CS.D and the byte's own 66h or 67h are folded into each variant, so Q
// selects by its accumulator alone; a CS.D change flushes the front end.
module fes_i386_lstage (
    input  logic                         code32,
    input  fes_i386_pkg::lclass_t [15:0] cls,
    output fes_i386_pkg::lbyte_t [15:0]  dec
);
    // Records are read and written whole: Yosys reads dec[k].field as a
    // generate-block name (DeanoC/yosys#52).
    for (genvar k = 0; k < 16; k++) begin : g_slot
        fes_i386_pkg::lclass_t c;
        fes_i386_pkg::lbyte_t  d;
        assign c = cls[k];
        assign d.prefix = c.prefix;
        assign d.run = c.run;
        assign dec[k] = d;
        for (genvar v = 0; v < 4; v++) begin : g_variant
            logic       os32, as32;
            logic [2:0] mb, imm;
            logic [4:0] sum;
            logic [5:0] end_at, end_sum;

            assign os32 = code32 ^ ((v / 2) != 0 || c.opsize);
            assign as32 = code32 ^ ((v % 2) != 0 || c.adsize);
            assign mb = as32 ? c.mb32 : {1'b0, c.mb16};
            assign imm = c.z ? (os32 ? 3'd4 : 3'd2) : c.moffs ? (as32 ? 3'd4 : 3'd2) : 3'd0;
            assign sum = fes_i386_pkg::small_add(
                fes_i386_pkg::small_add({2'b0, c.head}, {2'b0, mb}), {2'b0, imm});
            assign end_sum = fes_i386_pkg::small_add6(
                fes_i386_pkg::small_add6(6'(12 + k), {3'b0, c.head}),
                fes_i386_pkg::small_add6({3'b0, mb}, {3'b0, imm}));
            assign end_at = c.run ? 6'(12 + k + 1) : end_sum;
            assign d.len[v] = sum[3:0];
            assign d.fin[v] = end_at[4:0];
            // Above 16 (slots 0-3) or 32, tested bit by bit so it stays in LUTs.
            if (k < 4) begin : g_near
                assign d.past_half[v] = end_at[5] || (end_at[4] && end_at[3:0] != 4'd0);
            end else begin : g_this
                assign d.past_half[v] = end_at[5] && end_at[4:0] != 5'd0;
            end
        end
    end
endmodule
