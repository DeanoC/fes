// SPDX-License-Identifier: MIT
// Standalone DE25 decoded x87 arithmetic backend. In-order completion buffer
// decouples the II=1 fast pipeline from the iterative divide/square-root path.
// No operation changes CPU/x87 architectural state here. See ../README.md.
module fes_x87_pipe (
    input logic clk, reset, flush,
    input logic req_valid,
    output logic req_ready,
    input logic [2:0] req_op,
    input logic [79:0] req_a, req_b,
    input logic [1:0] req_pc, req_rc,
    input logic [7:0] req_tag,
    output logic resp_valid,
    input logic resp_ready,
    output logic [79:0] resp_value,
    output logic [5:0] resp_flags,
    output logic [2:0] resp_cc,
    output logic resp_incremented,
    output logic [7:0] resp_tag
);
    import fes_x87_pkg::*;
    request_t request, encoded, normalized;
    logic [5:0] shift_a, shift_b;
    // This is the external request timing boundary. Let Quartus retime the
    // arithmetic stages, but keep normalization behind the captured request.
    (* preserve, dont_retime *) request_t dispatch;
    raw_t fast_raw, slow_raw, round_raw;
    result_t rounded;
    logic fast_valid, slow_valid, slow_ready, round_valid;
    logic [3:0] allocate_ptr, retire_ptr, read_ptr;
    logic [4:0] occupied;
    logic [15:0] complete;
    logic accept, retire, is_slow, dispatch_valid, encoded_valid, normalized_valid;
    logic normalized_slow, slow_reserved;
    logic output_valid;
    // Single writer after arbitration, synchronous read: one M20K-shaped
    // completion RAM rather than a wide result mux or a flip-flop FIFO.
    (* ramstyle = "M20K" *) logic [89:0] values [0:15];
    logic [89:0] output_data;
    logic [7:0] tags [0:15];
    logic [7:0] output_tag;

    assign is_slow = req_op == OP_DIV || req_op == OP_SQRT;
    assign normalized_slow = normalized.op == OP_DIV || normalized.op == OP_SQRT;
    // Reserve the slow lane from admission through completion arbitration,
    // including every registered front-end stage before its busy bit rises.
    assign req_ready = !reset && !flush && occupied < 16 && req_op != 3'd7 &&
        (!is_slow || (slow_ready && !slow_reserved));
    assign accept = req_valid && req_ready;
    assign resp_valid = output_valid && !reset && !flush;
    assign retire = resp_valid && resp_ready;
    assign {resp_value,resp_flags,resp_cc,resp_incremented} = output_data;
    assign resp_tag = output_tag;
    assign read_ptr = retire ? retire_ptr + 4'd1 : retire_ptr;
    always_comb begin
        request.op = op_t'(req_op);
        request.a = req_a;
        request.b = req_b;
        request.pc = req_pc;
        request.rc = req_rc;
        request.slot = allocate_ptr;
        request.ua = '0;
        request.ub = '0;
        round_raw = fast_valid ? fast_raw : slow_raw;
    end
    fes_x87_fast fast (
        .clk, .reset, .flush, .in_valid(normalized_valid && !normalized_slow), .req(normalized),
        .out_valid(fast_valid), .res(fast_raw)
    );
    fes_x87_slow slow (
        .clk, .reset, .flush, .in_valid(normalized_valid && normalized_slow), .in_ready(slow_ready), .req(normalized),
        .out_valid(slow_valid), .out_ready(!fast_valid), .res(slow_raw)
    );
    fes_x87_round rounding (
        .clk, .reset, .flush, .in_valid(fast_valid || slow_valid), .raw(round_raw),
        .out_valid(round_valid), .res(rounded)
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
    always_ff @(posedge clk) begin
        // Read at the next head on a retirement: adjacent completed results
        // can leave on consecutive clocks. Valid follows the synchronous read.
        output_data <= values[read_ptr];
        output_tag <= tags[read_ptr];
        if (reset || flush) begin
            allocate_ptr <= 0;
            retire_ptr <= 0;
            occupied <= 0;
            complete <= 0;
            output_valid <= 0;
        end else begin
            output_valid <= complete[read_ptr];
            case ({accept,retire})
                2'b10: occupied <= occupied + 5'd1;
                2'b01: occupied <= occupied - 5'd1;
                default: ;
            endcase
            if (accept) begin
                tags[allocate_ptr] <= req_tag;
                allocate_ptr <= allocate_ptr + 4'd1;
            end
            if (retire) begin
                complete[retire_ptr] <= 0;
                retire_ptr <= retire_ptr + 4'd1;
            end
            if (round_valid) begin
                complete[rounded.slot] <= 1;
            end
        end
    end
    // Keep reset/flush out of the RAM itself. The valid bits make old data
    // unreachable; no reset fanout or RAM clear loop is required.
    always_ff @(posedge clk) begin
        if (round_valid && !reset && !flush)
            values[rounded.slot] <= {rounded.value,rounded.flags,rounded.cc,rounded.incremented};
    end
endmodule
