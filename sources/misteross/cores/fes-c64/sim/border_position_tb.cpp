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
 t.reading=0;t.read_sample=0;t.phi=0;t.cycle_clock=0;t.we=0;t.color_we=0;t.addr=0;t.din=0;t.color_addr=0;t.color_din=0;t.bank=0;t.eval();
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

 for(unsigned bank=0;bank<4;++bank)for(unsigned scroll:{0u,2u}){
  t.bank=bank;write(0x11,0x1b);write(0x16,scroll);write(0x18,0x18);write(0x20,0);write(0x21,6);write(0x15,0);
  for(unsigned c=0;c<1000;++c){ram[bank*0x4000+0x400+c]=0;t.color_addr=c;t.color_din=1;t.color_we=1;sys();}t.color_we=0;
  for(unsigned row=0;row<8;++row)ram[bank*0x4000+0x2000+row]=255;
  settle();unsigned pixels=0,errors=0;bool previous=false,record=false;
  for(unsigned n=0;n<3*1650*750;++n){pixel();if(t.vsync&&!previous){if(record&&pixels==1280*720)break;record=true;pixels=0;errors=0;}previous=t.vsync;
   if(record&&t.de){unsigned x=pixels%1280,y=pixels/1280;bool white=false;
    if(x>=160&&x<1120){unsigned vx=(x-160)*2/5,vy=y*17/45;white=vx>=39&&vx<=342&&vy>=35&&vy<=234;}
    unsigned expected=white?255:0;if(t.red!=expected||t.green!=expected||t.blue!=expected){if(errors++<4)std::printf("bank%u scroll%u pixel%u,%u got%02x%02x%02x expected%02x\n",bank,scroll,x,y,t.red,t.green,t.blue,expected);}++pixels;
   }
  }
  std::printf("CSEL38 bank%u scroll%u pixels%u errors%u\n",bank,scroll,pixels,errors);std::fflush(stdout);
  if(errors||pixels!=1280*720)return 1;
 }
}
