// SPDX-License-Identifier: MIT
// The return stack (docs/i386-cpu-design.md, "Prediction"): sixteen linear
// return addresses in one MLAB, and a pointer to the top one.
//
// F2 moves it as it predicts (fes_i386_fetchbuf): a BTB entry for a near CALL
// pushes the address after the call's last byte, a cycle after F2 passes on
// its block (push, push_lin); one for a RET pops, and fetch goes to the top
// it pops (top). Both are speculative: on every redirect, which flushes
// everything younger than WB, WB sets the pointer to the count of near CALLs
// less near RETs it has committed (set_ptr), and writes the return address
// of a near CALL that the front end did not push, whose redirect this is
// (set_wr, set_lin). An entry a wrong path overwrote is not restored: it
// costs a later misprediction, never a result, as EX checks every predicted
// RET's target (fes_i386_cpu, "Branch prediction"). The pointer wraps, so
// a seventeenth CALL overwrites the oldest entry, and a RET past the
// bottom predicts the entry the wrap gives.
//
// The stack has one write port, so Quartus can map it to an MLAB with an
// asynchronous read (#1581 S1d, docs/validation/2026-11-10-1581-mlab-inference).
// A redirect's write and a push's never fire together, since a redirect takes
// priority over a push, and neither fires during reset. The read is not checked
// against a write to the same entry (no_rw_check): that happens only on a
// redirect, and the front end does not use top in a redirect's cycle
// (fes_i386_fetchbuf).
module fes_i386_ras (
    input  logic        clk,
    input  logic        reset,
    input  logic        redirect,
    input  logic [3:0]  set_ptr,
    input  logic        set_wr,
    input  logic [31:0] set_lin,
    input  logic        push,
    input  logic [31:0] push_lin,
    input  logic        pop,
    output logic [31:0] top
);
    (* ramstyle = "MLAB, no_rw_check" *) logic [31:0] stack [0:15];
    logic [3:0]  ptr;

    logic        we;
    logic [3:0]  wa;
    logic [31:0] wd;

    assign we  = !reset && (redirect ? set_wr : push);
    assign wa  = redirect ? set_ptr : ptr + 4'd1;
    assign wd  = redirect ? set_lin : push_lin;
    assign top = stack[ptr];

    always_ff @(posedge clk) begin
        if (we) stack[wa] <= wd;
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            ptr <= 4'd0;
        end else if (redirect) begin
            ptr <= set_ptr;
        end else if (push) begin
            ptr <= ptr + 4'd1;
        end else if (pop) begin
            ptr <= ptr - 4'd1;
        end
    end
endmodule
