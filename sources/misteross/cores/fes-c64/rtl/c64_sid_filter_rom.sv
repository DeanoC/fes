// SPDX-License-Identifier: GPL-2.0-or-later
// Constants derived from the pinned reSID MOS6581 filter tables (Dag Lem).
module c64_sid_filter_rom #(parameter FAMILY=0,WORDS=256, BANKS=(WORDS+511)/512)(input wire clk,input wire [20:0] address,output wire [19:0] value);
 `include "c64_sid_filter_pages.vh"
 localparam BW=BANKS<2 ? 1 : $clog2(BANKS);
 wire [19:0] q[0:BANKS-1];reg [BW-1:0] selected;
 always @(posedge clk)selected<=BW'(address>>9);
 genvar b;generate for(b=0;b<BANKS;b=b+1)begin:bank
 localparam [10239:0] BANK_DATA=init_bank(FAMILY*32+b);
`ifndef FES_SIMULATION
 if (FAMILY == 1 || FAMILY == 6) begin: block_rom
 // Keep summer and cutoff tables in RAM; the remaining tables use logic.
 MISTRAL_M10K #(.CFG_ABITS(9),.CFG_DBITS(20),.CFG_DUAL_CLOCK(0),.CFG_ASYNC_READ(0),.INIT(BANK_DATA)) rom(
 .CLK1(clk),.CLK2(),.A1ADDR(9'd0),.A1DATA(20'd0),.A1EN(1'b1),.A1BE(),.B1ADDR(address[8:0]),.B1DATA(q[b]),.B1EN(1'b1),.ACLR0(1'b0),.ACLR1(1'b0));
 end else begin: logic_rom
 // mem2reg also bypasses the pinned Intel legacy BRAM mapper.
 (* ram_style = "logic", mem2reg *)
`endif
 reg [19:0] words[0:511];reg [19:0] read_q;
 integer init_index;
 initial for(init_index=0;init_index<512;init_index=init_index+1) words[init_index]=BANK_DATA[init_index*20+:20];
 always @(posedge clk) read_q<=words[address[8:0]];
 assign q[b]=read_q;
`ifndef FES_SIMULATION
 end
`endif
 end endgenerate
 assign value=q[selected];
endmodule
