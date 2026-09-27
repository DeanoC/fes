#include "Vtop.h"
#include "Vtop___024root.h"
#include <verilated.h>
#include <cstdio>
#include <cstdlib>
static void check(bool ok, const char* msg) { if (!ok) { std::fprintf(stderr,"FAIL %s\n",msg); std::exit(1); } }
int main(int argc,char**argv) {
 Verilated::commandArgs(argc,argv); Vtop t; t.eval();
 auto tick=[&]() { t.rootp->top__DOT__video_clock__DOT__outclk_0=0;t.eval();t.rootp->top__DOT__video_clock__DOT__outclk_0=1;t.eval(); };
 for(int i=0;i<100;++i) { tick(); check(!t.HDMI_TX_DE && !t.HDMI_TX_HS && !t.HDMI_TX_VS && !t.HDMI_TX_D,"PLL unlock gating"); }
 t.rootp->top__DOT__video_clock__DOT__locked=1;
 bool saw_green=false,saw_magenta=false,saw_return=false,last_vs=false;
 unsigned frames=0;
 for(unsigned i=0;i<1650u*750u*123u;++i) {
  tick(); if(t.HDMI_TX_VS&&!last_vs) ++frames; last_vs=t.HDMI_TX_VS;
  check(t.rootp->top__DOT__underflows==0,"board underflow");
  if(t.HDMI_TX_DE && t.rootp->top__DOT__scanout__DOT__h==2 && t.rootp->top__DOT__scanout__DOT__v==2) {
   if(t.HDMI_TX_D==0x00ff00) {saw_green=true;if(saw_magenta)saw_return=true;}
   if(t.HDMI_TX_D==0xff00ff) saw_magenta=true;
  }
 }
 check(frames>=120 && saw_green && saw_magenta && saw_return,"automatic 60-frame slot switching");
 std::puts("PASS board PLL gating/120-frame automatic pattern switching/no underflow");
}
