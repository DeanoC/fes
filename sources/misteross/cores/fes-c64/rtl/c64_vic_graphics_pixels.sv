// SPDX-License-Identifier: GPL-2.0-or-later
// Based on graphics drawing behavior in VICE vicii-draw-cycle.c, written by
// Daniel Kahlin <daniel@kahlin.net>. PAL6569R3 mode latency is selected.
// The caller supplies the already-prefetched byte and its matrix/color metadata.
module c64_vic_graphics_pixels(input wire clk,reset,tick,input wire [2:0] dot,
 input wire [7:0] glyph,matrix,d011,d016,input wire [3:0] cell_color,
 input wire [15:0] background,input wire latch_scroll,
 output wire [3:0] color,output wire foreground,output reg background0,
 output reg [3:0] color_q,output reg foreground_q);
 reg [7:0] shift_reg,matrix_reg;
 reg [3:0] color_reg;
 reg [2:0] xscroll;
 reg [1:0] mode11,pixel_reg;
 reg mode16,mode16_delayed,mc_phase;
 reg [7:0] next_shift,next_matrix;
 reg [3:0] next_color,result;
 reg [1:0] next_mode11,next_pixel;
 reg next_mode16,next_mode16_delayed,next_mc_phase;
 always @*begin
  next_shift=shift_reg;next_matrix=matrix_reg;next_color=color_reg;
  next_mode11=mode11;next_mode16=mode16;next_mode16_delayed=mode16_delayed;
  next_mc_phase=mc_phase;next_pixel=pixel_reg;
  if(dot==4)begin next_mode16=d016[4];next_mode11=next_mode11|d011[6:5];end
  if(dot==6)next_mode11=next_mode11&d011[6:5];
  if(dot==7)begin
   if(next_mode16&&!mode16_delayed)next_mc_phase=0;
   next_mode16_delayed=next_mode16;
  end
  if(dot==xscroll)begin
   next_shift=glyph;next_matrix=matrix;next_color=cell_color;next_mc_phase=1;
  end
  if(next_mode16_delayed&&(next_mode11[0]||next_color[3]))begin
   if(next_mc_phase)next_pixel=next_shift[7:6];
  end else if(next_mode11[0]||next_color[3])next_pixel={next_shift[7],1'b0};
  else next_pixel={next_shift[7],next_shift[7]};
  // Preserve source identity independently of the current color value.
  // Only D021 is resolved later by the PAL color pipeline.
  background0=0;
  case({next_mode11,next_mode16})
   0:begin
      result=next_pixel[1]?next_color:background[3:0];
      background0=!next_pixel[1];
     end
   1:case(next_pixel)
      0:begin result=background[3:0];background0=1;end
      1:result=background[7:4];
      2:result=background[11:8];default:result={1'b0,next_color[2:0]};
     endcase
   2:result=next_pixel[1]?next_matrix[7:4]:next_matrix[3:0];
   3:case(next_pixel)
      0:begin result=background[3:0];background0=1;end
      1:result=next_matrix[7:4];
      2:result=next_matrix[3:0];default:result=next_color;
     endcase
   4:begin
      result=next_pixel[1]?next_color:background[next_matrix[7:6]*4+:4];
      background0=!next_pixel[1]&&next_matrix[7:6]==0;
     end
   default:result=0;
  endcase
  next_shift=next_shift<<1;next_mc_phase=!next_mc_phase;
 end
 assign color=result;assign foreground=next_pixel[1];
 always @(posedge clk)begin
  if(reset)begin
   shift_reg<=0;matrix_reg<=0;color_reg<=0;xscroll<=0;mode11<=0;
   mode16<=0;mode16_delayed<=0;mc_phase<=0;pixel_reg<=0;color_q<=0;foreground_q<=0;
  end else if(tick)begin
   shift_reg<=next_shift;matrix_reg<=next_matrix;color_reg<=next_color;
   mode11<=next_mode11;mode16<=next_mode16;mode16_delayed<=next_mode16_delayed;
   mc_phase<=next_mc_phase;pixel_reg<=next_pixel;color_q<=result;foreground_q<=foreground;
   if(dot==7&&latch_scroll)xscroll<=d016[2:0];
  end
 end
endmodule
