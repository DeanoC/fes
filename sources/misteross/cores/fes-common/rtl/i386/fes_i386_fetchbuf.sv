// SPDX-License-Identifier: MIT
// F1, F2, L and the 32-byte fetch buffer (docs/i386-cpu-design.md, "Fetch and
// L1I" and "Length decode and alignment"). There is no L1I yet: the memory
// port stands for its way-selected output.
//
// F1 picks the next block address (sequential or redirect) into the memory's
// address register. F2 registers the returned 16 bytes; nothing decodes M10K
// data in F2. L runs the per-byte length decoders on the registered block
// (fes_i386_lstage) and writes block and decode into its half of the buffer
// once Q has released that half.
//
// The buffer is two 16-byte halves; buffer byte j holds linear byte j mod 32
// of the block in its half. A decoder needs four bytes after its own, so a
// block's bytes 0-11 are decoded as it passes L and its bytes 12-15 when the
// next sequential block does: L sees the other half's last four bytes and
// then its own block.
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
    // Each buffer byte's decode from L (fes_i386_lstage), by accumulator variant.
    output logic [31:0]       dec_prefix,
    output logic [31:0]       dec_run,
    output logic [31:0][15:0] dec_len,
    output logic [31:0][19:0] dec_fin,
    output logic [31:0][3:0]  dec_far
);
    logic             active;       // F2 holds a block; idle from reset to the first redirect
    logic [27:0]      f2_block;     // the block whose address the memory holds
    logic [27:0]      fetch_block;  // the block after f2_block
    logic             l_valid;      // the F2/L register
    logic [27:0]      l_block;
    logic [15:0][7:0] l_bytes;
    logic             half, l_fire, f2_advance;

    assign half = l_block[0];
    assign l_fire = l_valid && !half_valid[half];
    assign f2_advance = active && (!l_valid || l_fire);
    assign filling = {l_fire && half, l_fire && !half};
    assign fill_bytes = l_bytes;
    assign mem_load = redirect || f2_advance;
    assign mem_block = redirect ? redirect_linear[31:4] : fetch_block;

    logic [19:0][7:0]  window;
    logic [15:0]       new_prefix, new_run;
    logic [15:0][15:0] new_len;
    logic [15:0][19:0] new_fin;
    logic [15:0][3:0]  new_far;

    assign window = {l_bytes, half ? bytes[15:12] : bytes[31:28]};
    fes_i386_lstage l_stage (
        .w(window), .code32, .prefix(new_prefix), .run(new_run), .len(new_len), .fin(new_fin), .far(new_far)
    );

    always_ff @(posedge clk) begin
        if (reset) begin
            active <= 1'b0;
            l_valid <= 1'b0;
            half_valid <= 2'b00;
            tail_ok <= 2'b00;
        end else if (redirect) begin
            active <= 1'b1;
            f2_block <= redirect_linear[31:4];
            fetch_block <= redirect_linear[31:4] + 28'd1;
            l_valid <= 1'b0;
            half_valid <= 2'b00;
            tail_ok <= 2'b00;
        end else begin
            half_valid <= (half_valid & ~release_half) | filling;
            // A fill completes the other half's tail when that half holds the
            // previous block.
            tail_ok <= tail_ok & ~release_half & ~filling
                     | ({!half, half} & {2{l_fire}} & half_valid & ~release_half);
            if (f2_advance) begin
                l_valid <= 1'b1;
                f2_block <= fetch_block;
                fetch_block <= fetch_block + 28'd1;
            end else if (l_fire) begin
                l_valid <= 1'b0;
            end
        end

        if (f2_advance) begin
            l_block <= f2_block;
            l_bytes <= mem_data;
        end

        if (l_fire) begin
            half_block[half] <= l_block;
            // Slots 4-15 cover this block's bytes 0-11; slots 0-3 the other
            // half's bytes 12-15.
            if (half) begin
                bytes[31:16] <= l_bytes;
                {dec_prefix[27:16], dec_prefix[15:12]} <= new_prefix;
                {dec_run[27:16], dec_run[15:12]} <= new_run;
                {dec_len[27:16], dec_len[15:12]} <= new_len;
                {dec_fin[27:16], dec_fin[15:12]} <= new_fin;
                {dec_far[27:16], dec_far[15:12]} <= new_far;
            end else begin
                bytes[15:0] <= l_bytes;
                {dec_prefix[11:0], dec_prefix[31:28]} <= new_prefix;
                {dec_run[11:0], dec_run[31:28]} <= new_run;
                {dec_len[11:0], dec_len[31:28]} <= new_len;
                {dec_fin[11:0], dec_fin[31:28]} <= new_fin;
                {dec_far[11:0], dec_far[31:28]} <= new_far;
            end
        end
    end
endmodule
