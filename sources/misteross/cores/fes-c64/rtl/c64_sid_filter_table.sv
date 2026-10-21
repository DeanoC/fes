// SPDX-License-Identifier: GPL-2.0-or-later
// Paged interpolation ROM; constants generated from pinned reSID.
module c64_sid_filter_table #(
 parameter integer FAMILY=0
)(input wire clk, input wire reset, input wire request, input wire [20:0] address,
 output reg [15:0] value, output wire valid);
 `include "c64_sid_filter_coefficients.vh"
 localparam integer PAGES=page_count(FAMILY), COEFFICIENTS=coefficient_count(FAMILY);
 localparam MIXER=(FAMILY==2);
 localparam [15:0] ZERO=(FAMILY==2 ? 16'd24299 : 16'd0);
 localparam integer CW=$clog2(COEFFICIENTS), DW=CW+4;
 localparam [COEFFICIENTS*16-1:0] COEFFICIENT_DATA=(COEFFICIENTS*16)'(coefficient_data(FAMILY));
 wire [19:0] descriptor_full;
 c64_sid_filter_rom #(.FAMILY(FAMILY),.WORDS(PAGES)) descriptor_rom(.clk(clk),.address(index>>8),.value(descriptor_full));
 (* ram_style="m10k_tdp" *) reg [15:0] coefficients[0:COEFFICIENTS-1];
 integer init_index;
 initial for(init_index=0;init_index<COEFFICIENTS;init_index=init_index+1) coefficients[init_index]=COEFFICIENT_DATA[init_index*16+:16];
 wire [20:0] index=(MIXER && address!=0) ? address-21'd1 : address;
 wire [DW-1:0] descriptor=descriptor_full[DW-1:0];
 reg [7:0] local_index;
 reg [CW-1:0] coefficient_address;
 reg [3:0] shift1,shift2;
 reg [8:0] fraction1,fraction2;
 reg [15:0] a,b;
 reg [3:0] valid_pipe=0,zero_pipe=0;
 wire [8:0] step2=9'd1<<shift2;
 wire signed [16:0] difference=$signed({1'b0,b})-$signed({1'b0,a});
 wire signed [26:0] product=difference*$signed({1'b0,fraction2});
 wire signed [27:0] rounded=product+28'($signed({1'b0,(step2>>1)}));
 wire signed [27:0] interpolated=28'($signed({1'b0,a}))+(rounded>>>shift2);
 assign valid=valid_pipe[3];
 always @(posedge clk) begin
  valid_pipe<=reset ? 4'd0 : {valid_pipe[2:0],request};
  zero_pipe<=reset ? 4'd0 : {zero_pipe[2:0],MIXER && address==0};
  local_index<=index[7:0];
  coefficient_address<=descriptor[DW-1:4]+(CW'(local_index)>>descriptor[3:0]);
  shift1<=descriptor[3:0]; fraction1<={1'b0,local_index}&((9'd1<<descriptor[3:0])-9'd1);
  a<=coefficients[coefficient_address];b<=coefficients[coefficient_address+1'b1];
  shift2<=shift1;fraction2<=fraction1;
  value<=zero_pipe[2] ? ZERO : interpolated[15:0];
 end
endmodule
