// SPDX-License-Identifier: MIT
// F1, F2 and the 32-byte fetch buffer without L1I (docs/i386-cpu-design.md,
// "Fetch and L1I"). The buffer is two 16-byte halves; buffer byte j holds
// linear byte j mod 32 of the block in its half. F1 picks the next block
// address (sequential or redirect) into the memory's address register; F2
// writes the returned block into its half once Q has released that half.
//
// The 16 per-byte length decoders run on the block as F2 writes it. Decoder
// k needs four bytes after its own, so a block's bytes 0-11 are decoded as it
// arrives and its bytes 12-15 when the next sequential block does: the
// decoders see the other half's last four bytes and then the incoming block.
//
// The memory port models an M10K with address stall: mem_data is the block
// at the address registered on the last edge with mem_load set.
module fes_i386_fetchbuf (
    input  logic             clk,
    input  logic             reset,
    input  logic             redirect,
    input  logic [31:0]      redirect_linear,
    output logic [27:0]      mem_block,
    output logic             mem_load,
    input  logic [15:0][7:0] mem_data,
    input  logic [1:0]       release_half,   // Q left this half on this edge
    output logic [1:0]       half_valid,
    output logic [1:0]       tail_ok,        // the half's bytes 12-15 are decoded
    output logic [1:0]       filling,        // F2 writes this half on this edge
    output logic [1:0][27:0] half_block,
    output logic [31:0][7:0] bytes,
    // The per-byte length decode of each buffer byte (fes_i386_lendec).
    output logic [31:0]       dec_prefix,
    output logic [31:0]       dec_run,
    output logic [31:0]       dec_opsize,
    output logic [31:0]       dec_adsize,
    output logic [31:0][15:0] dec_len
);
    logic        active;       // F2 holds a block; idle from reset to the first redirect
    logic [27:0] f2_block;
    logic [27:0] fetch_block;  // the block after f2_block
    logic        half;
    logic        fill;

    assign half = f2_block[0];
    assign fill = active && !half_valid[half];
    assign filling = {fill && half, fill && !half};
    assign mem_load = redirect || fill;
    assign mem_block = redirect ? redirect_linear[31:4] : fetch_block;

    logic [19:0][7:0]  window;
    logic [15:0]       new_prefix, new_run, new_opsize, new_adsize;
    logic [15:0][15:0] new_len;

    assign window = {mem_data, half ? bytes[15:12] : bytes[31:28]};
    fes_i386_lendec decoders (
        .w(window), .prefix(new_prefix), .run(new_run), .opsize(new_opsize), .adsize(new_adsize), .len(new_len)
    );

    always_ff @(posedge clk) begin
        if (reset) begin
            active <= 1'b0;
            half_valid <= 2'b00;
            tail_ok <= 2'b00;
        end else if (redirect) begin
            active <= 1'b1;
            f2_block <= redirect_linear[31:4];
            fetch_block <= redirect_linear[31:4] + 28'd1;
            half_valid <= 2'b00;
            tail_ok <= 2'b00;
        end else begin
            half_valid <= (half_valid & ~release_half) | filling;
            // A fill completes the other half's tail when that half holds the
            // previous block.
            tail_ok <= tail_ok & ~release_half & ~filling
                     | ({!half, half} & {2{fill}} & half_valid & ~release_half);
            if (fill) begin
                f2_block <= fetch_block;
                fetch_block <= fetch_block + 28'd1;
            end
        end

        if (fill) begin
            half_block[half] <= f2_block;
            // Decoders 4-15 cover this block's bytes 0-11; decoders 0-3 the
            // other half's bytes 12-15.
            if (half) begin
                bytes[31:16] <= mem_data;
                {dec_prefix[27:16], dec_prefix[15:12]} <= new_prefix;
                {dec_run[27:16], dec_run[15:12]} <= new_run;
                {dec_opsize[27:16], dec_opsize[15:12]} <= new_opsize;
                {dec_adsize[27:16], dec_adsize[15:12]} <= new_adsize;
                {dec_len[27:16], dec_len[15:12]} <= new_len;
            end else begin
                bytes[15:0] <= mem_data;
                {dec_prefix[11:0], dec_prefix[31:28]} <= new_prefix;
                {dec_run[11:0], dec_run[31:28]} <= new_run;
                {dec_opsize[11:0], dec_opsize[31:28]} <= new_opsize;
                {dec_adsize[11:0], dec_adsize[31:28]} <= new_adsize;
                {dec_len[11:0], dec_len[31:28]} <= new_len;
            end
        end
    end
endmodule
