// SPDX-License-Identifier: GPL-2.0-or-later
module sid_coupling_sim_top (
 input wire clk,reset,phi,cs,we,
 input wire [4:0] addr,input wire [7:0] din,
 output wire [23:0] phase[0:2],
 output wire [15:0] freq[0:2],reset_count[0:2],
 output wire [11:0] pw[0:2],pulse[0:2],waveform[0:2],
 output wire [7:0] control[0:2],osc[0:2],
 output wire [22:0] lfsr[0:2],
 output wire [1:0] shift_pipe[0:2],
 output wire [17:0] fade_count[0:2],
 output wire [2:0] rise
);
 c64_sid dut(.clk(clk),.reset(reset),.phi(phi),.cs(cs),.we(we),.addr(addr),.din(din),.dout(),.sample());
 for(genvar v=0;v<3;v=v+1) begin: observe
  assign phase[v]=dut.envelopes[v].oscillator.q.phase;
  assign freq[v]=dut.envelopes[v].oscillator.q.freq;
  assign pw[v]=dut.envelopes[v].oscillator.q.pw;
  assign control[v]=dut.envelopes[v].oscillator.q.control;
  assign lfsr[v]=dut.envelopes[v].oscillator.q.lfsr;
  assign shift_pipe[v]=dut.envelopes[v].oscillator.q.shift_pipe;
  assign reset_count[v]=dut.envelopes[v].oscillator.q.reset_count;
  assign fade_count[v]=dut.envelopes[v].oscillator.q.fade_count;
  assign pulse[v]=dut.envelopes[v].oscillator.q.pulse;
  assign waveform[v]=dut.digital_wave[v];
  assign osc[v]=dut.oscillator_read[v];
  assign rise[v]=dut.envelopes[v].oscillator.q.msb_rising;
 end
endmodule
