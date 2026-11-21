// SPDX-License-Identifier: MIT
// The DE25 x87 arithmetic core: the shared normalising front end, the II=1
// fast lane, the iterative divide/square-root lane and the arbitration
// between them, with the result delivered **before rounding**.
//
// This is the body of what PR #1892 delivered as `fes_x87_pipe`, lifted out
// so that it has two consumers rather than one:
//
//   - `fes_x87_pipe` wraps it with `fes_x87_round` and a 16-entry completion
//     RAM, which is the standalone backend #1892 fitted and validated: tagged,
//     rounded, masked results in issue order under backpressure.
//   - `fes_i386_x87` wraps it with
//     nothing at all and takes `raw` straight into its own A_RND state.
//
// The second consumer is the reason the split exists. `fes_x87_round` rounds
// at the bounded exponent: it substitutes infinity or the largest finite value
// on overflow, and it denormalises *before* rounding on underflow. Both are
// correct for a masked result and both destroy what an **unmasked** overflow
// or underflow needs, which is the value rounded at the unbounded exponent
// with its exponent biased by -24576 or +24576 (387 PRM section 3.1.7, and
// slice 3's measured rules). `fes_i386_x87`'s A_RND already does exactly
// that, reading OM and UM out of the control word, and it is the side the
// oracle lockstep verifies. So the CPU consumer must tap the core ahead of
// the rounder, not adapt its response afterwards.
//
// The two representations meet without repacking, which is what makes the
// seam cheap. `raw_t` carries {sign, signed [17:0] exponent biased by 16383,
// significand[66:0] with bit 66 the integer bit and bits 2, 1 and 0 the
// guard, round and sticky}, and A_RND consumes {wsign, we, wm[63:0], wg,
// wrd, ws} with the same bias and the same bit order: wm is
// significand[66:3], and wg, wrd and ws are bits 2, 1 and 0.
//
// The lanes raise IE, DE and ZE only. PE, UE and OE are the rounding's, and
// belong to whichever rounder the consumer uses; neither lane sets them, so
// no consumer counts them twice.
//
// PIPELINED is the fast lane's (fes_x87_fast): 1 keeps its seven stages and
// its throughput of one operation a clock, which `fes_x87_pipe` needs; 0
// makes it one registered stage, for `fes_i386_x87`, which has one
// operation in flight (#2027 slice 6).
module fes_x87_core #(
    parameter bit PIPELINED = 1'b1
) (
    input  logic clk, reset, flush,
    // Admission. `ext_ready` is the consumer's own back pressure: the
    // standalone wrapper holds it low at sixteen outstanding operations, and
    // the CPU, which has one ESC instruction in flight, ties it high.
    input  logic req_valid,
    input  logic ext_ready,
    output logic req_ready,
    input  logic [2:0] req_op,
    input  logic [79:0] req_a, req_b,
    input  logic [1:0] req_pc, req_rc,
    input  logic [3:0] req_slot,
    // The result, before rounding. There is no back pressure here: a
    // consumer that cannot take `raw` in the cycle `raw_valid` is set must
    // register it, as `fes_x87_pipe`'s rounder does.
    output logic raw_valid,
    output fes_x87_pkg::raw_t raw
);
    import fes_x87_pkg::*;
    request_t request, encoded, normalized;
    logic [5:0] shift_a, shift_b;
    // This is the external request timing boundary. Let Quartus retime the
    // arithmetic stages, but keep normalization behind the captured request.
    (* preserve, dont_retime *) request_t dispatch;
    raw_t fast_raw, slow_raw;
    logic fast_valid, slow_valid, slow_ready;
    logic accept, is_slow, dispatch_valid, encoded_valid, normalized_valid;
    logic normalized_slow, slow_reserved;

    assign is_slow = req_op == OP_DIV || req_op == OP_SQRT;
    assign normalized_slow = normalized.op == OP_DIV || normalized.op == OP_SQRT;
    // Reserve the slow lane from admission through completion arbitration,
    // including every registered front-end stage before its busy bit rises.
    assign req_ready = !reset && !flush && ext_ready && req_op != 3'd7 &&
        (!is_slow || (slow_ready && !slow_reserved));
    assign accept = req_valid && req_ready;
    // The fast lane wins the single result port; a waiting slow result takes
    // the next hole in the fast stream. Admission back pressure is what stops
    // the slow lane starving, so it is the consumer's to supply.
    assign raw_valid = (fast_valid || slow_valid) && !reset && !flush;
    assign raw = fast_valid ? fast_raw : slow_raw;
    always_comb begin
        request.op = op_t'(req_op);
        request.a = req_a;
        request.b = req_b;
        request.pc = req_pc;
        request.rc = req_rc;
        request.slot = req_slot;
        request.ua = '0;
        request.ub = '0;
    end
    fes_x87_fast #(.PIPELINED(PIPELINED)) fast (
        .clk, .reset, .flush, .in_valid(normalized_valid && !normalized_slow), .req(normalized),
        .out_valid(fast_valid), .res(fast_raw)
    );
    fes_x87_slow slow (
        .clk, .reset, .flush, .in_valid(normalized_valid && normalized_slow), .in_ready(slow_ready), .req(normalized),
        .out_valid(slow_valid), .out_ready(!fast_valid), .res(slow_raw)
    );
    always_ff @(posedge clk) begin
        // Isolate virtual/CPU request wiring from unpacking and classification.
        // Only valid is reset; operands need no enable or reset fanout.
        dispatch <= request;
        // Normalize once for both lanes: encode the shift, then apply it.
        encoded <= dispatch;
        shift_a <= normalization_shift(dispatch.a[63:0]);
        shift_b <= normalization_shift(dispatch.b[63:0]);
        normalized <= encoded;
        normalized.ua.sign <= encoded.a[79];
        normalized.ub.sign <= encoded.b[79];
        normalized.ua.significand <= encoded.a[63:0] << shift_a;
        normalized.ub.significand <= encoded.b[63:0] << shift_b;
        normalized.ua.exponent <= (encoded.a[78:64] == 0 ? 18'sd1 :
            $signed({3'd0,encoded.a[78:64]})) - $signed({12'd0,shift_a});
        normalized.ub.exponent <= (encoded.b[78:64] == 0 ? 18'sd1 :
            $signed({3'd0,encoded.b[78:64]})) - $signed({12'd0,shift_b});
        if (reset || flush) begin
            dispatch_valid <= 0;
            encoded_valid <= 0;
            normalized_valid <= 0;
            slow_reserved <= 0;
        end else begin
            dispatch_valid <= accept;
            encoded_valid <= dispatch_valid;
            normalized_valid <= encoded_valid;
            if (slow_valid && !fast_valid) slow_reserved <= 0;
            if (accept && is_slow) slow_reserved <= 1;
        end
    end
endmodule
