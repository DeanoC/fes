// Functional test endpoint only. Quartus uses the generated reset_release IP.
// The bench controls this signal to exercise delayed initialization and reset.
module reset_release (
    output reg ninit_done /* verilator public_flat_rw */
);
    initial ninit_done = 1'b1;
endmodule
