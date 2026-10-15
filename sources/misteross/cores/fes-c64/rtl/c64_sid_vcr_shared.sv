// SPDX-License-Identifier: GPL-2.0-or-later
// Two independent exact VCR lookup streams share fourteen initialized TDP lanes.
// Constants derived from pinned reSID MOS6581 tables (Dag Lem).
module c64_sid_vcr_shared(input wire clk,reset,request_a,request_b,input wire [15:0] address_a,address_b,
 output wire [15:0] value_a,value_b,output wire valid_a,valid_b);
 `include "c64_sid_vcr_data.vh"

 wire [279:0] word_a,word_b;
 reg [6:0] index_a,index_b;reg [3:0] va=0,vb=0;
 assign valid_a=va[3];assign valid_b=vb[3];
 always @(posedge clk)begin index_a<=address_a[6:0];index_b<=address_b[6:0];va<=reset ? 4'd0 : {va[2:0],request_a};vb<=reset ? 4'd0 : {vb[2:0],request_b};end
 genvar lane;generate for(lane=0;lane<14;lane=lane+1)begin:bank
`ifdef FES_SIMULATION
 localparam [10239:0] BANK_DATA=init_bank(lane);
 reg [19:0] words[0:511];reg [19:0] read_a,read_b;
 integer init_index;
 initial for(init_index=0;init_index<512;init_index=init_index+1) words[init_index]=BANK_DATA[init_index*20+:20];
 always @(posedge clk)begin read_a<=words[address_a[15:7]];read_b<=words[address_b[15:7]];end
 assign word_a[lane*20+:20]=read_a;assign word_b[lane*20+:20]=read_b;
`else
 MISTRAL_M10K_TDP #(.CFG_ABITS(9),.CFG_DBITS(20),.CFG_ASYNC_READ(0),.INIT(init_bank(lane))) rom(
 .CLK1(clk),.CLK2(clk),.A1ADDR(address_a[15:7]),.B1ADDR(address_b[15:7]),.A1DATA(20'd0),.B1DATA(20'd0),.A1Q(word_a[lane*20+:20]),.B1Q(word_b[lane*20+:20]),.A1EN(1'b1),.B1EN(1'b1),.A1WE(1'b0),.B1WE(1'b0),.A1BE(2'b00),.B1BE(2'b00),.ACLR0(1'b0),.ACLR1(1'b0));
`endif
 end endgenerate
 c64_sid_vcr_finish lookup_finish_a(.clk(clk),.index1(index_a),.packed_word(word_a),.value(value_a));
 c64_sid_vcr_finish lookup_finish_b(.clk(clk),.index1(index_b),.packed_word(word_b),.value(value_b));
endmodule
module c64_sid_vcr_finish(input wire clk,input wire [6:0] index1,input wire [279:0] packed_word,output reg [15:0] value);
 function automatic [4:0] prefix8(input [271:0] record,input [6:0] count,input integer offset);
 integer j;begin prefix8=0;for(j=0;j<8;j=j+1)if(offset+j<127 && count>7'(offset+j))prefix8=prefix8+{3'd0,record[16+2*(offset+j)+:2]};end endfunction

 reg [15:0] base2,base3;
 wire [4:0] groups[0:15];genvar b;
 generate for(b=0;b<16;b=b+1)begin:group_sum
 reg [4:0] partial;always @(posedge clk)partial<=prefix8(packed_word[271:0],index1,b*8);assign groups[b]=partial;
 end endgenerate
 reg [6:0] sums[0:3];integer i;
 always @(posedge clk)begin
  base2<=packed_word[15:0];base3<=base2;
  for(i=0;i<4;i=i+1)sums[i]<={2'd0,groups[i*4]}+{2'd0,groups[i*4+1]}+{2'd0,groups[i*4+2]}+{2'd0,groups[i*4+3]};
  value<=base3+{9'd0,sums[0]}+{9'd0,sums[1]}+{9'd0,sums[2]}+{9'd0,sums[3]};
 end
endmodule
