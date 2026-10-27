// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_menu_endpoint.h"
#include "Vfes_menu_endpoint___024root.h"
#include "blit_reference.hpp"
#include <verilated.h>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <random>
#include <vector>
using namespace blit;
static void check(bool ok,const char* m){if(!ok){std::fprintf(stderr,"FAIL %s\n",m);std::exit(1);}}
#define MODEL(name) t.rootp->fes_menu_endpoint__DOT__scanout__DOT__memory__DOT__f2sdram__DOT__##name
// The completed count the GP reports: it advances once port 1 has drained.
#define PUBLISHED(name) t.rootp->fes_menu_endpoint__DOT__scanout__DOT__blit__DOT__##name
#define VIDEO(name) t.rootp->fes_menu_endpoint__DOT__scanout__DOT__video__DOT__##name
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv); Vfes_menu_endpoint t; bool toggle=false;
 t.clk=0;t.gpo=0;t.reset_hold=1;
 t.build_id[3]=0x00112233;t.build_id[2]=0x44556677;t.build_id[1]=0x8899aabb;t.build_id[0]=0xccddeeff;
 auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();check(!MODEL(violation),"DDR span/port/write violation");};
 auto count=[&](){return MODEL(accepted)[0];};
 // The model holds the slots and list region at their offsets, then the heap.
 auto at=[&](uint32_t offset){
  uint32_t index=(offset<0x900000?offset:offset-HEAP+0x900000)>>3;
  return reinterpret_cast<uint8_t*>(&MODEL(mem)[index])+(offset&7);
 };
 std::vector<uint8_t> image(HEAP+0x100000);
 auto mirror=[&](){for(uint32_t o=0;o<0x900000;++o)image[o]=*at(o);for(uint32_t o=HEAP;o<HEAP+0x100000;++o)image[o]=*at(o);};
 auto put=[&](uint32_t first,const std::vector<Command>& list){
  for(size_t i=0;i<list.size();++i)std::memcpy(at(LIST+uint32_t(first+i)*32),list[i].word,32);
 };
 auto same_slot=[&](int slot){
  for(uint32_t o=uint32_t(slot)*SLOT;o<uint32_t(slot)*SLOT+uint32_t(H)*STRIDE;++o)if(*at(o)!=image[o])return false;
  return true;
 };
 for(unsigned i=0;i<20;++i)tick();check(count()==0&&t.exec_reset&&!t.enable,"startup");
 t.reset_hold=0;for(unsigned i=0;i<20;++i)tick();check(count()==0,"disabled reads");
 auto command=[&](unsigned op,unsigned index,unsigned arg){
  auto previous=t.gpi;auto fields=op<<24|index<<16|arg;t.gpo=fields|unsigned(toggle)<<31;
  tick();check(t.gpi==previous,"payload without toggle");toggle=!toggle;t.gpo=fields|unsigned(toggle)<<31;
  unsigned cycles=0;while(bool(t.gpi&0x800000)!=toggle&&cycles++<2000000)tick();
  check(bool(t.gpi&0x800000)==toggle,"ACK timeout");auto result=t.gpi;
  for(unsigned i=0;i<10;++i)tick();check(t.gpi==result,"repeated command");return result;
 };
 // The fixture's run: a full-frame fill, then a texture blended at an odd
 // source phase, into slot 1.
 std::mt19937 random(959);
 for(uint32_t o=HEAP;o<HEAP+0x10000;o+=4){uint32_t v=random();if(random()%3==0)v&=0x00ffffffu;else if(random()%2)v|=0xff000000u;std::memcpy(at(o),&v,4);}
 const std::vector<Command> fixture_list={Command::make(FILL,0xff336699,0,0,W,H),Command::make(BLEND,0,101,50,64,32,HEAP+4,264)};
 put(0,fixture_list);mirror();reference([&](uint32_t o){return &image[o];},fixture_list,1);
 check(argc==2,"fixture path");std::ifstream fixture(argv[1]);check(bool(fixture),"fixture open");
 unsigned op,index,arg,expected,event;unsigned n=0;
 while(fixture>>op>>index>>arg>>expected>>event){
  if(event==1){
   for(unsigned i=0;i<2000000&&t.displayed_sequence!=1;++i)tick();check(t.displayed_sequence==1,"frame completion");
   // Scanout now shows slot 1: check one whole frame against it.
   unsigned frames=0,seen=0;
   while(frames<2){
    t.clk=0;t.eval();unsigned h=VIDEO(h),v=VIDEO(v);if(h==0&&v==0)seen=0;
    if(t.de){uint32_t pixel;std::memcpy(&pixel,at(SLOT+v*STRIDE+h*4),4);check(t.rgb==(pixel&0xffffffu),"scanout shows the blitted slot");++seen;}
    if(h==1649&&v==749)++frames;tick();
   }
   check(seen==921600&&!t.underflows,"blitted frame scanned out whole");
  }
  if(event==2){
   for(unsigned i=0;i<20000000&&PUBLISHED(completed_q)!=1;++i)tick();check(PUBLISHED(completed_q)==1,"blit completion");
   check(same_slot(1)&&same_slot(0),"blit matches gfx.blendOver reference and leaves slot 0");
  }
  check(command(op,index,arg)==expected,"golden exchange");++n;
 }
 check(n>60,"fixture coverage");check(t.quiesced&&t.exec_reset&&!t.faulted,"golden drained hold");
 command(2,0,1);command(20,0,1);
 unsigned before=count();for(unsigned i=0;i<2000000&&count()==before;++i)tick();check(count()>before,"restart");
 MODEL(pause_responses)=1;
 for(unsigned i=0;i<10000;++i)tick();toggle=!toggle;t.gpo=(20u<<24)|unsigned(toggle)<<31;
 for(unsigned i=0;i<1000;++i)tick();check(bool(t.gpi&0x800000)!=toggle&&!t.quiesced,"quiesce must wait");
 MODEL(pause_responses)=0;
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
 VIDEO(underflows)=0x1234ffffu;
 check((command(18,12,0)&0x40ffff)==65535,"counter low");
 VIDEO(underflows)=0x12350000u;
 check((command(18,13,0)&0x40ffff)==0x1234,"coherent high");
 check((command(18,13,0)&0x40ffff)==0x400004,"snapshot consumed");
 std::puts("PASS menu GP golden identity/config/sequence/frame/drain/restart/bounds and blit fixture");

 // Quiesce halts a running blit at a row and ACKs only once its ports drain.
 check((command(20,0,1)&0x40ffff)==0,"enable for blits");
 put(100,{Command::make(FILL_BLEND,0x80ff0000,0,0,W,H)});
 check((command(22,0,100)&0x40ffff)==0&&(command(22,1,1)&0x40ffff)==0&&(command(22,2,0)&0x40ffff)==0,"long blit runs");
 for(unsigned i=0;i<20000;++i)tick();
 check((command(22,3,0)&0x40ffff)==1,"long blit busy");
 MODEL(pause_responses)=1;
 for(unsigned i=0;i<2000;++i)tick();toggle=!toggle;t.gpo=(20u<<24)|unsigned(toggle)<<31;
 for(unsigned i=0;i<20000;++i)tick();check(bool(t.gpi&0x800000)!=toggle&&!t.quiesced,"quiesce waits for the blitter");
 MODEL(pause_responses)=0;
 for(unsigned i=0;i<20000&&bool(t.gpi&0x800000)!=toggle;++i)tick();
 check(bool(t.gpi&0x800000)==toggle&&t.quiesced,"quiesce drains the blitter");
 // The fixture's execution hold reset the count, so the halted run leaves it at 0.
 check((command(22,3,0)&0x40ffff)==0,"halted blitter is idle");
 check((command(22,4,0)&0x40ffff)==0,"halted run is not counted");
 check((command(22,0,0)&0x40ffff)==0x400004,"quiesced blitter refuses staging");
 // A bad command faults the blitter until execution reset.
 check((command(20,0,1)&0x40ffff)==0,"enable after halt");
 put(200,{Command::make(7,0,0,0,1,1)});
 mirror();
 check((command(22,0,200)&0x40ffff)==0&&(command(22,1,1)&0x40ffff)==0&&(command(22,2,0)&0x40ffff)==0,"bad list runs");
 for(unsigned i=0;i<2000;++i)tick();
 check((command(22,3,0)&0x40ffff)==0x0202,"bad op faults with its code");
 check((command(22,0,0)&0x40ffff)==0x400004,"faulted blitter refuses staging");
 check(same_slot(0)&&same_slot(1),"faulting command writes nothing");
 check((command(20,0,0)&0x40ffff)==0&&(command(2,0,0)&0x40ffff)==0&&(command(2,0,1)&0x40ffff)==0,"hold and release");
 check((command(22,3,0)&0x40ffff)==0&&(command(22,4,0)&0x40ffff)==0,"execution reset clears the fault");
 std::puts("PASS menu blitter GP quiesce halt/drain and fault containment");
}
