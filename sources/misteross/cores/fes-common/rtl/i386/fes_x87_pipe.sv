// SPDX-License-Identifier: MIT
// Standalone DE25 decoded x87 arithmetic backend: `fes_x87_core` wrapped with
// the shared rounder and an in-order completion buffer, which decouples the
// II=1 fast pipeline from the iterative divide/square-root path. No operation
// changes CPU/x87 architectural state here. See
// ../../../../boards/de25nano/x87/README.md.
//
// The front end, the two arithmetic lanes and their arbitration moved into
// `fes_x87_core` so that `fes_i386_x87` can take the pre-rounding `raw_t`
// into its own A_RND state: the rounder below delivers a **masked** result,
// and an unmasked overflow or underflow needs the value rounded at the
// unbounded exponent, which this rounder has already spent. That split is
// why this module is a wrapper rather than the whole backend; the behaviour
// and the interface it presents are unchanged.
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
    raw_t raw;
    result_t rounded;
    logic raw_valid, round_valid, core_ready;
    logic [3:0] allocate_ptr, retire_ptr, read_ptr;
    logic [4:0] occupied;
    logic [15:0] complete;
    logic accept, retire;
    logic output_valid;
    // Single writer after arbitration, synchronous read: one M20K-shaped
    // completion RAM rather than a wide result mux or a flip-flop FIFO.
    (* ramstyle = "M20K" *) logic [89:0] values [0:15];
    logic [89:0] output_data;
    logic [7:0] tags [0:15];
    logic [7:0] output_tag;

    assign resp_valid = output_valid && !reset && !flush;
    assign retire = resp_valid && resp_ready;
    assign {resp_value,resp_flags,resp_cc,resp_incremented} = output_data;
    assign resp_tag = output_tag;
    assign read_ptr = retire ? retire_ptr + 4'd1 : retire_ptr;
    // Admission stops at sixteen outstanding operations, which is also what
    // stops a waiting slow result starving behind the fast stream.
    assign core_ready = occupied < 16;
    assign accept = req_valid && req_ready;

    fes_x87_core #(.PIPELINED(1'b1)) core (
        .clk, .reset, .flush,
        .req_valid, .ext_ready(core_ready), .req_ready,
        .req_op, .req_a, .req_b, .req_pc, .req_rc, .req_slot(allocate_ptr),
        .raw_valid, .raw
    );
    fes_x87_round rounding (
        .clk, .reset, .flush, .in_valid(raw_valid), .raw,
        .out_valid(round_valid), .res(rounded)
    );
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
