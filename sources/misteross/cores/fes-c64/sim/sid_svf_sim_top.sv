// SPDX-License-Identifier: GPL-2.0-or-later
module sid_svf_sim_top(input wire clk,reset,start,
 input wire [11:0] wave0,wave1,wave2,input wire [7:0] env0,env1,env2,
 input wire [10:0] fc,input wire [7:0] res_filt,mode_vol,
 output wire done,busy,output wire signed [31:0] sample,
 output wire signed [27:0] lp,bp,hp);
 c64_sid_svf dut(.*);
 assign lp=dut.lp;assign bp=dut.bp;assign hp=dut.hp;
endmodule
