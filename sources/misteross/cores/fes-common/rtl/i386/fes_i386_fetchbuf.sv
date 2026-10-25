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
// the address registered on the last edge with mem_load set, once mem_valid
// says it has arrived. F2 waits for it, and the address stays loaded until F2
// takes the block; a redirect replaces a request still waiting.
//
// avail[j][v] says Q may issue or consume the instruction at buffer byte j
// under accumulator variant v: byte j's half is valid, its bytes 12-15 have
// been decoded if j is one of them, and if the instruction ends past its half,
// the next half is valid or being filled. It is registered from next-cycle
// state, so Q's loop reads one register. It ignores Q's release of a half on
// this edge: Q's start has then left that half and cannot read it next cycle.
// For the same reason the next half's term assumes a release whenever Q is in
// that half now.
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
    input  logic [1:0]        release_half,   // Q left this half on this edge
    input  logic              q_half,         // the half Q's start is in
    output logic [1:0]        half_valid,
    output logic [15:0][7:0]  fill_bytes,     // the bytes L2 writes on this edge
    output logic [1:0][27:0]  half_block,
    output logic [31:0][7:0]  bytes,
    // Each buffer byte's decode from L2 (fes_i386_lstage), by accumulator variant.
    output lbyte_t [31:0]     dec,
    output logic [31:0][3:0]  avail
);
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
    lclass_t [15:0]   l2_cls;
    logic             half, l2_fire, l1_advance, f2_advance;

    assign half = l2_block[0];
    assign l2_fire = l2_valid && !half_valid[half];
    assign l1_advance = l1_valid && (!l2_valid || l2_fire);
    assign f2_advance = active && mem_valid && (!l1_valid || l1_advance);
    assign filling = {l2_fire && half, l2_fire && !half};
    assign fill_bytes = l2_bytes;
    assign mem_load = redirect || f2_advance;
    assign mem_block = redirect ? redirect_linear[31:4] : fetch_block;

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
        assign far_next = written ? new_dec[SLOT].past_half : dec[j].past_half;
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
            l2_cls <= new_cls;
        end

        if (l2_fire) begin
            half_block[half] <= l2_block;
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
