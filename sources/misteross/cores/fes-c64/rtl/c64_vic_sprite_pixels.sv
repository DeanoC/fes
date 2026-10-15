// SPDX-License-Identifier: MIT
// PAL sprite shift registers, expansion, multicolor, priority and dot collision masks.
module c64_vic_sprite_pixels #(parameter integer ROW_DATA_DOT=4)(input wire clk,reset,tick,input wire [2:0] dot,
 input wire [8:0] xpos,input wire [71:0] positions,input wire [191:0] data,
 input wire pending_update,input wire [7:0] display_bits,pointer_cycle,row_cycle,
 input wire [7:0] priority_bits,multicolor_bits,expand_bits,
 input wire foreground,input wire [3:0] background_color,multi0,multi1,
 input wire [31:0] colors,
 output wire [3:0] color,output reg [3:0] color_q,output reg [7:0] ss_q,bg_q,
 output reg [7:0] active,pending,halt,ex_flop,mc_flop,
 output wire [255:0] shifts,output wire [15:0] pixels);
 reg [8:0] xpipe[0:7];reg [31:0] shift[0:7],nshift[0:7];reg [1:0] pixel[0:7],npixel[0:7];
 reg [7:0] pri,mc,expand,npri,nmc,nexpand;
 reg [7:0] nactive,npending,nhalt,nex,nmf,opacity;
 reg [3:0] result;integer winner;
 always @*begin
  nactive=active;npending=pending;nhalt=halt;nex=ex_flop;nmf=mc_flop;
  npri=pri;nmc=mc;nexpand=expand;opacity=0;winner=-1;result=background_color;
  for(integer i=0;i<8;i=i+1)begin nshift[i]=shift[i];npixel[i]=pixel[i];end
  if(dot==2)nactive=nactive&~row_cycle;
  if(dot==3)nhalt=nhalt|pointer_cycle;
  if(dot==4)begin
   if(pending_update)npending=display_bits;
  end
  if(dot==ROW_DATA_DOT)for(integer i=0;i<8;i=i+1)if(row_cycle[i])nshift[i]={8'd0,data[i*24+:24]};
  if(dot==6)begin npri=priority_bits;nexpand=expand_bits;end
  if(dot==7)begin nmf=nmf&~(multicolor_bits^mc);nmc=multicolor_bits;nhalt=nhalt&~row_cycle;end
  for(integer i=0;i<8;i=i+1)
   if(npending[i]&&!nactive[i]&&!nhalt[i]&&xpos==xpipe[i])begin nex[i]=1;nmf[i]=1;nactive[i]=1;end
  for(integer i=7;i>=0;i=i-1)if(nactive[i])begin
   if(nshift[i]!=0||npixel[i]!=0)begin
    if(!nhalt[i])begin
     if(nex[i])begin
      if(nmc[i])begin if(nmf[i])npixel[i]=nshift[i][23:22];nmf[i]=!nmf[i];end
      else npixel[i]={nshift[i][23],1'b0};
     end
     if(nex[i])nshift[i]=nshift[i]<<1;
     if(nexpand[i])nex[i]=!nex[i];else nex[i]=1;
    end
    if(npixel[i]!=0)begin winner=i;opacity[i]=1;end
   end else nactive[i]=0;
  end
  if(winner>=0&&!(foreground&&npri[winner]))case(npixel[winner])
   1:result=multi0;2:result=colors[winner*4+:4];3:result=multi1;default:begin end
  endcase
 end
 assign color=result;
 for(genvar i=0;i<8;i=i+1)begin : state_outputs
  assign shifts[i*32+:32]=shift[i];assign pixels[i*2+:2]=pixel[i];
 end
 always @(posedge clk)begin
  if(reset)begin
   active<=0;pending<=0;halt<=0;ex_flop<=0;mc_flop<=0;pri<=0;mc<=0;expand<=0;color_q<=0;ss_q<=0;bg_q<=0;
   for(integer i=0;i<8;i=i+1)begin xpipe[i]<=0;shift[i]<=0;pixel[i]<=0;end
  end else if(tick)begin
   active<=nactive;pending<=npending;halt<=nhalt;ex_flop<=nex;mc_flop<=nmf;pri<=npri;mc<=nmc;expand<=nexpand;
   color_q<=result;ss_q<=((opacity&(opacity-1'b1))!=0)?opacity:0;bg_q<=foreground?opacity:0;
   for(integer i=0;i<8;i=i+1)begin shift[i]<=nshift[i];pixel[i]<=npixel[i];if(dot==7)xpipe[i]<=positions[i*9+:9];end
  end
 end
endmodule
