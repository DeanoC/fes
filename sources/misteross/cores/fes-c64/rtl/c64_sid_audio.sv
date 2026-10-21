// SPDX-License-Identifier: GPL-2.0-or-later
// Staged MOS6581 DAC, internal filter and external output network.
module c64_sid_audio(input wire clk,reset,start,
 input wire [11:0] wave0,wave1,wave2,input wire [7:0] env0,env1,env2,
 input wire [10:0] fc,input wire [7:0] res_filt,mode_vol,
 output reg done=0,output wire busy,output reg signed [31:0] sample,
 output wire signed [15:0] internal_sample,
 output wire signed [31:0] hp,bp,lp,bpx,bpc,lpx,lpc,external_low,external_high);
 wire [11:0] wd0,wd1,wd2;wire [7:0] ed0,ed1,ed2;
 c64_sid_dac d0(.wave(wave0),.envelope(env0),.wave_dac(wd0),.envelope_dac(ed0));
 c64_sid_dac d1(.wave(wave1),.envelope(env1),.wave_dac(wd1),.envelope_dac(ed1));
 c64_sid_dac d2(.wave(wave2),.envelope(env2),.wave_dac(wd2),.envelope_dac(ed2));
 reg signed [31:0] voice0,voice1,voice2;reg [10:0] cutoff;reg [7:0] routing,mode;
 reg [1:0] state=0;wire filter_done,filter_busy,output_done,output_busy;wire signed [31:0] output_sample;
 assign busy=state!=0;
 c64_sid_filter filter(.clk(clk),.reset(reset),.start(state==1),.voice1(voice0),.voice2(voice1),.voice3(voice2),.fc(cutoff),.res_filt(routing),.mode_vol(mode),.done(filter_done),.busy(filter_busy),.sample(internal_sample),.hp(hp),.bp(bp),.lp(lp),.bpx(bpx),.bpc(bpc),.lpx(lpx),.lpc(lpc));
 c64_sid_output_filter output_network(.clk(clk),.reset(reset),.start(state==2 && filter_done),.signal_in(internal_sample),.done(output_done),.busy(output_busy),.sample(output_sample),.low_state(external_low),.high_state(external_high));
 always @(posedge clk)begin
  done<=0;
  if(reset)begin state<=0;done<=0;sample<=0;voice0<=0;voice1<=0;voice2<=0;cutoff<=0;routing<=0;mode<=0;end else case(state)
   0:if(start)begin
    voice0<=($signed({20'd0,wd0})-32'sd896)*$signed({1'b0,ed0});
    voice1<=($signed({20'd0,wd1})-32'sd896)*$signed({1'b0,ed1});
    voice2<=($signed({20'd0,wd2})-32'sd896)*$signed({1'b0,ed2});
    cutoff<=fc;routing<=res_filt;mode<=mode_vol;state<=1;
   end
   1:state<=2;
   2:if(filter_done)state<=3;
   3:if(output_done)begin sample<=output_sample;done<=1;state<=0;end
   default:state<=0;
  endcase
 end
endmodule
