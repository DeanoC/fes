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
 for(unsigned b=0;b<4;++b){
  for(unsigned c=0;c<1000;++c)ram[b*0x4000+0x400+c]=(c&1)?0x42:0x41;
  for(unsigned code=0;code<256;++code)for(unsigned row=0;row<8;++row)
   ram[b*0x4000+0x2000+code*8+row]=(code&1)?(0x80>>((row+b)&7)):(1<<((row+b)&7));
 }
 for(unsigned bank=0;bank<4;++bank){
  t.bank=bank;settle();unsigned pixels=0;bool previous=false,record=false;unsigned errors=0;
  for(unsigned n=0;n<3*1650*750;++n){pixel();
   if(t.vsync&&!previous){if(record&&pixels==1280*720)break;record=true;pixels=0;errors=0;}
   previous=t.vsync;
   if(record&&t.de){unsigned x=pixels%1280,y=pixels/1280;
    bool white=false;if(y>=60&&y<660){unsigned col=x/32,row=((y-60)/3)%8,bit=(x/4)%8;
     white=(col&1)?(bit==7-((row+bank)&7)):(bit==((row+bank)&7));}
    unsigned col=x/32; unsigned expected=white?((col&1)?204:255):0; unsigned gb=white&&!(col&1)?255:0;
    if(t.red!=expected||t.green!=gb||t.blue!=gb){if(errors++<5)std::printf("bank%u x%u y%u got%u wanted%u\n",bank,x,y,t.red,expected);}
    ++pixels;
   }
  }
  if(pixels!=1280*720||errors){std::printf("FAIL bank%u pixels%u errors%u\n",bank,pixels,errors);return 1;}
  std::printf("RAM charset bank%u: complete 1280x720 frame matched\n",bank);
 }
 write(0x11,0x3b);write(0x16,0x18);write(0x21,6);
 for(unsigned c=0;c<1000;++c){t.color_addr=c;t.color_din=5;t.color_we=1;sys();}t.color_we=0;
 for(unsigned b=0;b<4;++b)for(unsigned c=0;c<1000;++c){
  ram[b*0x4000+0x400+c]=0x12;
  for(unsigned row=0;row<8;++row)ram[b*0x4000+0x2000+c*8+row]=((c+row+b)&1)?0xe4:0x1b;
 }
 const unsigned colors[]={0x0000cc,0xffffff,0xcc0000,0x00cc00};
 for(bool multicolor : {false,true}) {
 write(0x16,multicolor?0x18:0x08);
 for(unsigned bank=0;bank<4;++bank){
  t.bank=bank;settle();unsigned pixels=0;bool previous=false,record=false;unsigned errors=0;
  for(unsigned n=0;n<3*1650*750;++n){pixel();
   if(t.vsync&&!previous){if(record&&pixels==1280*720)break;record=true;pixels=0;errors=0;}
   previous=t.vsync;
   if(record&&t.de){unsigned x=pixels%1280,y=pixels/1280,expected=0;
    if(y>=60&&y<660){unsigned cell=((y-60)/24)*40+x/32,row=((y-60)/3)%8;
     unsigned packed=((cell+row+bank)&1)?0xe4:0x1b;
     if(multicolor){unsigned pair=(packed>>(6-2*((x%32)/8)))&3;expected=colors[pair];}
     else expected=(packed&(0x80>>((x%32)/4)))?0xffffff:0xcc0000;}
    if(t.red!=(expected>>16)||t.green!=((expected>>8)&255)||t.blue!=(expected&255)){
      if(errors++<5)std::printf("bitmap bank%u x%u y%u got%02x%02x%02x expected%06x\n",bank,x,y,t.red,t.green,t.blue,expected);}
    ++pixels;
   }
  }
  if(pixels!=1280*720||errors){std::printf("FAIL bitmap bank%u pixels%u errors%u\n",bank,pixels,errors);return 1;}
  std::printf("%s bitmap bank%u: complete 1280x720 frame and palette selections matched\n",multicolor?"Multicolor":"Monochrome",bank);
 }

 }
}
