// SPDX-License-Identifier: GPL-2.0-or-later
// reSID source-oracle replay; see ../rtl/c64_sid.COPYING and the core README.
module oscillator_sim_top (
 input wire clk,reset,phi,we,
 input wire [2:0] addr,
 input wire [7:0] din,
 output wire [11:0] waveform,pulse,pw,
 output wire [7:0] osc,control,
 output wire [23:0] phase,
 output wire [22:0] lfsr,
 output wire [15:0] freq,reset_count,
 output wire [17:0] fade_count,
 output wire [1:0] shift_pipe
);
 c64_sid_oscillator dut(.clk(clk),.reset(reset),.phi(phi),.we(we),.addr(addr),.din(din),.waveform(waveform),.osc(osc));
 assign phase=dut.q.phase;
 assign freq=dut.q.freq;
 assign pw=dut.q.pw;
 assign control=dut.q.control;
 assign lfsr=dut.q.lfsr;
 assign shift_pipe=dut.q.shift_pipe;
 assign reset_count=dut.q.reset_count;
 assign fade_count=dut.q.fade_count;
 assign pulse=dut.q.pulse;
endmodule
