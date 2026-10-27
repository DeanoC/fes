// SPDX-License-Identifier: MIT
// Test wrapper for lendec_tb.cpp: L1's decoders (fes_i386_lendec) feeding
// L2's sums (fes_i386_lstage) directly. The L1/L2 register is left out, so the
// sweep covers both stages' logic in one evaluation. The per-byte records come
// out as one plain array per field, len[k][4*v +: 4] for variant v and so on.
module lendec_tb_top import fes_i386_pkg::*; (
    input  logic [19:0][7:0]  w,
    input  logic              code32,
    output logic [15:0]       prefix,
    output logic [15:0]       run,
    output logic [15:0]       opsize,
    output logic [15:0]       adsize,
    output logic [15:0][15:0] len,
    output logic [15:0][19:0] fin,
    output logic [15:0][3:0]  past_half
);
    lclass_t [15:0] cls;
    lbyte_t [15:0]  dec;

    fes_i386_lendec l1 (.w, .cls);
    fes_i386_lstage l2 (.code32, .cls, .dec);

    for (genvar k = 0; k < 16; k++) begin : g_slot
        assign prefix[k] = dec[k].prefix;
        assign run[k] = dec[k].run;
        assign opsize[k] = cls[k].opsize;
        assign adsize[k] = cls[k].adsize;
        assign len[k] = dec[k].len;
        assign fin[k] = dec[k].fin;
        assign past_half[k] = dec[k].past_half;
    end
endmodule
