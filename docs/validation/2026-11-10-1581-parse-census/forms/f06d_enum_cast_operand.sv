// An enum cast of a variable used as an operand (f06c line 10), alone. f06b, where the cast is
// the whole right-hand side, passes Yosys.
module f06d_enum_cast_operand import census_pkg::*; (input logic [1:0] code, input state_t st, output logic eq, output logic ok);
    assign eq = (st == state_t'(code));
    assign ok = (eq == (2'(st) == code));
endmodule
