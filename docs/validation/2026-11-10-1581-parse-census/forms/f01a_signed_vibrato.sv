// vibrato.sv:73 form. ok is 1 iff the cast result is right.
module f01a_signed_vibrato (input logic [2:0] range, input logic neg, output logic signed [3:0] y, output logic ok);
    always_comb y = neg ? -signed'({1'b0, range}) : signed'({1'b0, range});
    assign ok = (y == (neg ? -$signed({1'b0, range}) : $signed({1'b0, range})));
endmodule
