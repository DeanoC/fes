module neg_a_plain (input logic [2:0] r, input logic n, output logic signed [3:0] y);
    always_comb y = signed'({1'b0, r});
endmodule
