// The first f06 form: a size cast in a for loop and an enum cast of a variable in one module
// with an import and parameters. Both Yosys locks rejected it ("Static cast with non constant
// expression"); f06a and f06b alone pass. Kept to show which combination fails.
module f06c_casts_together import census_pkg::*; #(parameter int N = 8, parameter int IW = 3)
    (input logic [N-1:0] v, input logic [1:0] code, output logic [IW-1:0] slot, output state_t st, output logic ok);
    always_comb begin
        slot = '0;
        for (int k = N - 1; k >= 0; k--) if (!v[k]) slot = IW'(k);
    end
    assign st = state_t'(code);
    assign ok = (st == state_t'(code));
endmodule
