#include "Vtop.h"
#include "Vtop___024root.h"
#include <verilated.h>
#include <cstdio>
#include <cstdlib>
#include <cstring>
static void check(bool ok,const char* m){if(!ok){std::fprintf(stderr,"FAIL %s\n",m);std::exit(1);}}
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv);Vtop t;t.eval();bool disabled=argc>1&&!std::strcmp(argv[1],"--disabled");
 auto tick=[&](){t.rootp->top__DOT__video_clock__DOT__outclk_0=0;t.eval();t.rootp->top__DOT__video_clock__DOT__outclk_0=1;t.eval();};
 auto accepted=[&](){return t.rootp->top__DOT__ddr__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__accepted;};
 for(int i=0;i<100;++i){tick();check(!t.HDMI_TX_DE&&!t.HDMI_TX_HS&&!t.HDMI_TX_VS&&!t.HDMI_TX_D&&accepted()==0,"DDR PLL hold/output gating");}
 t.rootp->top__DOT__video_clock__DOT__locked=1;unsigned frames=0,seen=0;
 for(unsigned i=0;i<1650u*750u*3u;++i){
  t.rootp->top__DOT__video_clock__DOT__outclk_0=0;t.eval();
  unsigned h=t.rootp->top__DOT__ddr__DOT__scanout__DOT__video__DOT__h;
  unsigned v=t.rootp->top__DOT__ddr__DOT__scanout__DOT__video__DOT__v;
  if(h==0&&v==0)seen=0;
  if(t.HDMI_TX_DE){unsigned index=v*1280+h;unsigned expected=disabled?0:((index&255)<<16)|(((index>>8)&255)<<8)|0x22;check(t.HDMI_TX_D==expected,"DDR board pixel/order");++seen;}
  if(h==1649&&v==749)++frames;
  t.rootp->top__DOT__video_clock__DOT__outclk_0=1;t.eval();
  check(!t.rootp->top__DOT__ddr__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__violation,"DDR board bounds/writes/unused ports");
  check(!t.rootp->top__DOT__underflows,"DDR board underflow");
  if(frames>=2)break;
 }
 check(frames>=2&&seen==921600,"DDR board full frame");
 check(disabled?accepted()==0:accepted()>0,"DDR board diagnostic enable");
 std::puts("PASS DDR board PLL gating/diagnostic enable/full frame");
}
