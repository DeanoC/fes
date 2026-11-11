module neg_a_ternary (input logic [2:0] r, input logic n, output logic signed [3:0] y);
    always_comb y = n ? signed'({1'b0, r}) : 4'sd0;
endmodule
