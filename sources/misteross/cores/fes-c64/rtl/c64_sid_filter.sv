// SPDX-License-Identifier: GPL-2.0-or-later
// Diagnostic staged translation of reSID MOS6581 filter (Dag Lem), default bias,
// enabled filter and three-voice mask7, no external input.
module c64_sid_filter(input wire clk,reset,start,
 input wire signed [31:0] voice1,voice2,voice3,
 input wire [10:0] fc,input wire [7:0] res_filt,mode_vol,
 output reg done=0,output wire busy,output reg signed [15:0] sample,
 output reg signed [31:0] hp,bp,lp,bpx,bpc,lpx,lpc);
 initial begin hp=0;bp=0;lp=0;bpx=0;bpc=0;lpx=0;lpc=0;end
 reg [3:0] state=0;assign busy=state!=0;
 reg signed [31:0] v1,v2,v3;
 reg [7:0] rf,mv;
 wire [19:0] cutoff_full;wire [15:0] cutoff=cutoff_full[15:0];
 c64_sid_filter_rom #(.FAMILY(6),.WORDS(2048)) fc_rom(.clk(clk),.address({10'd0,fc}),.value(cutoff_full));

 wire [31:0] cutoff_delta=32'd65535-{16'd0,cutoff};
 wire [31:0] cutoff_square=cutoff_delta*cutoff_delta;
 reg [31:0] vw_squared;
 wire idone1,idone2,ibusy1,ibusy2;
 wire tr1,tr2,tv1,tv2;wire [15:0] ta1,ta2,td1,td2;
 c64_sid_vcr_shared shared_vcr(.clk(clk),.reset(reset),.request_a(tr1),.request_b(tr2),.address_a(ta1),.address_b(ta2),.value_a(td1),.value_b(td2),.valid_a(tv1),.valid_b(tv2));
 wire signed [31:0] ox1,oc1,ov1,ox2,oc2,ov2;
 c64_sid_integrator low(.clk(clk),.reset(reset),.start(state==2),.vi(bp),.vx(lpx),.vc(lpc),.vw_squared(vw_squared),.done(idone1),.busy(ibusy1),.term_request(tr1),.term_address_out(ta1),.term_value(td1),.term_valid(tv1),.new_vx(ox1),.new_vc(oc1),.vo(ov1));
 c64_sid_integrator band(.clk(clk),.reset(reset),.start(state==2),.vi(hp),.vx(bpx),.vc(bpc),.vw_squared(vw_squared),.done(idone2),.busy(ibusy2),.term_request(tr2),.term_address_out(ta2),.term_value(td2),.term_valid(tv2),.new_vx(ox2),.new_vc(oc2),.vo(ov2));
 function automatic [3:0] pop(input [6:0] bits);integer i;begin pop=0;for(i=0;i<7;i=i+1)pop=pop+{3'd0,bits[i]};end endfunction
 function automatic [20:0] summer_offset(input [3:0] n);begin case(n)0:summer_offset=0;1:summer_offset=21'd131072;2:summer_offset=21'd327680;3:summer_offset=21'd589824;default:summer_offset=21'd917504;endcase end endfunction
 function automatic [20:0] mixer_offset(input [3:0] n);begin case(n)0:mixer_offset=0;1:mixer_offset=1;2:mixer_offset=21'd65537;3:mixer_offset=21'd196609;4:mixer_offset=21'd393217;5:mixer_offset=21'd655361;6:mixer_offset=21'd983041;default:mixer_offset=21'd1376257;endcase end endfunction
 wire [2:0] sum_mask=rf[2:0];
 wire [6:0] mix_mask={mv[6:4],1'b0,(~(rf[2:0]|(mv[7]?3'b100:3'b000)))};
 wire signed [31:0] sum_voices=(sum_mask[0]?v1:32'sd0)+(sum_mask[1]?v2:32'sd0)+(sum_mask[2]?v3:32'sd0);
 wire [15:0] gain_value,summer_value,mixer_value;
 wire gain_valid,summer_valid,mixer_valid;
 wire [20:0] gain_address=state==4 ? {1'b0,~rf[7:4],bp[15:0]} : {1'b0,mv[3:0],mixer_value};
 wire [31:0] summer_address={11'd0,summer_offset(pop({4'd0,sum_mask}))}+{16'd0,gain_value}+lp+sum_voices;
 wire signed [31:0] mix_values=(mix_mask[0]?v1:32'sd0)+(mix_mask[1]?v2:32'sd0)+(mix_mask[2]?v3:32'sd0)+(mix_mask[4]?lp:32'sd0)+(mix_mask[5]?bp:32'sd0)+(mix_mask[6]?$signed({16'd0,summer_value}):32'sd0);
 wire [31:0] mixer_address={11'd0,mixer_offset(pop(mix_mask))}+mix_values;
 c64_sid_filter_table #(.FAMILY(0)) gain_rom(.clk(clk),.reset(reset),.request((state==4)||(state==7 && mixer_valid)),.address(gain_address),.value(gain_value),.valid(gain_valid));
c64_sid_filter_table #(.FAMILY(1)) summer_rom(.clk(clk),.reset(reset),.request(state==5 && gain_valid),.address(summer_address[20:0]),.value(summer_value),.valid(summer_valid));
c64_sid_filter_table #(.FAMILY(2)) mixer_rom(.clk(clk),.reset(reset),.request(state==6 && summer_valid),.address(mixer_address[20:0]),.value(mixer_value),.valid(mixer_valid));

 always @(posedge clk)begin
  done<=0;
  if(reset)begin state<=0;done<=0;sample<=0;hp<=0;bp<=0;lp<=0;bpx<=0;bpc<=0;lpx<=0;lpc<=0;v1<=0;v2<=0;v3<=0;rf<=0;mv<=0;vw_squared<=0;end else case(state)
   0:if(start)begin
    v1<=((voice1*32'sd2442)>>>18)+32'sd27295;v2<=((voice2*32'sd2442)>>>18)+32'sd27295;v3<=((voice3*32'sd2442)>>>18)+32'sd27295;
    rf<=res_filt;mv<=mode_vol;state<=1;
   end
   1:begin vw_squared<=cutoff_square>>1;state<=2;end
   2:state<=3;
   3:if(idone1 && idone2)begin lp<=ov1;lpx<=ox1;lpc<=oc1;bp<=ov2;bpx<=ox2;bpc<=oc2;state<=4;end
   4:state<=5;
   5:if(gain_valid)state<=6;
   6:if(summer_valid)begin hp<=$signed({16'd0,summer_value});state<=7;end
   7:if(mixer_valid)state<=8;
   8:if(gain_valid)begin sample<=gain_value-16'd32768;done<=1;state<=0;end
   default:state<=0;
  endcase
 end
endmodule
