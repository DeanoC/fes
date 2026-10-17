// SPDX-License-Identifier: GPL-2.0-or-later
// Default C64 SID audio stage: voice DAC/envelope products, a Chamberlin
// state-variable filter and the master volume, evaluated once per Phi by a
// small sequencer with one shared nibble-serial multiplier. Routing follows
// the MOS6581 registers: RES/FILT selects the filtered voices, MODE/VOL
// selects LP/BP/HP outputs, the 3OFF bit mutes an unfiltered voice 3 and the
// external input is absent. Coefficients come from c64_sid_svf_tables.vh.
// This is a listening approximation; the reSID translation is c64_sid_audio.
module c64_sid_svf(input wire clk,reset,start,
 input wire [11:0] wave0,wave1,wave2,input wire [7:0] env0,env1,env2,
 input wire [10:0] fc,input wire [7:0] res_filt,mode_vol,
 output reg done=0,output wire busy,output reg signed [31:0] sample);
 `include "c64_sid_svf_tables.vh"
 localparam [3:0] IDLE=0,DAMP=1,HIGH=2,BAND_MUL=3,BAND=4,LOW_MUL=5,LOW=6,MIX=7,MUL=8;
 reg [3:0] state=IDLE,after=IDLE;
 reg signed [27:0] lp=0,bp=0,hp=0,filtered=0,direct=0;
 reg [7:0] mode=0;reg [3:0] volume=0;
 // Registered coefficient lookups (synchronous ROM reads). fc and res_filt
 // are held by the caller from capture until the next Phi, so the values are
 // valid from the second edge after a change; the sequencer uses them later.
 // mem2reg keeps the tables in logic, bypassing the pinned Intel legacy
 // BRAM mapper that would otherwise infer an asynchronous M10K read.
 (* ram_style = "logic", mem2reg *) reg [17:0] cutoff_words[0:127];
 (* ram_style = "logic", mem2reg *) reg [20:0] damping_words[0:15];
 integer table_index;
 initial begin
  for(table_index=0;table_index<128;table_index=table_index+1)cutoff_words[table_index]=svf_cutoff_coefficient(7'(table_index));
  for(table_index=0;table_index<16;table_index=table_index+1)damping_words[table_index]=svf_damping_coefficient(4'(table_index));
 end
 reg [17:0] cutoff;reg [20:0] damping;
 always @(posedge clk)begin
  cutoff<=cutoff_words[fc[10:4]];
  damping<=damping_words[res_filt[7:4]];
 end
 // Shared multiplier: signed 28-bit operand by unsigned 21-bit coefficient,
 // four coefficient bits per edge, Q20 result in product[47:20].
 reg signed [27:0] mul_a=0;reg [23:0] mul_b=0;reg signed [51:0] product=0;reg [2:0] mul_step=0;
 wire signed [31:0] nibble_product=
  (mul_b[0]?{{4{mul_a[27]}},mul_a}:32'sd0)+(mul_b[1]?{{3{mul_a[27]}},mul_a,1'b0}:32'sd0)+
  (mul_b[2]?{{2{mul_a[27]}},mul_a,2'b0}:32'sd0)+(mul_b[3]?{mul_a[27],mul_a,3'b0}:32'sd0);
 wire signed [27:0] product_q20=product[47:20];
 assign busy=state!=IDLE;
 function automatic signed [27:0] voice(input [11:0] wave,input [7:0] envelope);
  logic signed [12:0] centered;
  begin
   centered=$signed({1'b0,wave})-13'sd2048;
   voice=28'(centered*$signed({1'b0,envelope}));
  end
 endfunction
 wire signed [27:0] v0=voice(wave0,env0),v1=voice(wave1,env1),v2=voice(wave2,env2);
 wire signed [27:0] mixed=direct+(mode[4]?lp:28'sd0)+(mode[5]?bp:28'sd0)+(mode[6]?hp:28'sd0);
 wire signed [31:0] scaled=(mixed*$signed({1'b0,volume}))>>>10;
 always @(posedge clk)begin
  done<=0;
  if(reset)begin
   state<=IDLE;lp<=0;bp<=0;hp<=0;filtered<=0;direct<=0;mode<=0;volume<=0;sample<=0;product<=0;mul_step<=0;
  end else case(state)
   IDLE:if(start)begin
    filtered<=(res_filt[0]?v0:28'sd0)+(res_filt[1]?v1:28'sd0)+(res_filt[2]?v2:28'sd0);
    direct<=(res_filt[0]?28'sd0:v0)+(res_filt[1]?28'sd0:v1)+((res_filt[2]||mode_vol[7])?28'sd0:v2);
    mode<=mode_vol;volume<=mode_vol[3:0];state<=DAMP;
   end
   DAMP:begin mul_a<=bp;mul_b<={3'd0,damping};product<=0;mul_step<=0;after<=HIGH;state<=MUL;end
   HIGH:begin hp<=filtered-lp-product_q20;state<=BAND_MUL;end
   BAND_MUL:begin mul_a<=hp;mul_b<={6'd0,cutoff};product<=0;mul_step<=0;after<=BAND;state<=MUL;end
   BAND:begin bp<=bp+product_q20;state<=LOW_MUL;end
   LOW_MUL:begin mul_a<=bp;mul_b<={6'd0,cutoff};product<=0;mul_step<=0;after<=LOW;state<=MUL;end
   LOW:begin lp<=lp+product_q20;state<=MIX;end
   MIX:begin
    if(scaled>32'sd32767)sample<=32'sd32767;
    else if(scaled< -32'sd32768)sample<= -32'sd32768;
    else sample<=scaled;
    done<=1;state<=IDLE;
   end
   MUL:begin
    product<=product+({{20{nibble_product[31]}},nibble_product}<<<(4*mul_step));
    mul_b<=mul_b>>4;mul_step<=mul_step+1'b1;
    if(mul_step==3'd5)state<=after;
   end
   default:state<=IDLE;
  endcase
 end
endmodule
