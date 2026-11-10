// An enum cast of a variable.
module f06b_enum_cast import census_pkg::*; (input logic [1:0] code, output state_t st, output logic ok);
    assign st = state_t'(code);
    assign ok = (2'(st) == code);
endmodule
