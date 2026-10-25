// SPDX-License-Identifier: MIT
// L: the 16 per-byte length decoders on registered bytes, and each byte's
// next start (docs/i386-cpu-design.md, "Length decode and alignment").
//
// w holds the previous block's bytes 12-15 and then the block in L. Counting
// w[0] as 12, slot k sits at 12 + k: the previous half's bytes 12-15 are
// 12-15 and the new block's bytes are 16-31. For each prefix-accumulator
// variant v = {66h seen, 67h seen} the slot reports
//   len[k][4*v +: 4]  the instruction's length, its one prefix included
//   fin[k][5*v +: 5]  where it ends, 12 + k + length (12 + k + 1 for a prefix
//                     that another prefix follows), modulo 32
//   far[k][v]         it ends past its own half: above 16 for slots 0-3,
//                     above 32 for the rest
// The decoders produce the end beside the length, so no add follows them.
// CS.D and the byte's own 66h or 67h are folded into each variant, so Q
// selects by its accumulator alone; a CS.D change flushes the front end.
module fes_i386_lstage (
    input  logic [19:0][7:0]  w,
    input  logic              code32,
    output logic [15:0]       prefix,
    output logic [15:0]       run,
    output logic [15:0][15:0] len,
    output logic [15:0][19:0] fin,
    output logic [15:0][3:0]  far
);
    logic [15:0]       opsize, adsize;
    logic [15:0][15:0] sized;  // sized[k][4*{os32, as32} +: 4], from the decoders
    logic [15:0][23:0] sized_ends;

    fes_i386_lendec decoders (
        .w, .prefix, .run, .opsize, .adsize, .len(sized), .ends(sized_ends)
    );

    for (genvar k = 0; k < 16; k++) begin : g_slot
        logic [15:0] lengths;
        logic [23:0] ends;

        assign lengths = sized[k];
        assign ends = sized_ends[k];
        for (genvar v = 0; v < 4; v++) begin : g_variant
            logic       os32, as32;
            logic [5:0] end_at;

            assign os32 = code32 ^ ((v / 2) != 0 || opsize[k]);
            assign as32 = code32 ^ ((v % 2) != 0 || adsize[k]);
            assign end_at = run[k] ? 6'(12 + k + 1) : ends[{os32, as32} * 6 +: 6];
            assign len[k][4 * v +: 4] = lengths[{os32, as32, 2'b00} +: 4];
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
