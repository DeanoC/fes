// SPDX-License-Identifier: MIT
// L: the 16 per-byte length decoders on registered bytes, and each byte's
// next start (docs/i386-cpu-design.md, "Length decode and alignment").
//
// w holds the previous block's bytes 12-15 and then the block in L, so slot k
// is the byte at position K = (k < 4 ? 12 + k : k - 4) of its half. For each
// prefix-accumulator variant v = {66h seen, 67h seen} the slot reports
//   len[k][4*v +: 4]  the instruction's length, its one prefix included
//   fin[k][5*v +: 5]  K plus that length (K + 1 for a prefix that another
//                     prefix follows): the next start, counted from this
//                     byte's half
//   far[k][v]         fin above 16: the instruction ends in the next half
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

    fes_i386_lendec decoders (
        .w, .prefix, .run, .opsize, .adsize, .len(sized)
    );

    for (genvar k = 0; k < 16; k++) begin : g_slot
        localparam int K = k < 4 ? 12 + k : k - 4;
        logic [15:0] lengths;

        assign lengths = sized[k];
        for (genvar v = 0; v < 4; v++) begin : g_variant
            logic       os32, as32;
            logic [3:0] length;
            logic [4:0] end_at;

            assign os32 = code32 ^ ((v / 2) != 0 || opsize[k]);
            assign as32 = code32 ^ ((v % 2) != 0 || adsize[k]);
            assign length = lengths[{os32, as32, 2'b00} +: 4];
            assign end_at = run[k] ? 5'(K + 1) : fes_i386_pkg::small_add(5'(K), {1'b0, length});
            assign len[k][4 * v +: 4] = length;
            assign fin[k][5 * v +: 5] = end_at;
            assign far[k][v] = end_at[4] && end_at[3:0] != 4'd0;
        end
    end
endmodule
