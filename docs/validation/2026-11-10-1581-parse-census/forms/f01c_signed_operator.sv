// operator.sv:230 form: signed'() on elements of a packed 2-D array, then <<<.
module f01c_signed_operator (input logic [1:0][13:0] fb, input logic [2:0] sh,
                             output logic signed [21:0] y, output logic ok);
    always_comb y = sh == 0 ? 0 : (signed'(fb[0]) + signed'(fb[1])) <<< sh;
    assign ok = (y == (sh == 0 ? 0 : ($signed(fb[0]) + $signed(fb[1])) <<< sh));
endmodule
