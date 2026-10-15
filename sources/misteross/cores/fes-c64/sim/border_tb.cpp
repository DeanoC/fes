// SPDX-License-Identifier: MIT
#include "Vc64_video_sim_top.h"
#include "Vc64_video_sim_top___024root.h"
#include "verilated.h"
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <initializer_list>
int main(int argc,char **argv){
 Verilated::commandArgs(argc,argv);Vc64_video_sim_top t;t.clk_sys=0;t.pixel_clk=0;t.reset=1;
 t.phi=0;t.cycle_clock=0;t.we=0;t.color_we=0;t.addr=0;t.din=0;t.color_addr=0;t.color_din=0;t.bank=0;t.eval();
 auto sys=[&](){t.clk_sys=0;t.eval();t.clk_sys=1;t.eval();};sys();t.reset=0;sys();
 auto write=[&](unsigned a,unsigned v){t.addr=a;t.din=v;t.we=1;sys();t.we=0;sys();};
 write(0x20,0);write(0x21,0);write(0x18,0x18);
 for(unsigned c=0;c<1000;++c){t.color_addr=c;t.color_din=(c&1)?2:1;t.color_we=1;sys();}t.color_we=0;
 uint64_t sys_edge=0,pixel_edge=0;
 unsigned native_phase=0,native_clock=0;
 auto pixel=[&](){
  if(!sys_edge){t.clk_sys=0;t.eval();}
  for(;;){
   bool system=sys_edge*74250000<=pixel_edge*52224000;
   if(system){
    bool rising=!(sys_edge&1);t.clk_sys=rising;
    if(rising){t.phi=native_phase+985248>=52224000;t.cycle_clock=native_clock;}
    if(rising){
     unsigned cy=t.rootp->c64_video_sim_top__DOT__vic__DOT__phi_div+1;
     unsigned ra=t.rootp->c64_video_sim_top__DOT__vic__DOT__raster;
     t.we=0;
     if(native_clock==24){
      if(cy==16||cy==56){t.addr=0x16;t.din=cy==16?8:0;t.we=1;}
      if((ra==250&&cy==55)||(ra==0&&cy==1)){t.addr=0x11;t.din=ra==250?0x13:0x1b;t.we=1;}
     }
    }
    t.eval();++sys_edge;
    if(rising){native_phase+=985248;if(native_phase>=52224000){native_phase-=52224000;native_clock=0;}else ++native_clock;}
   }else{
    bool rising=!(pixel_edge&1);t.pixel_clk=rising;t.eval();++pixel_edge;
    if(rising)return;
   }
  }
 };
 // A bank/mode change must propagate through a complete native frame and the
 // HDMI mailbox before comparing a complete displayed frame.
 auto settle=[&](){for(unsigned n=0;n<6*1650*750;++n)pixel();};
 auto &ram=t.rootp->c64_video_sim_top__DOT__memory__DOT__ram;


 const unsigned palette[]={0x000000,0xffffff,0xcc0000,0x00ffff,0xcc44cc,0x00cc00,0x0000cc,0xeeee77,0xdd8855,0x664400,0xff7777,0x333333,0x777777,0xaaff66,0x0088ff,0xbbbbbb};
 const unsigned xpos[]={0,4,16,24,344,368,496,480};
 const unsigned ypos[]={30,50,80,230,250,50,16,80};
 for(unsigned bank=0;bank<4;++bank)for(unsigned mode=0;mode<2;++mode){
  unsigned mcmask=mode?0xaa:0x55,xmask=mode?0xcc:0x33,ymask=mode?0xf0:0x0f;
  t.bank=bank;write(0x11,0x1b);write(0x16,8);write(0x18,0x18);write(0x20,0);write(0x21,6);
  write(0x25,13);write(0x26,10);write(0x1b,0);write(0x1c,mcmask);write(0x1d,xmask);write(0x17,ymask);
  for(unsigned c=0;c<1000;++c){ram[bank*0x4000+0x400+c]=0;t.color_addr=c;t.color_din=1;t.color_we=1;sys();}t.color_we=0;
  for(unsigned row=0;row<8;++row)ram[bank*0x4000+0x2000+row]=0;
  unsigned highx=0;
  for(unsigned i=0;i<8;++i){unsigned x=xpos[i];highx|=((x>>8)&1)<<i;write(i*2,x&255);write(i*2+1,ypos[i]-1);write(0x27+i,1+i);ram[bank*0x4000+0x7f8+i]=0xc0+i;
   for(unsigned row=0;row<21;++row){unsigned packed=(row&1)?0xe4e4e4:0x1b1b1b;for(unsigned b=0;b<3;++b)ram[bank*0x4000+(0xc0+i)*64+row*3+b]=(packed>>(16-b*8))&255;}
  }
  write(0x10,highx);write(0x15,255);settle();unsigned pixels=0,errors=0;bool previous=false,record=false;
  for(unsigned n=0;n<3*1650*750;++n){pixel();
   if(t.vsync&&!previous){if(record&&pixels==1280*720)break;record=true;pixels=0;errors=0;}previous=t.vsync;
   if(record&&t.de){unsigned x=pixels%1280,y=pixels/1280,color=0;
    if(x>=160&&x<1120){unsigned vx=(x-160)*2/5,raster=y*17/45+16;color=6;
     for(unsigned i=0;i<8;++i){
      // PAL sprite flags and X positions refer to the same cycle. Sprite7
      // X480 starts after its row reload and can cover the left eight pixels.
      int start=xpos[i]>=400?int(xpos[i])-496:int(xpos[i])+8;int dx=int(vx)-start,dy=int(raster)-int(ypos[i]);unsigned xscale=(xmask&(1u<<i))?2:1,yscale=(ymask&(1u<<i))?2:1;
      // Y compares only eight bits; PAL's312 lines allow a second
      // trigger and an expanded sprite can cross frame rollover.
      if(ypos[i]<56){
       int from_previous=int(raster)-int(ypos[i])+56;
       int second=int(raster)-int(ypos[i])-256;
       if(from_previous>=0&&unsigned(from_previous)<21*yscale)dy=from_previous;
       if(second>=0&&unsigned(second)<21*yscale)dy=second;
      }
      // X368 starts after the cycle58 display update reaches the
      // renderer. VICE DMA oracle confirms the final row cannot retrigger.
      unsigned height=21*yscale-(i==5?1:0);
      if(dx<0||dy<0||unsigned(dx)>=24*xscale||unsigned(dy)>=height)continue;
      unsigned row=unsigned(dy)/yscale,bit=unsigned(dx)/xscale,packed=(row&1)?0xe4e4e4:0x1b1b1b;
      unsigned px=(mcmask&(1u<<i))?((packed>>(22-2*(bit/2)))&3):((packed>>(23-bit))&1)*2;
      if(!px)continue;color=px==1?13:px==3?10:1+i;break;
     }
    }
    unsigned expected=palette[color];if(t.red!=(expected>>16)||t.green!=((expected>>8)&255)||t.blue!=(expected&255)){if(errors++<12)std::printf("bank%u mode%u x%u y%u got%02x%02x%02x expected%06x\n",bank,mode,x,y,t.red,t.green,t.blue,expected);}++pixels;
   }
  }
  if(pixels!=1280*720||errors){std::printf("FAIL border bank%u mode%u pixels%u errors%u\n",bank,mode,pixels,errors);return 1;}
  std::printf("Open-border frame PASS bank%u mode%u: wrapped and side/top/bottom border sprites\n",bank,mode);std::fflush(stdout);
 }
}
