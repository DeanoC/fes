package h_pkg_int_12_nocast_pkg; localparam int D = 12; localparam int S = 16; localparam SH = (D - S - 2) < 0 ? 0 : D - S - 3; endpackage
module h_pkg_int_12_nocast import h_pkg_int_12_nocast_pkg::*; (output logic [7:0] v, output logic ok); assign v = 8'(SH); assign ok = (SH == 0); endmodule
