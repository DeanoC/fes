// SPDX-License-Identifier: MIT
// PAL sprite row/pointer DMA and bus ownership; pixel composition is separate.
module c64_vic_sprite_fetch(input wire clk,reset,phi1,phi2,
 input wire [5:0] cycle,input wire [8:0] raster,
 input wire [7:0] enabled,expanded,input wire [63:0] ypos,
 input wire expansion_write,input wire [7:0] expansion_value,input wire other_ba_n,
 input wire sample,input wire [7:0] ram_data,cpu_data,d018,input wire [1:0] bank,
 output reg request,output reg [15:0] address,output wire [63:0] pointers,
 output wire [191:0] sprite_data,
 output reg [7:0] dma,display,exp_flop,output wire [47:0] counters,bases,
 output wire ba_n,aec_n);
 reg [5:0] mc[0:7],mcbase[0:7];reg [2:0] lead;
 reg [7:0] mask,start_mask;reg slot,second_slot;reg [2:0] sprite;
 integer i;
 always @* begin
  mask=0;
  case(cycle)
   55,56:mask=8'h01;57,58:mask=8'h03;59:mask=8'h07;60:mask=8'h06;
   61:mask=8'h0e;62:mask=8'h0c;63:mask=8'h1c;1:mask=8'h18;
   2:mask=8'h38;3:mask=8'h30;4:mask=8'h70;5:mask=8'h60;
   6:mask=8'he0;7,8:mask=8'hc0;9,10:mask=8'h80;
   default:mask=0;
  endcase
  start_mask=0;
  for(integer n=0;n<8;n=n+1)
   if(phi1&&(cycle==55||cycle==56)&&enabled[n]&&ypos[n*8+:8]==raster[7:0]&&!dma[n])start_mask[n]=1;
  slot=1;second_slot=0;sprite=0;
  case(cycle)
   58:sprite=0;59:begin sprite=0;second_slot=1;end
   60:sprite=1;61:begin sprite=1;second_slot=1;end
   62:sprite=2;63:begin sprite=2;second_slot=1;end
   1:sprite=3;2:begin sprite=3;second_slot=1;end
   3:sprite=4;4:begin sprite=4;second_slot=1;end
   5:sprite=5;6:begin sprite=5;second_slot=1;end
   7:sprite=6;8:begin sprite=6;second_slot=1;end
   9:sprite=7;10:begin sprite=7;second_slot=1;end
   default:slot=0;
  endcase
 end
 assign ba_n=other_ba_n && !|(mask&(dma|start_mask));
 assign aec_n=!(slot&&dma[sprite]&&lead==4);
 for(genvar n=0;n<8;n=n+1)begin : state_outputs
  assign counters[n*6+:6]=mc[n];assign bases[n*6+:6]=mcbase[n];
 end
 always @(posedge clk)begin
  if(reset)begin
   dma<=0;display<=0;exp_flop<=8'hff;lead<=0;
   for(i=0;i<8;i=i+1)begin mc[i]<=0;mcbase[i]<=0;end
  end else begin
   if(phi1)begin
    if(ba_n)lead<=0;else if(lead<4)lead<=lead+1'b1;
    for(i=0;i<8;i=i+1)begin
     if(cycle==16&&exp_flop[i])begin
      mcbase[i]<=mc[i];if(mc[i]==63)dma[i]<=0;
     end
     if(start_mask[i])begin dma[i]<=1;mcbase[i]<=0;exp_flop[i]<=1;end
     if(cycle==56&&(dma[i]||start_mask[i])&&expanded[i])
      exp_flop[i]<=start_mask[i]?1'b0:!exp_flop[i];
     if(cycle==58)begin
      mc[i]<=mcbase[i];
      if(dma[i])begin if(enabled[i]&&ypos[i*8+:8]==raster[7:0])display[i]<=1;end
      else display[i]<=0;
     end
    end
    if(slot&&second_slot&&dma[sprite])mc[sprite]<=mc[sprite]+1'b1;
   end
   if(phi2&&slot&&dma[sprite])mc[sprite]<=mc[sprite]+1'b1;
   if(expansion_write&&expansion_value!=expanded)for(i=0;i<8;i=i+1)
    if(!expansion_value[i]&&!exp_flop[i])begin
     exp_flop[i]<=1;
     if(cycle==15)mc[i]<=(6'h2a&(mcbase[i]&mc[i]))|(6'h15&(mcbase[i]|mc[i]));
    end
  end
 end
 reg [7:0] pointer[0:7];reg [23:0] data[0:7];
 reg [2:0] pending_sprite;reg [1:0] pending_lane;reg pending_pointer;
 for(genvar n=0;n<8;n=n+1)begin : data_outputs
  assign pointers[n*8+:8]=pointer[n];assign sprite_data[n*24+:24]=data[n];
 end
 always @(posedge clk)begin
  if(reset)begin
   request<=0;address<=0;pending_sprite<=0;pending_lane<=0;pending_pointer<=0;
   for(integer n=0;n<8;n=n+1)begin pointer[n]<=0;data[n]<=0;end
  end else begin
   if(phi1)begin
    request<=0;
    if(slot)begin
     request<=1;pending_sprite<=sprite;pending_pointer<=!second_slot;pending_lane<=1;
     if(!second_slot)address<={bank,d018[7:4],7'b1111111,sprite};
     else address<=dma[sprite]?{bank,pointer[sprite],mc[sprite]}:{bank,14'h3fff};
    end
   end
   if(phi2)begin
    request<=0;
    if(slot)begin
     pending_sprite<=sprite;pending_pointer<=0;pending_lane<=second_slot?2:0;
     if(dma[sprite]&&lead==4)begin request<=1;address<={bank,pointer[sprite],mc[sprite]};end
     else if(second_slot)data[sprite][7:0]<=cpu_data;
     else data[sprite][23:16]<=cpu_data;
    end
   end
   if(sample&&request)begin
    request<=0;
    if(pending_pointer)pointer[pending_sprite]<=ram_data;
    else case(pending_lane)
     0:data[pending_sprite][23:16]<=ram_data;
     1:data[pending_sprite][15:8]<=ram_data;
     2:data[pending_sprite][7:0]<=ram_data;
     default:begin end
    endcase
   end
  end
 end
endmodule
