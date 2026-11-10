// SPDX-License-Identifier: MIT
// Spike S1d harness (#1581): the same for every variant. Every return-stack
// input comes from a register and top goes to a register, so each timed path
// is register to register and no port is pruned. The inputs shift in from
// one pin and top folds into a rotating signature that leaves on one pin, so
// the build has three pins and no I/O path is in the Fmax.
//
// top reaches its register through a two-way select, as it does in the CPU
// (fes_i386_fetchbuf: btb_target = btb_entry.ret ? ras_top : btb_entry.target).
// Without it Quartus packs top_q into the inferred RAM as its output
// register, which the CPU's use of top does not allow.
module s1d_harness (
    input  logic clk,
    input  logic din,
    output logic dout
);
    // reset, redirect, set_ptr, set_wr, set_lin, push, push_lin, pop
    logic [72:0] sr;
    logic [31:0] top, top_q, sig;

    always_ff @(posedge clk) begin
        sr    <= {sr[71:0], din};
        top_q <= sr[0] ? top : sr[32:1];
        sig   <= {sig[30:0], sig[31]} ^ top_q;
    end
    assign dout = sig[31];

    fes_i386_ras ras (
        .clk,
        .reset(sr[72]), .redirect(sr[71]), .set_ptr(sr[70:67]), .set_wr(sr[66]), .set_lin(sr[65:34]),
        .push(sr[33]), .push_lin(sr[32:1]), .pop(sr[0]),
        .top
    );
endmodule
