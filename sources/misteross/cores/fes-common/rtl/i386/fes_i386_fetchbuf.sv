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
module fes_i386_fetchbuf (
    input  logic              clk,
    input  logic              reset,
    input  logic              redirect,
    input  logic [31:0]       redirect_linear,
    input  logic              code32,
    output logic [27:0]       mem_block,
    output logic              mem_load,
    input  logic [15:0][7:0]  mem_data,
    input  logic [1:0]        release_half,   // Q left this half on this edge
    output logic [1:0]        half_valid,
    output logic [1:0]        tail_ok,        // the half's bytes 12-15 are decoded
    output logic [1:0]        filling,        // L writes this half on this edge
    output logic [15:0][7:0]  fill_bytes,     // ... with these bytes
    output logic [1:0][27:0]  half_block,
    output logic [31:0][7:0]  bytes,
    // Each buffer byte's decode from L2 (fes_i386_lstage), by accumulator variant.
    output logic [31:0]       dec_prefix,
    output logic [31:0]       dec_run,
    output logic [31:0][15:0] dec_len,
    output logic [31:0][19:0] dec_fin,
    output logic [31:0][3:0]  dec_far
);
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
    assign f2_advance = active && (!l1_valid || l1_advance);
    assign filling = {l2_fire && half, l2_fire && !half};
    assign fill_bytes = l2_bytes;
    assign mem_load = redirect || f2_advance;
    assign mem_block = redirect ? redirect_linear[31:4] : fetch_block;

    // L1: the decoders on F2's block behind the previous block's bytes 12-15.
    logic [15:0]      new_prefix, new_run, new_opsize, new_adsize, new_z, new_moffs;
    logic [15:0][2:0] new_head, new_mb32;
    logic [15:0][1:0] new_mb16;

    fes_i386_lendec l1 (
        .w({l1_bytes, l2_bytes[15:12]}), .prefix(new_prefix), .run(new_run), .opsize(new_opsize),
        .adsize(new_adsize), .head(new_head), .mb16(new_mb16), .mb32(new_mb32), .z(new_z), .moffs(new_moffs)
    );

    // L2: lengths and next starts for every size variant.
    logic [15:0][15:0] new_len;
    logic [15:0][19:0] new_fin;
    logic [15:0][3:0]  new_far;

    fes_i386_lstage l2 (
        .code32, .run(l2_run), .opsize(l2_opsize), .adsize(l2_adsize), .head(l2_head), .mb16(l2_mb16),
        .mb32(l2_mb32), .z(l2_z), .moffs(l2_moffs), .len(new_len), .fin(new_fin), .far(new_far)
    );

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
