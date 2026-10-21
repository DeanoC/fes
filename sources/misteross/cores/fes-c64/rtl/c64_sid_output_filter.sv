// SPDX-License-Identifier: GPL-2.0-or-later
// dt1 external SID output network, derived from reSID ExternalFilter (Dag Lem).
// Unintegrated diagnostic; explicit 32-bit wrapping and arithmetic shifts.
module c64_sid_output_filter(input wire clk,reset,start,input wire signed [15:0] signal_in,
 output reg done=0,output wire busy,output reg signed [31:0] sample,
 output reg signed [31:0] low_state,high_state);
 reg [1:0] state=0;reg signed [31:0] delta_low,delta_high;
 initial begin low_state=0;high_state=0;end
 assign busy=state!=0;
 wire [31:0] input_scaled={{16{signal_in[15]}},signal_in}<<11;
 wire signed [31:0] low_difference=input_scaled-low_state;
 wire signed [31:0] high_difference=low_state-high_state;
 wire signed [31:0] low_product=32'sd12*low_difference;
 wire signed [31:0] high_product=32'sd13*high_difference;
 always @(posedge clk)begin
  done<=0;
  if(reset)begin state<=0;done<=0;sample<=0;low_state<=0;high_state<=0;end else case(state)
   0:if(start)begin delta_low<=low_product>>>7;delta_high<=high_product>>>17;state<=1;end
   1:begin low_state<=low_state+delta_low;high_state<=high_state+delta_high;state<=2;end
   2:begin sample<=(low_state-high_state)>>>11;done<=1;state<=0;end
   default:state<=0;
  endcase
 end
endmodule
