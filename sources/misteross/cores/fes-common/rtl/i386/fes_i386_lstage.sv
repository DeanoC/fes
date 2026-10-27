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
module fes_i386_lstage import fes_i386_pkg::*; (
    input  logic           code32,
    input  lclass_t [15:0] cls,
    output lbyte_t [15:0]  dec
);
    for (genvar k = 0; k < 16; k++) begin : g_slot
        assign dec[k].prefix = cls[k].prefix;
        assign dec[k].run = cls[k].run;
        for (genvar v = 0; v < 4; v++) begin : g_variant
            logic       os32, as32;
            logic [2:0] mb, imm;
            logic [4:0] sum;
            logic [5:0] end_at, end_sum;

            assign os32 = code32 ^ ((v / 2) != 0 || cls[k].opsize);
            assign as32 = code32 ^ ((v % 2) != 0 || cls[k].adsize);
            assign mb = as32 ? cls[k].mb32 : {1'b0, cls[k].mb16};
            assign imm = cls[k].z ? (os32 ? 3'd4 : 3'd2) : cls[k].moffs ? (as32 ? 3'd4 : 3'd2) : 3'd0;
            // Lengths and ends are a few bits: LUTs, not a carry chain.
            assign sum = 5'(cls[k].head) + (* no_carry *) 5'(mb) + (* no_carry *) 5'(imm);
            assign end_sum = 6'(12 + k) + (* no_carry *) 6'(cls[k].head) + (* no_carry *) 6'(mb) + (* no_carry *) 6'(imm);
            assign end_at = cls[k].run ? 6'(12 + k + 1) : end_sum;
            assign dec[k].len[v] = sum[3:0];
            assign dec[k].fin[v] = end_at[4:0];
            // Above 16 (slots 0-3) or 32, tested bit by bit so it stays in LUTs.
            if (k < 4) begin : g_near
                assign dec[k].past_half[v] = end_at[5] || (end_at[4] && end_at[3:0] != 4'd0);
            end else begin : g_this
                assign dec[k].past_half[v] = end_at[5] && end_at[4:0] != 5'd0;
            end
        end
    end
endmodule
