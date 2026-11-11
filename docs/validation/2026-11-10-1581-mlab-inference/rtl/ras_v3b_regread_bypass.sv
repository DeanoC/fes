// SPDX-License-Identifier: MIT
// Spike S1d variant 3b (#1581): ONE write port and a REGISTERED read at the
// NEXT pointer, with the write bypassed into the output register. Not
// production code.
//
// Every write goes to the entry the pointer moves to (push writes ptr+1 and
// moves there; a redirect writes set_ptr and moves there), so "the new top
// is the write data whenever there is a write" is the whole bypass. top is
// then the current form's top in every cycle after the first clock edge:
// a registered read with no latency change for the CPU.
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
    logic [3:0]  ptr, ptr_next;

    logic        we;
    logic [3:0]  wa;
    logic [31:0] wd;
    assign we = !reset && (redirect ? set_wr : push);
    assign wa = redirect ? set_ptr : ptr + 4'd1;
    assign wd = redirect ? set_lin : push_lin;

    always_comb begin
        ptr_next = ptr;
        if (reset) ptr_next = 4'd0;
        else if (redirect) ptr_next = set_ptr;
        else if (push) ptr_next = ptr + 4'd1;
        else if (pop) ptr_next = ptr - 4'd1;
    end

    always_ff @(posedge clk) begin
        if (we) stack[wa] <= wd;
        top <= we ? wd : stack[ptr_next];
        ptr <= ptr_next;
    end
endmodule
