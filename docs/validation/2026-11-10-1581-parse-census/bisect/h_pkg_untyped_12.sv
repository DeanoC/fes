package h_pkg_untyped_12_pkg; localparam D = 12; localparam S = 16; localparam SH = signed'(D - S - 2) < 0 ? 0 : D - S - 3; endpackage
module h_pkg_untyped_12 import h_pkg_untyped_12_pkg::*; (output logic [7:0] v, output logic ok); assign v = 8'(SH); assign ok = (SH == 0); endmodule
