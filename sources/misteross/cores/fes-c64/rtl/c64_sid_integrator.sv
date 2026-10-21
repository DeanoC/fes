// SPDX-License-Identifier: GPL-2.0-or-later
// Diagnostic translation of reSID's solve_integrate_6581, copyright Dag Lem.
// Unintegrated MOS6581 dt=1 kernel with bounded compressed model tables.
module c64_sid_integrator(input wire clk,reset,start,input wire signed [31:0] vi,vx,vc,
 input wire [31:0] vw_squared,output reg done=0,output wire busy,
 output wire term_request,output wire [15:0] term_address_out,input wire [15:0] term_value,input wire term_valid,
 output reg signed [31:0] new_vx,new_vc,vo);
 reg [2:0] state=0;
 reg signed [31:0] in_vi,in_vx,in_vc,snake,charge;
 reg [15:0] kg,term_gs;
 wire [31:0] gst=32'd65535-vx,gdt=32'd65535-vi;
 wire [31:0] gst_sq=gst*gst,gdt_sq=gdt*gdt;
 wire signed [31:0] square_difference=gst_sq-gdt_sq;
 wire [31:0] kg_address=(vw_squared+(gdt_sq>>1))>>16;
 wire [15:0] kg_value,op_value;
 wire kg_valid,op_valid;
 wire signed [31:0] vgs=$signed({16'd0,kg_value})-in_vx;
 wire signed [31:0] vgd=$signed({16'd0,kg})-in_vi;
 wire [20:0] term_address=(state==1) ? (vgs<0 ? 21'd0 : vgs[20:0]) : (vgd<0 ? 21'd0 : vgd[20:0]);
 wire [31:0] vcr_current=({16'd0,term_gs}-{16'd0,term_value})<<15;
 wire signed [31:0] next_charge=in_vc-snake-vcr_current;
 wire signed [31:0] op_address=(next_charge>>>15)+32'sd32768;
 assign busy=state!=0;
 c64_sid_filter_table #(.FAMILY(4)) k(.clk(clk),.reset(reset),.request(state==0 && start),.address(kg_address[20:0]),.value(kg_value),.valid(kg_valid));
 assign term_request=(state==1 && kg_valid)||state==2;assign term_address_out=term_address[15:0];
 c64_sid_filter_table #(.FAMILY(3)) o(.clk(clk),.reset(reset),.request(state==4 && term_valid),.address(op_address[20:0]),.value(op_value),.valid(op_valid));
 always @(posedge clk) begin
  done<=0;
  if(reset)begin state<=0;done<=0;new_vx<=0;new_vc<=0;vo<=0;end else case(state)
   0:if(start)begin in_vi<=vi;in_vx<=vx;in_vc<=vc;snake<=32'sd15*(square_difference>>>15);state<=1;end
   1:if(kg_valid)begin kg<=kg_value;state<=2;end
   2:state<=3;
   3:if(term_valid)begin term_gs<=term_value;state<=4;end
   4:if(term_valid)begin charge<=next_charge;state<=5;end
   5:if(op_valid)begin new_vx<=$signed({16'd0,op_value});new_vc<=charge;vo<=$signed({16'd0,op_value})+(charge>>>14);done<=1;state<=0;end
   default:state<=0;
  endcase
 end
endmodule
