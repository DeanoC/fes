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
// The buffer is two 16-byte halves; buffer byte j holds linear byte j mod 32
// of the block in its half. A decoder needs four bytes after its own, so a
// block's bytes 0-11 are decoded as it passes L1 and its bytes 12-15 when the
// next sequential block does: L1 sees the previous block's last four bytes,
// which L2's register still holds, and then its own block.
//
// The memory port models an M10K with address stall: mem_data is the block at
// the address registered on the last edge with mem_load set.
//
// avail[j][v] says Q may issue or consume the instruction at buffer byte j
// under accumulator variant v: byte j's half is valid, its bytes 12-15 have
// been decoded if j is one of them, and if the instruction ends past its half,
// the next half is valid or being filled. It is registered from next-cycle
// state, so Q's loop reads one register. It ignores Q's release of a half on
// this edge: Q's start has then left that half and cannot read it next cycle.
// For the same reason the next half's term assumes a release whenever Q is in
// that half now.
module fes_i386_fetchbuf (
    input  logic              clk,
    input  logic              reset,
    input  logic              redirect,
    input  logic [31:0]       redirect_linear,
    input  logic              code32,
    output logic [27:0]       mem_block,
    output logic              mem_load,
    input  logic              mem_ready,      // response for the registered F2 address is available
    input  logic [15:0][7:0]  mem_data,
    input  logic [1:0]        release_half,   // Q left this half on this edge
    input  logic              q_half,         // the half Q's start is in
    output logic [1:0]        half_valid,
    output logic [15:0][7:0]  fill_bytes,     // the bytes L2 writes on this edge
    output logic [1:0][27:0]  half_block,
    output logic [31:0][7:0]  bytes,
    // Each buffer byte's decode from L2 (fes_i386_lstage), by accumulator variant.
    output logic [31:0]       dec_prefix,
    output logic [31:0]       dec_run,
    output logic [31:0][15:0] dec_len,
    output logic [31:0][19:0] dec_fin,
    output logic [31:0][3:0]  avail
);
    logic [31:0][3:0] dec_far;  // the instruction at the byte ends past its half
    logic [1:0]       tail_ok;  // the half's bytes 12-15 are decoded
    logic [1:0]       filling;  // L2 writes this half on this edge
    logic             active;       // F2 holds a block; idle from reset to the first redirect
    logic [27:0]      f2_block;     // the block whose address the memory holds
    logic [27:0]      fetch_block;  // the block after f2_block
    logic             l1_valid;     // the F2/L1 register
    logic [27:0]      l1_block;
    logic [15:0][7:0] l1_bytes;
    logic             l2_valid;     // the L1/L2 register: the block and its length classes
    logic [27:0]      l2_block;
    logic [15:0][7:0] l2_bytes;
    logic [15:0]      l2_prefix, l2_run, l2_opsize, l2_adsize, l2_z, l2_moffs;
    logic [15:0][2:0] l2_head, l2_mb32;
    logic [15:0][1:0] l2_mb16;
    logic             half, l2_fire, l1_advance, f2_advance;

    assign half = l2_block[0];
    assign l2_fire = l2_valid && !half_valid[half];
    assign l1_advance = l1_valid && (!l2_valid || l2_fire);
    assign f2_advance = active && mem_ready && (!l1_valid || l1_advance);
    assign filling = {l2_fire && half, l2_fire && !half};
    assign fill_bytes = l2_bytes;
    assign mem_load = redirect || f2_advance;
    assign mem_block = redirect ? redirect_linear[31:4] : fetch_block;

    // L1: the decoders on F2's block behind the previous block's bytes 12-15.
    logic [15:0]      new_prefix, new_run, new_opsize, new_adsize, new_z, new_moffs;
    logic [15:0][2:0] new_head, new_mb32;
    logic [15:0][1:0] new_mb16;

    fes_i386_lendec decoders (
        .w({l1_bytes, l2_bytes[15:12]}), .prefix(new_prefix), .run(new_run), .opsize(new_opsize),
        .adsize(new_adsize), .head(new_head), .mb16(new_mb16), .mb32(new_mb32), .z(new_z), .moffs(new_moffs)
    );

    // L2: lengths and next starts for every size variant.
    logic [15:0][15:0] new_len;
    logic [15:0][19:0] new_fin;
    logic [15:0][3:0]  new_far;

    fes_i386_lstage sums (
        .code32, .run(l2_run), .opsize(l2_opsize), .adsize(l2_adsize), .head(l2_head), .mb16(l2_mb16),
        .mb32(l2_mb32), .z(l2_z), .moffs(l2_moffs), .len(new_len), .fin(new_fin), .far(new_far)
    );

    // Next-cycle state as if Q released nothing.
    logic [1:0] valid_next, tail_next, hold_next;
    logic       l2_valid_next, l2_half_next;

    assign valid_next = half_valid | filling;
    assign tail_next = tail_ok & ~filling | ({!half, half} & {2{l2_fire}} & half_valid);
    assign l2_valid_next = l1_advance || (l2_valid && !l2_fire);
    assign l2_half_next = l1_advance ? l1_block[0] : half;
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
        assign far_next = written ? new_far[SLOT] : dec_far[j];
        for (genvar v = 0; v < 4; v++) begin : g_variant
            assign avail_next[j][v] = here && (!far_next[v] || next_ok);
        end
    end

    always_ff @(posedge clk) begin
        if (reset || redirect) begin
            avail <= '0;
        end else begin
            avail <= avail_next;
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            active <= 1'b0;
            l1_valid <= 1'b0;
            l2_valid <= 1'b0;
            half_valid <= 2'b00;
            tail_ok <= 2'b00;
        end else if (redirect) begin
            active <= 1'b1;
            f2_block <= redirect_linear[31:4];
            fetch_block <= redirect_linear[31:4] + 28'd1;
            l1_valid <= 1'b0;
            l2_valid <= 1'b0;
            half_valid <= 2'b00;
            tail_ok <= 2'b00;
        end else begin
            half_valid <= (half_valid & ~release_half) | filling;
            // A fill completes the other half's tail when that half holds the
            // previous block.
            tail_ok <= tail_ok & ~release_half & ~filling
                     | ({!half, half} & {2{l2_fire}} & half_valid & ~release_half);
            if (f2_advance) begin
                l1_valid <= 1'b1;
                f2_block <= fetch_block;
                fetch_block <= fetch_block + 28'd1;
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
        end

        if (l1_advance) begin
            l2_block <= l1_block;
            l2_bytes <= l1_bytes;
            l2_prefix <= new_prefix;
            l2_run <= new_run;
            l2_opsize <= new_opsize;
            l2_adsize <= new_adsize;
            l2_head <= new_head;
            l2_mb16 <= new_mb16;
            l2_mb32 <= new_mb32;
            l2_z <= new_z;
            l2_moffs <= new_moffs;
        end

        if (l2_fire) begin
            half_block[half] <= l2_block;
            // Slots 4-15 cover this block's bytes 0-11; slots 0-3 the other
            // half's bytes 12-15.
            if (half) begin
                bytes[31:16] <= l2_bytes;
                {dec_prefix[27:16], dec_prefix[15:12]} <= l2_prefix;
                {dec_run[27:16], dec_run[15:12]} <= l2_run;
                {dec_len[27:16], dec_len[15:12]} <= new_len;
                {dec_fin[27:16], dec_fin[15:12]} <= new_fin;
                {dec_far[27:16], dec_far[15:12]} <= new_far;
            end else begin
                bytes[15:0] <= l2_bytes;
                {dec_prefix[11:0], dec_prefix[31:28]} <= l2_prefix;
                {dec_run[11:0], dec_run[31:28]} <= l2_run;
                {dec_len[11:0], dec_len[31:28]} <= new_len;
                {dec_fin[11:0], dec_fin[31:28]} <= new_fin;
                {dec_far[11:0], dec_far[31:28]} <= new_far;
            end
        end
    end
endmodule
