// SPDX-License-Identifier: MIT
module c64_sid_sim_top (
 input wire clk, reset, phi, cs, we,
 input wire [4:0] addr,
 input wire [7:0] din,
 output wire [7:0] dout, e0, e1, e2,
 output wire signed [15:0] sample
);
 c64_sid sid(.*);
 assign e0=sid.env[0];
 assign e1=sid.env[1];
 assign e2=sid.env[2];
endmodule
