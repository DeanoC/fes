// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vtop.h"
#include "Vtop___024root.h"
#include <verilated.h>
#include <cstdio>
#include <cstdlib>
static void check(bool ok,const char* m){if(!ok){std::fprintf(stderr,"FAIL %s\n",m);std::exit(1);}}
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv); Vtop t; bool toggle=false;
 auto tick=[&](){t.rootp->top__DOT__video_clock__DOT__outclk_0=0;t.eval();t.rootp->top__DOT__video_clock__DOT__outclk_0=1;t.eval();check(!t.rootp->top__DOT__endpoint__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__violation,"bounds");};
 auto count=[&](){return t.rootp->top__DOT__endpoint__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__accepted;};
 auto command=[&](unsigned op,unsigned index,unsigned arg){toggle=!toggle;t.rootp->top__DOT__hps_gp__DOT__gp_out=op<<24|index<<16|arg|unsigned(toggle)<<31;for(unsigned i=0;i<2000000&&bool(t.rootp->top__DOT__hps_gp__DOT__observed&0x800000)!=toggle;++i)tick();check(bool(t.rootp->top__DOT__hps_gp__DOT__observed&0x800000)==toggle,"ack");auto result=t.rootp->top__DOT__hps_gp__DOT__observed&0x40ffff;for(unsigned i=0;i<10;++i)tick();return result;};
 for(unsigned i=0;i<100;++i)tick();check(count()==0&&!t.HDMI_TX_DE&&!t.HDMI_TX_HS&&!t.HDMI_TX_VS&&t.HDMI_TX_D==0,"PLL unlocked");
 t.rootp->top__DOT__video_clock__DOT__locked=1;
 tick();check(!t.HDMI_TX_DE&&!t.HDMI_TX_HS&&!t.HDMI_TX_VS&&t.HDMI_TX_D==0,"2-FF locked sync cycle 1 holds outputs");
 for(unsigned i=0;i<4;++i)tick();
 unsigned hs_cycles=0,vs_cycles=0,de_cycles=0;
 for(unsigned i=0;i<1650u*750u;++i){
  t.rootp->top__DOT__video_clock__DOT__outclk_0=0;t.eval();
  hs_cycles+=t.HDMI_TX_HS;vs_cycles+=t.HDMI_TX_VS;de_cycles+=t.HDMI_TX_DE;
  check(t.HDMI_TX_D==0,"black pixels before enable");
  t.rootp->top__DOT__video_clock__DOT__outclk_0=1;t.eval();
  check(!t.rootp->top__DOT__endpoint__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__violation,"bounds");
 }
 check(hs_cycles==40u*750u&&vs_cycles==5u*1650u&&de_cycles==1280u*720u,"720p60 syncs run during exec_reset");
 check(count()==0,"locked but disabled");
 check(command(1,7,0)==1794,"capabilities");check(command(19,0,1)==0,"configure");check(command(2,0,1)==0,"release");check(command(20,0,1)==0,"enable");
 for(unsigned i=0;i<1650*750;++i)tick();check(count()>0,"reads after explicit enable");
 check(command(20,0,0)==0,"quiesce");check(command(2,0,0)==0,"hold after drain");auto before=count();for(unsigned i=0;i<1000;++i)tick();check(count()==before,"held no reads");
 std::puts("PASS described menu board PLL/identity/explicit enable/drain/DDR bounds");
}
