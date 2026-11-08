// SPDX-License-Identifier: MIT
// F1, F2, L1, L2 and the 32-byte fetch buffer (docs/i386-cpu-design.md,
// "Fetch and L1I" and "Length decode and alignment"). There is no L1I yet: the
// memory port stands for its way-selected output.
//
// F1 picks the next block address (sequential or redirect) into the memory's
// address register. F2 registers the returned 16 bytes; nothing decodes M10K
// data in F2. L1 runs the per-byte length decoders on that register
// (fes_i386_lendec) and registers each byte's length classes. L2 adds them up
// for every size variant (fes_i386_lstage) and writes block and decode into
// its half of the buffer once Q has released that half.
//
// The buffer is two 16-byte halves; buffer byte j holds byte j mod 16 of the
// block in its half. A decoder needs four bytes after its own, so a
// block's bytes 0-11 are decoded as it passes L1 and its bytes 12-15 when the
// next sequential block does: L1 sees the previous block's last four bytes,
// which L2's register still holds, and then its own block.
//
// The memory port models an M10K with address stall: mem_data is the block at
// the address registered on the last edge with mem_load set, once mem_valid
// says it has arrived. F2 waits for it, and the address stays loaded until F2
// takes the block; a redirect replaces a request still waiting.
//
// Prediction (fes_i386_btb, read with the memory). When F2 passes on a block
// whose BTB entry predicts its branch taken, F1 loads the entry's target
// block on the next edge in place of the sequential one it loaded with the
// pass, which F2 drops: one bubble. The halves are filled alternately, from
// the redirect's half, not by block address, so the target block follows its
// branch's block whatever its address. Each block carries its BTB lookup
// (btb_info_t) and whether it is the sequential successor of the block
// filled before it (seq); both are kept per half.
//
// The return stack (fes_i386_ras). A taken entry for a CALL rel pushes the
// linear address after the call's last byte, on the edge after F2 passes on
// its block (when pred_go loads its target); one for a RET pops the stack,
// and its target is the top it pops, not the entry's. Only a block F2
// passes on moves the stack, once.
//
// jm[j][v] says the instruction at buffer byte j under variant v ends where
// a half's BTB entry says its branch ends (that half's block holds the
// branch's last byte; jx[j][v] names the half), and jt[j][v] that the entry
// was predicted taken, so Q goes on at the target, in the half after that
// one. They are registered from next-cycle state as avail is, so
// Q's loop reads one register. An entry ends a branch in its half or at the
// next half's byte 0, so the two halves' ends never coincide.
//
// avail[j][v] says Q may issue or consume the instruction at buffer byte j
// under accumulator variant v: byte j's half is valid, its bytes 12-15 have
// been decoded if j is one of them, and if the instruction ends past its half,
// the next half is valid or being filled. It is registered from next-cycle
// state, so Q's loop reads one register. It ignores Q's release of a half on
// this edge: Q's start has then left that half and cannot read it next cycle.
// For the same reason the next half's term assumes a release whenever Q is in
// that half now. A taken jump from a branch that starts in the half before
// its last byte's leaves Q in the half it releases, for the target; Q clears
// avail then (jump_clear).
//
// A block's bytes 12-15 are decoded with the block filled after it, so after
// a predicted branch's block with the target's bytes. Every instruction up to
// and including the branch ends at or before the branch's end, and an x86
// length depends only on the instruction's own bytes, so their lengths are
// right; an instruction that runs past the end into the target's half is
// flagged by Q (xbad).
module fes_i386_fetchbuf import fes_i386_pkg::*; (
    input  logic              clk,
    input  logic              reset,
    input  logic              redirect,
    input  logic [31:0]       redirect_linear,
    input  logic              code32,
    output logic [27:0]       mem_block,
    output logic              mem_load,
    input  logic [15:0][7:0]  mem_data,
    input  logic              mem_valid,
    input  ftag_t             mem_tag,        // the block's paging tag (fes_i386_l1i)
    input  logic [1:0]        release_half,   // Q left this half on this edge
    input  logic              q_half,         // the half Q's start is in
    output logic [1:0]        half_valid,
    output logic [15:0][7:0]  fill_bytes,     // the bytes L2 writes on this edge
    output logic [1:0][27:0]  half_block,
    output logic [31:0][7:0]  bytes,
    // Each buffer byte's decode from L2 (fes_i386_lstage), by accumulator variant.
    output lbyte_t [31:0]     dec,
    output logic [31:0][3:0]  avail,
    // Prediction: the BTB's lookup of F2's block, whether to predict, the
    // per-half BTB information and sequential flags, and Q's taken jump
    // that empties both halves (avail clears).
    output logic [3:0]        btb_enter,      // where fetch entered F2's block (fes_i386_btb enter)
    input  logic              btb_hit,
    input  logic              btb_way,
    input  btb_entry_t        btb_entry,
    input  logic              pred_en,
    input  logic              jump_clear,
    // The return stack (fes_i386_ras): its top, and F2's push and pop.
    input  logic [31:0]       ras_top,
    output logic              ras_push,
    output logic [31:0]       ras_push_lin,
    output logic              ras_pop,
    output btb_info_t [1:0]   half_info,
    output logic [1:0]        half_seq,
    output logic              l2_seq,
    output logic [31:0][3:0]  jm,
    output logic [31:0][3:0]  jt,
    output logic [31:0][3:0]  jx,
    // The blocks the front end holds or loads next, for the self-modifying
    // code check (fes_i386_cpu): F1's next (fetch_block or a predicted
    // target), F2's, L1's, L2's and the two halves', each with a valid bit.
    output logic [6:0][27:0]  held_block,
    output logic [6:0]        held_valid,
    // Each block's paging tag, kept per half and in L2 (Q's tm and pp), and
    // the held blocks' physical page bits 19:12 where the block has been
    // looked up (held_pk; F1's next blocks have not).
    output ftag_t [1:0]       half_tag,
    output ftag_t             l2_tag,
    output logic [6:0][7:0]   held_pp,
    output logic [6:0]        held_pk
);
    logic [1:0]       tail_ok;  // the half's bytes 12-15 are decoded
    logic [1:0]       filling;  // L2 writes this half on this edge
    logic             active;       // F2 holds a block; idle from reset to the first redirect
    logic [27:0]      f2_block;     // the block whose address the memory holds
    logic [27:0]      fetch_block;  // the block after f2_block
    logic             l1_valid;     // the F2/L1 register
    logic [27:0]      l1_block;
    ftag_t            l1_tag;
    logic [15:0][7:0] l1_bytes;
    logic             l2_valid;     // the L1/L2 register: the block and its length classes
    logic [27:0]      l2_block;
    logic [15:0][7:0] l2_bytes;
    lclass_t [15:0]   l2_cls;
    logic             half, l2_fire, l1_advance, f2_advance;
    // Prediction: pred_go, F1 loads pred_block on this edge (F2 passed on a
    // block predicted taken on the last); f2_seq, F2's block follows the
    // block before it; the blocks' BTB lookups and sequential flags in L1
    // and L2.
    logic             pred_go, pred_now, f2_seq, l1_seq;
    logic [27:0]      pred_block;
    logic [3:0]       pred_enter;
    btb_info_t        f2_info, l1_info, l2_info;
    logic             btb_taken;
    logic [31:0]      btb_target;  // the entry's target, or for a RET's the return stack's top
    logic             push_call;   // pred_go is for a CALL rel's entry ...
    logic [31:0]      push_last;   // ... whose last byte is here

    // half: the half L2's block goes to, alternately from the redirect's.
    assign l2_fire = l2_valid && !half_valid[half];
    assign l1_advance = l1_valid && (!l2_valid || l2_fire);
    assign f2_advance = active && mem_valid && (!l1_valid || l1_advance) && !pred_go;
    assign filling = {l2_fire && half, l2_fire && !half};
    assign fill_bytes = l2_bytes;
    assign mem_load = redirect || pred_go || f2_advance;
    assign mem_block = redirect ? redirect_linear[31:4] : pred_go ? pred_block : fetch_block;

    assign btb_taken = btb_entry.uncond || btb_entry.ctr[1];
    assign pred_now = f2_advance && btb_hit && btb_taken && pred_en;
    assign btb_target = btb_entry.ret ? ras_top : btb_entry.target;
    assign f2_info = '{hit: btb_hit, way: btb_way, ctr: btb_entry.ctr, last: btb_entry.last,
                       taken: pred_now, tblk: btb_target[31:4], toff: btb_target[3:0]};
    assign ras_pop = pred_now && btb_entry.ret;
    assign ras_push = pred_go && push_call;
    assign ras_push_lin = push_last + 32'd1;
    assign held_block = {pred_block, fetch_block, f2_block, l1_block, l2_block, half_block[1], half_block[0]};
    assign held_valid = {pred_go, active, active, l1_valid, l2_valid, half_valid[1], half_valid[0]};
    assign held_pp = {8'd0, 8'd0, mem_tag.pp, l1_tag.pp, l2_tag.pp, half_tag[1].pp, half_tag[0].pp};
    assign held_pk = 7'b0011111;

    // L1: the decoders on F2's block behind the previous block's bytes 12-15.
    lclass_t [15:0] new_cls;

    fes_i386_lendec decoders (.w({l1_bytes, l2_bytes[15:12]}), .cls(new_cls));

    // L2: lengths and next starts for every size variant.
    lbyte_t [15:0] new_dec;

    fes_i386_lstage sums (.code32, .cls(l2_cls), .dec(new_dec));

    // Next-cycle state as if Q released nothing.
    logic [1:0] valid_next, tail_next, hold_next;
    logic       l2_valid_next, l2_half_next;

    assign valid_next = half_valid | filling;
    assign tail_next = tail_ok & ~filling | ({!half, half} & {2{l2_fire}} & half_valid);
    assign l2_valid_next = l1_advance || (l2_valid && !l2_fire);
    assign l2_half_next = half ^ l2_fire;
    assign hold_next = {l2_valid_next && l2_half_next, l2_valid_next && !l2_half_next};

    logic [31:0][3:0] avail_next;
    for (genvar j = 0; j < 32; j++) begin : g_avail
        localparam int H = j / 16;
        localparam int K = j % 16;
        // L2 writes this byte's decode with half H (bytes 0-11, slot K + 4) or
        // with the other half (bytes 12-15, slot K - 12).
        localparam int SLOT = K < 12 ? K + 4 : K - 12;
        logic       here, next_ok, written;
        logic [3:0] far_next;

        assign here = valid_next[H] && (K < 12 || tail_next[H]);
        assign next_ok = hold_next[1 - H] || (q_half == (H != 0) && valid_next[1 - H]);
        assign written = l2_fire && (half == (H != 0)) == (K < 12);
        assign far_next = written ? new_dec[SLOT].past_half : dec[j].past_half;
        for (genvar v = 0; v < 4; v++) begin : g_variant
            assign avail_next[j][v] = here && (!far_next[v] || next_ok);
        end
    end

    always_ff @(posedge clk) begin
        if (reset || redirect || jump_clear) begin
            avail <= '0;
        end else begin
            avail <= avail_next;
        end
    end

    // ---- BTB matches -------------------------------------------------------
    // Each half's entry, as next cycle sees it: the branch's end as a buffer
    // position (the byte after its last), valid while the half is.
    btb_info_t [1:0] info_next;
    logic [1:0][4:0] end_next;
    logic [1:0]      ion_next;
    for (genvar x = 0; x < 2; x++) begin : g_info
        assign info_next[x] = filling[x] ? l2_info : half_info[x];
        assign end_next[x] = {1'(x), 4'd0} + {1'b0, info_next[x].last} + 5'd1;
        assign ion_next[x] = valid_next[x] && info_next[x].hit;
    end
    logic [31:0][3:0] jm_next, jt_next, jx_next;
    for (genvar j = 0; j < 32; j++) begin : g_match
        localparam int H = j / 16;
        localparam int K = j % 16;
        localparam int SLOT = K < 12 ? K + 4 : K - 12;
        logic   written;
        lbyte_t d;
        assign written = l2_fire && (half == (H != 0)) == (K < 12);
        assign d = written ? new_dec[SLOT] : dec[j];
        for (genvar v = 0; v < 4; v++) begin : g_variant
            logic [4:0] n;
            logic       m0, m1;
            // Where Q goes after the instruction, as fes_i386_qalign works it out.
            assign n = {!(d.fin[v][4] ^ ((K < 12) == (H != 0))), d.fin[v][3:0]};
            assign m0 = ion_next[0] && n == end_next[0];
            assign m1 = ion_next[1] && n == end_next[1];
            assign jm_next[j][v] = m0 || m1;
            assign jx_next[j][v] = m1;
            assign jt_next[j][v] = m0 && info_next[0].taken || m1 && info_next[1].taken;
        end
    end
    always_ff @(posedge clk) begin
        jm <= jm_next;
        jt <= jt_next;
        jx <= jx_next;
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            active <= 1'b0;
            l1_valid <= 1'b0;
            l2_valid <= 1'b0;
            half_valid <= 2'b00;
            tail_ok <= 2'b00;
            pred_go <= 1'b0;
        end else if (redirect) begin
            active <= 1'b1;
            f2_block <= redirect_linear[31:4];
            fetch_block <= redirect_linear[31:4] + 28'd1;
            f2_seq <= 1'b0;
            btb_enter <= redirect_linear[3:0];
            half <= redirect_linear[4];
            l1_valid <= 1'b0;
            l2_valid <= 1'b0;
            half_valid <= 2'b00;
            tail_ok <= 2'b00;
            pred_go <= 1'b0;
        end else begin
            half_valid <= (half_valid & ~release_half) | filling;
            // A fill completes the other half's tail when that half holds the
            // previous block.
            tail_ok <= tail_ok & ~release_half & ~filling
                     | ({!half, half} & {2{l2_fire}} & half_valid & ~release_half);
            if (l2_fire) half <= !half;
            pred_go <= pred_now;
            if (pred_now) begin
                pred_block <= btb_target[31:4];
                pred_enter <= btb_target[3:0];
                push_call <= btb_entry.call;
                push_last <= {f2_block, btb_entry.last};
            end
            if (pred_go) begin
                // The sequential block F1 loaded with the pass is dropped.
                f2_block <= pred_block;
                fetch_block <= pred_block + 28'd1;
                f2_seq <= 1'b0;
                btb_enter <= pred_enter;
            end
            if (f2_advance) begin
                l1_valid <= 1'b1;
                f2_block <= fetch_block;
                fetch_block <= fetch_block + 28'd1;
                f2_seq <= 1'b1;
                btb_enter <= 4'd0;
            end else if (l1_advance) begin
                l1_valid <= 1'b0;
            end
            if (l1_advance) begin
                l2_valid <= 1'b1;
            end else if (l2_fire) begin
                l2_valid <= 1'b0;
            end
        end

        if (f2_advance) begin
            l1_block <= f2_block;
            l1_bytes <= mem_data;
            l1_tag <= mem_tag;
            l1_info <= f2_info;
            l1_seq <= f2_seq;
        end

        if (l1_advance) begin
            l2_block <= l1_block;
            l2_bytes <= l1_bytes;
            l2_tag <= l1_tag;
            l2_cls <= new_cls;
            l2_info <= l1_info;
            l2_seq <= l1_seq;
        end

        if (l2_fire) begin
            half_block[half] <= l2_block;
            half_tag[half] <= l2_tag;
            half_info[half] <= l2_info;
            half_seq[half] <= l2_seq;
            // Slots 4-15 cover this block's bytes 0-11; slots 0-3 the other
            // half's bytes 12-15.
            if (half) begin
                bytes[31:16] <= l2_bytes;
                {dec[27:16], dec[15:12]} <= new_dec;
            end else begin
                bytes[15:0] <= l2_bytes;
                {dec[11:0], dec[31:28]} <= new_dec;
            end
        end
    end
endmodule
