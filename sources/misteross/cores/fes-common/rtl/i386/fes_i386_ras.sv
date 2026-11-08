// SPDX-License-Identifier: MIT
// The return stack (docs/i386-cpu-design.md, "Prediction"): sixteen linear
// return addresses in flip-flops, and a pointer to the top one.
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
    logic [31:0] stack [0:15];
    logic [3:0]  ptr;

    assign top = stack[ptr];

    always_ff @(posedge clk) begin
        if (reset) begin
            ptr <= 4'd0;
        end else if (redirect) begin
            ptr <= set_ptr;
            if (set_wr) stack[set_ptr] <= set_lin;
        end else if (push) begin
            ptr <= ptr + 4'd1;
            stack[ptr + 4'd1] <= push_lin;
        end else if (pop) begin
            ptr <= ptr - 4'd1;
        end
    end
endmodule
