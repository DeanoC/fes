// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_menu_endpoint.h"
#include "Vfes_menu_endpoint___024root.h"
#include <verilated.h>
#include <cstdio>
#include <cstdlib>
#include <fstream>
static void check(bool ok,const char* m){if(!ok){std::fprintf(stderr,"FAIL %s\n",m);std::exit(1);}}
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv); Vfes_menu_endpoint t; bool toggle=false;
 t.clk=0;t.gpo=0;t.reset_hold=1;
 t.build_id[3]=0x00112233;t.build_id[2]=0x44556677;t.build_id[1]=0x8899aabb;t.build_id[0]=0xccddeeff;
 auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();check(!t.rootp->fes_menu_endpoint__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__violation,"DDR bounds/write violation");};
 auto count=[&](){return t.rootp->fes_menu_endpoint__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__accepted;};
 for(unsigned i=0;i<20;++i)tick();check(count()==0&&t.exec_reset&&!t.enable,"startup");
 t.reset_hold=0;for(unsigned i=0;i<20;++i)tick();check(count()==0,"disabled reads");
 auto command=[&](unsigned op,unsigned index,unsigned arg){
  auto previous=t.gpi;auto fields=op<<24|index<<16|arg;t.gpo=fields|unsigned(toggle)<<31;
  tick();check(t.gpi==previous,"payload without toggle");toggle=!toggle;t.gpo=fields|unsigned(toggle)<<31;
  unsigned cycles=0;while(bool(t.gpi&0x800000)!=toggle&&cycles++<2000000)tick();
  check(bool(t.gpi&0x800000)==toggle,"ACK timeout");auto result=t.gpi;
  for(unsigned i=0;i<10;++i)tick();check(t.gpi==result,"repeated command");return result;
 };
 check(argc==2,"fixture path");std::ifstream fixture(argv[1]);check(bool(fixture),"fixture open");
 unsigned op,index,arg,expected,event;unsigned n=0;
 while(fixture>>op>>index>>arg>>expected>>event){
  if(event){for(unsigned i=0;i<2000000&&t.displayed_sequence!=1;++i)tick();check(t.displayed_sequence==1,"frame completion");}
  check(command(op,index,arg)==expected,"golden exchange");++n;
 }
 check(n>40,"fixture coverage");check(t.quiesced&&t.exec_reset&&!t.faulted,"golden drained hold");
 command(2,0,1);command(20,0,1);
 unsigned before=count();for(unsigned i=0;i<2000000&&count()==before;++i)tick();check(count()>before,"restart");
 t.rootp->fes_menu_endpoint__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__pause_responses=1;
 for(unsigned i=0;i<10000;++i)tick();toggle=!toggle;t.gpo=(20u<<24)|unsigned(toggle)<<31;
 for(unsigned i=0;i<1000;++i)tick();check(bool(t.gpi&0x800000)!=toggle&&!t.quiesced,"quiesce must wait");
 t.rootp->fes_menu_endpoint__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__pause_responses=0;
 for(unsigned i=0;i<10000&&bool(t.gpi&0x800000)!=toggle;++i)tick();
 check(bool(t.gpi&0x800000)==toggle&&t.quiesced,"quiesce completion");
 check((command(20,0,1)&0x40ffff)==0,"re-enable");
 check((command(21,0,65535)&0x40ffff)==0,"max low");
 check((command(21,1,65535)&0x40ffff)==0,"max high");
 check((command(21,2,2)&0x40ffff)==0x400003,"invalid slot preserves staging");
 check((command(21,2,1)&0x40ffff)==0,"max commit");
 for(unsigned i=0;i<2000000&&t.displayed_sequence!=0xffffffffu;++i)tick();
 check(t.displayed_sequence==0xffffffffu,"max sequence displayed");
 check((command(21,0,1)&0x40ffff)==0,"wrapped low");
 check((command(21,1,0)&0x40ffff)==0,"wrapped high");
 check((command(21,2,0)&0x40ffff)==0x400004,"wrap rejected");
 command(20,0,0);
 t.rootp->fes_menu_endpoint__DOT__scanout__DOT__video__DOT__underflows=0x1234ffffu;
 check((command(18,12,0)&0x40ffff)==65535,"counter low");
 t.rootp->fes_menu_endpoint__DOT__scanout__DOT__video__DOT__underflows=0x12350000u;
 check((command(18,13,0)&0x40ffff)==0x1234,"coherent high");
 check((command(18,13,0)&0x40ffff)==0x400004,"snapshot consumed");
 std::puts("PASS menu GP golden identity/config/sequence/frame/drain/restart/bounds");
}
