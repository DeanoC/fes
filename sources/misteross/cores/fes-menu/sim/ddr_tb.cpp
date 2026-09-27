#include "Vfes_menu_ddr.h"
#include "Vfes_menu_ddr___024root.h"
#include <verilated.h>
#include <cstdio>
#include <cstdlib>
static void check(bool ok,const char* m){if(!ok){std::fprintf(stderr,"FAIL %s\n",m);std::exit(1);}}
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv);Vfes_menu_ddr t;t.reset_hold=1;t.enable=0;t.quiesce=0;t.submit_valid=0;
 auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();check(!t.rootp->fes_menu_ddr__DOT__memory__DOT__f2sdram__DOT__violation,"DDR command bounds/unused ports/write violation");};
 auto accepted=[&](){return t.rootp->fes_menu_ddr__DOT__memory__DOT__f2sdram__DOT__accepted;};
 for(int i=0;i<20;++i)tick();check(accepted()==0&&!t.faulted,"startup hold");
 t.reset_hold=0;for(int i=0;i<20;++i)tick();check(accepted()==0&&!t.faulted,"disabled commands");
 t.enable=1;unsigned frames=0,seen=0;unsigned h=0,v=720;
 auto frame_tick=[&](){
  t.clk=0;t.eval();h=t.rootp->fes_menu_ddr__DOT__video__DOT__h;v=t.rootp->fes_menu_ddr__DOT__video__DOT__v;
  check(t.hs==(h>=1390&&h<1430)&&t.vs==(v>=725&&v<730),"DDR sync");
  if(h==0&&v==0)seen=0;
  if(t.de){unsigned index=v*1280+h;unsigned expected=((index&255)<<16)|(((index>>8)&255)<<8)|(t.displayed_sequence==42?0x77:0x22);check(t.rgb==expected,"DDR pixel order/slot");++seen;}
  if(h==1649&&v==749)++frames;tick();
 };
 while(frames<2)frame_tick();check(seen==921600&&!t.underflows,"DDR full frame");
 t.submit_valid=1;t.submit_slot=1;t.submit_sequence=42;while(!t.submit_ready)frame_tick();frame_tick();t.submit_valid=0;
 while(frames<4)frame_tick();check(t.displayed_sequence==42&&!t.underflows,"DDR slot switch");
 t.rootp->fes_menu_ddr__DOT__memory__DOT__f2sdram__DOT__pause_responses=1;
 for(unsigned i=0;i<10000;++i)tick();t.quiesce=1;
 for(int i=0;i<1000;++i)tick();check(!t.quiesced,"quiesce must wait for responses");
 t.rootp->fes_menu_ddr__DOT__memory__DOT__f2sdram__DOT__pause_responses=0;
 for(int i=0;i<1000&&!t.quiesced;++i)tick();check(t.quiesced,"guard/reader drain");
 unsigned count=accepted();t.reset_hold=1;for(int i=0;i<20;++i)tick();check(!t.faulted&&accepted()==count,"ordered hold");
 t.reset_hold=0;for(int i=0;i<20;++i)tick();t.quiesce=0;
 for(int i=0;i<1650*750 && accepted()==count;++i)tick();check(accepted()>count,"restart after drained hold");
 t.reset_hold=1;for(int i=0;i<20;++i)tick();check(t.faulted,"unexpected hold fail closed");
 t.reset_hold=0;for(int i=0;i<1000;++i)tick();count=accepted();for(int i=0;i<1000;++i)tick();check(accepted()==count&&t.rgb==0,"fault cannot restart hidden-response stream");
 std::puts("PASS DDR pixels/slots/bounds/disabled/guard drain/ordered hold/fault containment");
}
