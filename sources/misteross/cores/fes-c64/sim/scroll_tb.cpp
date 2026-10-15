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
 const auto palette=pal_palette_reference();
 Verilated::commandArgs(argc,argv);Vc64_video_sim_top t;t.clk_sys=0;t.pixel_clk=0;t.reset=1;
 t.reading=0;t.read_sample=0;t.phi=0;t.cycle_clock=0;t.we=0;t.color_we=0;t.addr=0;t.din=0;t.color_addr=0;t.color_din=0;t.bank=0;t.eval();
 auto sys=[&](){t.clk_sys=0;t.eval();t.clk_sys=1;t.eval();};sys();t.reset=0;sys();
 auto write=[&](unsigned a,unsigned v){t.addr=a;t.din=v;t.we=1;sys();t.we=0;sys();};
 write(0x20,0);write(0x21,0);write(0x18,0x18);
 for(unsigned c=0;c<1000;++c){t.color_addr=c;t.color_din=(c&1)?2:1;t.color_we=1;sys();}t.color_we=0;
 uint64_t sys_edge=0,pixel_edge=0;
 unsigned native_phase=0,native_clock=0;
 unsigned change_cycle=56,change_value=0x62;
 auto pixel=[&](){
  if(!sys_edge){t.clk_sys=0;t.eval();}
  for(;;){
   bool system=sys_edge*74250000<=pixel_edge*52224000;
   if(system){
    bool rising=!(sys_edge&1);t.clk_sys=rising;
    if(rising){t.phi=native_phase+985248>=52224000;t.cycle_clock=native_clock;}
    if(rising){unsigned cy=t.rootp->c64_video_sim_top__DOT__vic__DOT__phi_div+1;
     t.we=native_clock==26&&(cy==16||cy==change_cycle);if(t.we){t.addr=0x16;t.din=cy==16?0x08:change_value;}}
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

 for(unsigned bank=0;bank<4;++bank)for(unsigned mode=0;mode<3;++mode){
  bool carry=mode==1;change_cycle=carry?32:mode==2?55:56;change_value=mode==2?0x0a:0x62;
  t.bank=bank;write(0x11,0x1b);write(0x16,8);write(0x18,0x18);write(0x20,0);write(0x21,6);write(0x15,0);
  for(unsigned c=0;c<1000;++c){ram[bank*0x4000+0x400+c]=0;t.color_addr=c;t.color_din=1;t.color_we=1;sys();}t.color_we=0;
  for(unsigned row=0;row<8;++row)ram[bank*0x4000+0x2000+row]=carry?255:8;
  settle();unsigned pixels=0,errors=0,checked=0;bool previous=false,record=false;
  for(unsigned n=0;n<3*1650*750;++n){pixel();if(t.vsync&&!previous){if(record&&pixels==1280*720)break;record=true;pixels=0;errors=0;checked=0;}previous=t.vsync;
   if(record&&t.de){unsigned x=pixels%1280,y=pixels/1280;
    if(x>=160&&x<1120){unsigned vx=(x-160)*2/5,vy=y*17/45;
     if(vx>=32&&vx<(carry?332u:352u)&&vy>=35&&vy<235){unsigned nx=vx-32;unsigned expected=carry?((nx==128||nx==129)?palette[6]:palette[1]):((nx%8)==4?palette[1]:palette[6]);
      if(t.red!=(expected>>16)||t.green!=((expected>>8)&255)||t.blue!=(expected&255)){if(errors++<4)std::printf("bank%u native%u,%u got%02x%02x%02x expected%06x\n",bank,nx,vy-35,t.red,t.green,t.blue,expected);}++checked;
     }
    }++pixels;
   }
  }
  std::printf("D016 scroll bank%u mode%u total%u checked%u errors%u\n",bank,mode,pixels,checked,errors);std::fflush(stdout);
  if(errors||pixels!=1280*720||checked<390000)return 1;
 }
}
