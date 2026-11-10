// SPDX-License-Identifier: MIT
// Spike S1d variant 2 (#1581): the return stack with ONE write port and the
// same asynchronous read. Not production code.
//
// The current form's two write sites never fire together (the redirect
// branch has priority over push) and neither fires in reset, so they fold
// into one port. Every cycle's ptr and top equal the current form's.
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
    assign we = !reset && (redirect ? set_wr : push);
    assign wa = redirect ? set_ptr : ptr + 4'd1;
    assign wd = redirect ? set_lin : push_lin;

    assign top = stack[ptr];

    always_ff @(posedge clk) begin
        if (we) stack[wa] <= wd;
    end

    always_ff @(posedge clk) begin
        if (reset) ptr <= 4'd0;
        else if (redirect) ptr <= set_ptr;
        else if (push) ptr <= ptr + 4'd1;
        else if (pop) ptr <= ptr - 4'd1;
    end
endmodule
