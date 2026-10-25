// SPDX-License-Identifier: MIT
// L2: each byte's length and next start (docs/i386-cpu-design.md, "Length
// decode and alignment"), from the length classes L1 (fes_i386_lendec)
// registered.
//
// L1's window held the previous block's bytes 12-15 and then the block.
// Counting its first byte as 12, slot k sits at 12 + k: the previous half's
// bytes 12-15 are 12-15 and the new block's bytes are 16-31. For each
// prefix-accumulator variant v = {66h seen, 67h seen} the slot reports
//   len[k][4*v +: 4]  the instruction's length, its one prefix included
//   fin[k][5*v +: 5]  where it ends, 12 + k + length (12 + k + 1 for a prefix
//                     that another prefix follows), modulo 32
//   far[k][v]         it ends past its own half: above 16 for slots 0-3,
//                     above 32 for the rest
// The end is summed beside the length, so no add follows the length.
// CS.D and the byte's own 66h or 67h are folded into each variant, so Q
// selects by its accumulator alone; a CS.D change flushes the front end.
module fes_i386_lstage (
    input  logic              code32,
    input  logic [15:0]       run,
    input  logic [15:0]       opsize,
    input  logic [15:0]       adsize,
    input  logic [15:0][2:0]  head,
    input  logic [15:0][1:0]  mb16,
    input  logic [15:0][2:0]  mb32,
    input  logic [15:0]       z,
    input  logic [15:0]       moffs,
    output logic [15:0][15:0] len,
    output logic [15:0][19:0] fin,
    output logic [15:0][3:0]  far
);
    for (genvar k = 0; k < 16; k++) begin : g_slot
        for (genvar v = 0; v < 4; v++) begin : g_variant
            logic       os32, as32;
            logic [2:0] mb, imm;
            logic [4:0] sum;
            logic [5:0] end_at, end_sum;

            assign os32 = code32 ^ ((v / 2) != 0 || opsize[k]);
            assign as32 = code32 ^ ((v % 2) != 0 || adsize[k]);
            assign mb = as32 ? mb32[k] : {1'b0, mb16[k]};
            assign imm = z[k] ? (os32 ? 3'd4 : 3'd2) : moffs[k] ? (as32 ? 3'd4 : 3'd2) : 3'd0;
            assign sum = fes_i386_pkg::small_add(
                fes_i386_pkg::small_add({2'b0, head[k]}, {2'b0, mb}), {2'b0, imm});
            assign end_sum = fes_i386_pkg::small_add6(
                fes_i386_pkg::small_add6(6'(12 + k), {3'b0, head[k]}),
                fes_i386_pkg::small_add6({3'b0, mb}, {3'b0, imm}));
            assign end_at = run[k] ? 6'(12 + k + 1) : end_sum;
            assign len[k][4 * v +: 4] = sum[3:0];
            assign fin[k][5 * v +: 5] = end_at[4:0];
            // Above 16 (slots 0-3) or 32, tested bit by bit so it stays in LUTs.
            if (k < 4) begin : g_near
                assign far[k][v] = end_at[5] || (end_at[4] && end_at[3:0] != 4'd0);
            end else begin : g_this
                assign far[k][v] = end_at[5] && end_at[4:0] != 5'd0;
            end
        end
    end
endmodule
