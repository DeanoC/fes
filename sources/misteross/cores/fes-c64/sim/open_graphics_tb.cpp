// SPDX-License-Identifier: MIT
#include "Vc64_video_sim_top.h"
#include "Vc64_video_sim_top___024root.h"
#include "verilated.h"
#include "pal_palette_reference.h"
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
 unsigned full_d011=0x3b,short_d011=0x33;
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
      if((ra==250&&cy==55)||(ra==0&&cy==1)){t.addr=0x11;t.din=ra==250?short_d011:full_d011;t.we=1;}
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



 const auto palette=pal_palette_reference();
 for(unsigned bank=0;bank<4;++bank)for(unsigned mode=0;mode<2;++mode){
  full_d011=mode?0x5b:0x3b;short_d011=mode?0x53:0x33;
  t.bank=bank;write(0x11,full_d011);write(0x16,8);write(0x18,0x18);write(0x20,0);write(0x21,6);write(0x22,13);write(0x23,10);write(0x24,7);write(0x15,0);
  for(unsigned c=0;c<1000;++c){ram[bank*0x4000+0x400+c]=mode?0x80:0xa2;t.color_addr=c;t.color_din=1;t.color_we=1;sys();}t.color_we=0;
  for(unsigned i=0;i<8000;++i)ram[bank*0x4000+0x2000+i]=255;
  ram[bank*0x4000+0x3fff]=0;if(mode)ram[bank*0x4000+0x39ff]=0;
  settle();unsigned pixels=0,errors=0;bool previous=false,record=false;
  for(unsigned n=0;n<3*1650*750;++n){pixel();if(t.vsync&&!previous){if(record&&pixels==1280*720)break;record=true;pixels=0;errors=0;}previous=t.vsync;
   if(record&&t.de){unsigned x=pixels%1280,y=pixels/1280,color=0;
    if(x>=160&&x<1120){unsigned vx=(x-160)*2/5,raster=y*17/45+16;unsigned idle=mode?6:0,last=mode?10:2,foreground=mode?1:10;
     color=idle;
     if(raster>=51&&raster<=250){color=(vx>=32&&vx<352)?foreground:last;if(raster==51&&vx<32)color=idle;}
     if(raster==251&&vx<32)color=last;
    }
    unsigned expected=palette[color];if(t.red!=(expected>>16)||t.green!=((expected>>8)&255)||t.blue!=(expected&255)){if(errors++<8)std::printf("metadata bank%u mode%u pixel%u,%u got%02x%02x%02x expected%06x\n",bank,mode,x,y,t.red,t.green,t.blue,expected);}++pixels;
   }
  }
  if(errors||pixels!=1280*720){std::printf("FAIL metadata bank%u mode%u pixels%u errors%u\n",bank,mode,pixels,errors);return 1;}
  std::printf("Open graphics metadata PASS bank%u mode%u: blank glyphs,held metadata,first/last display rows andfullPALview\n",bank,mode);std::fflush(stdout);
 }
}
