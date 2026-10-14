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

 const unsigned palette[]={0x000000,0xffffff,0xcc0000,0x00ffff,0xcc44cc,0x00cc00,0x0000cc,0xeeee77,0xdd8855,0x664400,0xff7777,0x333333,0x777777,0xaaff66,0x0088ff,0xbbbbbb};

 for(unsigned bank=0;bank<4;++bank)for(unsigned mode=0;mode<5;++mode){
  const unsigned enabled=0xdb,mcmask=0xaa,xmask=0x33,ymask=0x0f;
  t.bank=bank;write(0x11,mode==4?0x5b:mode==2||mode==3?0x3b:0x1b);
  write(0x16,mode==1||mode==3?0x18:8);write(0x18,0x18);write(0x20,0);
  write(0x21,6);write(0x22,13);write(0x23,10);write(0x24,7);
  write(0x25,13);write(0x26,10);write(0x1b,0x55);write(0x1c,mcmask);write(0x1d,xmask);write(0x17,ymask);
  const unsigned patterns[]={0x1b,0xe4,0x3c,0x81};
  for(unsigned c=0;c<1000;++c){
   unsigned code=c%4,mat=mode==2||mode==3?((c%14+1)<<4)|((c*3)%14+1):mode==4?((c%4)<<6)|code:code;
   ram[bank*0x4000+0x400+c]=mat;t.color_addr=c;t.color_din=(c&1)?9:2;t.color_we=1;sys();
   if(mode==2||mode==3)for(unsigned row=0;row<8;++row)ram[bank*0x4000+0x2000+c*8+row]=patterns[(c+row)&3];
  }t.color_we=0;
  if(mode!=2&&mode!=3)for(unsigned code=0;code<4;++code)for(unsigned row=0;row<8;++row)ram[bank*0x4000+0x2000+code*8+row]=patterns[(code+row)&3];
  unsigned highx=0;
  for(unsigned i=0;i<8;++i){unsigned x=32+i*32;highx|=((x>>8)&1)<<i;write(i*2,x&255);write(i*2+1,80+i*6);write(0x27+i,1+i);ram[bank*0x4000+0x7f8+i]=0x40+i;
   for(unsigned row=0;row<21;++row){unsigned packed=(row&1)?0xe4e4e4:0x1b1b1b;for(unsigned b=0;b<3;++b)ram[bank*0x4000+(0x40+i)*64+row*3+b]=(packed>>(16-b*8))&255;}
  }
  write(0x10,highx);write(0x15,enabled);settle();unsigned pixels=0,errors=0;bool previous=false,record=false;
  for(unsigned n=0;n<3*1650*750;++n){pixel();
   if(t.vsync&&!previous){if(record&&pixels==1280*720)break;record=true;pixels=0;errors=0;}previous=t.vsync;
   if(record&&t.de){unsigned x=pixels%1280,y=pixels/1280,color=0;
    if(x>=160&&x<1120){unsigned vx=(x-160)*2/5,vy=y*17/45;
    if(vx>=32&&vx<352&&vy>=35&&vy<235){unsigned nx=vx-32,raster=vy+16,cell=((raster-51)/8)*40+nx/8,row=(raster-51)&7,bit=nx&7;
     unsigned mat=ram[bank*0x4000+0x400+cell],col=(cell&1)?9:2;
     unsigned address=mode==2||mode==3?0x2000+cell*8+row:0x2000+(mode==4?(mat&63):mat)*8+row;
     unsigned bits=ram[bank*0x4000+address],mono=(bits>>(7-bit))&1,pair=(bits>>(6-2*(bit/2)))&3;
     bool multi=mode==3||(mode==1&&(col&8)),fg=multi?(pair&2)!=0:mono!=0;
     if(multi){unsigned colors[]={6,mode==3?mat>>4:13,mode==3?mat&15:10,mode==3?col:col&7};color=colors[pair];}
     else if(mode==2)color=mono?mat>>4:mat&15;
     else {unsigned backgrounds[]={6,13,10,7};color=mono?col:mode==4?backgrounds[mat>>6]:6;}
     for(unsigned i=0;i<8;++i){if(!(enabled&(1u<<i)))continue;int dx=int(nx)+24-int(32+i*32),dy=int(raster)-int(81+i*6);unsigned xs=(xmask&(1u<<i))?2:1,ys=(ymask&(1u<<i))?2:1;
      if(dx<0||dy<0||unsigned(dx)>=24*xs||unsigned(dy)>=21*ys)continue;
      unsigned sr=unsigned(dy)/ys,sb=unsigned(dx)/xs,packed=(sr&1)?0xe4e4e4:0x1b1b1b;
      unsigned px=(mcmask&(1u<<i))?((packed>>(22-2*(sb/2)))&3):((packed>>(23-sb))&1)*2;
      if(!px)continue;if(!(fg&&(0x55&(1u<<i))))color=px==1?13:px==3?10:1+i;break;
     }
    }
    }
    unsigned expected=palette[color];if(t.red!=(expected>>16)||t.green!=((expected>>8)&255)||t.blue!=(expected&255)){if(errors++<8)std::printf("gfx bank%u mode%u x%u y%u got%02x%02x%02x expected%06x\n",bank,mode,x,y,t.red,t.green,t.blue,expected);}++pixels;
   }
  }
  if(pixels!=1280*720||errors){std::printf("FAIL gfx bank%u mode%u pixels%u errors%u\n",bank,mode,pixels,errors);return 1;}
  std::printf("Sprite graphics-mode frame PASS bank%u mode%u\n",bank,mode);
 }
}
