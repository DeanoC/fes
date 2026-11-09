// SPDX-License-Identifier: MIT
// The fes_i386_cpu front end: F1, F2, L (the length decoders), the fetch
// buffer and Q, with the branch target buffer read beside the fetch port.
// Fetch reads a memory port (the L1I, or a memory model in the front-end
// bench) whose data may arrive late. Fetch is sequential from the last
// redirect except where the BTB predicts a branch taken (pred_en), a RET's
// to the return stack's top (fes_i386_ras), and Q hands D one instruction
// per cycle when no earlier prefix needs a cycle of its own. WB writes the
// BTB (btb_wr) and restores the return stack on a redirect (ras_set_*);
// with no BTB writes it predicts nothing.
module fes_i386_frontend import fes_i386_pkg::*; (
    input  logic                   clk,
    input  logic                   reset,
    input  logic                   redirect,
    input  logic [31:0]            redirect_linear,
    input  logic                   code32,
    output logic [27:0]            mem_block,
    output logic                   mem_load,
    input  logic [15:0][7:0]       mem_data,
    input  logic                   mem_valid,
    input  ftag_t                  mem_tag,
    output logic                   q_valid,
    output fetched_t               q,
    input  logic                   d_ready,
    input  logic                   pred_en,
    input  btb_write_t             btb_wr,
    // The return stack's restore on a redirect (fes_i386_ras set_*).
    input  logic [3:0]             ras_set_ptr,
    input  logic                   ras_set_wr,
    input  logic [31:0]            ras_set_lin,
    output logic [6:0][27:0]       held_block,  // the blocks the front end holds (fes_i386_fetchbuf)
    output logic [6:0]             held_valid,
    output logic [6:0][7:0]        held_pp,     // ... their physical page bits 19:12, where looked up (held_pk)
    output logic [6:0]             held_pk
);
    logic [1:0]        release_half, half_valid;
    logic              start_half;
    logic [15:0][7:0]  fill_bytes;
    logic [1:0][27:0]  half_block;
    logic [31:0][7:0]  bytes;
    lbyte_t [31:0]     dec;
    logic [31:0][3:0]  avail;

    logic              btb_hit, btb_way, jump_clear, l2_seq;
    logic [3:0]        btb_enter;
    btb_entry_t        btb_entry;
    btb_info_t [1:0]   half_info;
    logic [1:0]        half_seq;
    logic [31:0][3:0]  jm, jt, jx;
    logic              ras_push, ras_pop;
    logic [31:0]       ras_push_lin, ras_top;
    ftag_t [1:0]       half_tag;
    ftag_t             l2_tag;

    fes_i386_btb btb (.clk, .mem_load, .mem_block, .enter(btb_enter), .wr(btb_wr), .hit(btb_hit), .way(btb_way), .entry(btb_entry));

    fes_i386_ras ras (
        .clk, .reset, .redirect, .set_ptr(ras_set_ptr), .set_wr(ras_set_wr), .set_lin(ras_set_lin),
        .push(ras_push), .push_lin(ras_push_lin), .pop(ras_pop), .top(ras_top)
    );

    fes_i386_fetchbuf fetch (
        .clk, .reset, .redirect, .redirect_linear, .code32, .mem_block, .mem_load, .mem_data,
        .mem_valid, .mem_tag, .release_half, .q_half(start_half), .half_valid, .fill_bytes, .half_block,
        .bytes, .dec, .avail, .btb_enter, .btb_hit, .btb_way, .btb_entry, .pred_en, .jump_clear, .ras_top, .ras_push, .ras_push_lin, .ras_pop, .half_info, .half_seq,
        .l2_seq, .jm, .jt, .jx, .held_block, .held_valid, .half_tag, .l2_tag, .held_pp, .held_pk
    );

    fes_i386_qalign align (
        .clk, .reset, .redirect, .redirect_linear, .half_valid, .half_block, .bytes, .dec, .avail,
        .fill_data(fill_bytes), .release_half, .start_half,
        .q_valid, .q, .d_ready, .jm, .jt, .jx, .half_info, .half_seq, .l2_seq, .jump_clear, .half_tag, .l2_tag
    );
endmodule
