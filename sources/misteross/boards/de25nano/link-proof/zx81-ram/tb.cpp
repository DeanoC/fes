#include "Vfes_zx81_pr_sim.h"
#include "verilated.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <array>
static void need(bool b,const char*s){if(!b)throw std::runtime_error(s);}
struct Bench {
 Vfes_zx81_pr_sim d;
 uint64_t cycles=0,writes=0,freeze_cycle=0,last_write=0;
 uint16_t ramtop=0; unsigned observed=0,seq=0;
 uint64_t stop_cycle=0;
 Bench(){d.clk=0;d.reset_n=0;d.persona_b=0;d.address=0;d.read=0;d.write=0;d.writedata=0;d.debug_bus=0;d.debug_request=0;d.eval();for(int i=0;i<8;i++)tick();d.reset_n=1;for(int i=0;i<16;i++)tick();need(d.frozen,"cold freeze missing");}
 void tick(){
  d.clk=0;d.eval();
  bool was_running=d.running;
  if(d.memory_write){need(!d.frozen,"memory write after Frozen");writes++;last_write=cycles;}
  // Independently observe the CPU's bus writes; never initialize or inject RAMTOP.
  uint64_t r=d.machine_request;
  if(d.running && !(r&(1ull<<24)) && !(r&(1ull<<27))){
   if((r&65535)==0x4004){ramtop=(ramtop&0xff00)|((r>>16)&255);observed|=1;}
   if((r&65535)==0x4005){ramtop=(ramtop&255)|((r>>8)&0xff00);observed|=2;}
  }
  d.clk=1;d.eval();cycles++;
  if(was_running&&!d.running)stop_cycle=cycles;
  if(!d.running && stop_cycle && cycles>stop_cycle+3)need(!d.memory_write,"bus write did not drain within three edges");
  if(d.frozen){need(!d.running && d.slot_reset,"Frozen must hold reset");need(!d.memory_write,"Frozen includes pending write");}
 }
 void ticks(unsigned n){for(unsigned i=0;i<n;i++)tick();}
 void wr(unsigned a,uint32_t v){d.address=a;d.writedata=v;d.write=1;tick();need(d.writeresponsevalid&&d.response==0,"MMIO write failed");d.write=0;tick();}
 uint32_t rd(unsigned a){d.address=a;d.read=1;tick();need(d.readdatavalid,"MMIO read failed");auto v=d.readdata;d.read=0;tick();return v;}
 void command(unsigned op,uint32_t arg){wr(12,arg);wr(16,(op<<24)|++seq);for(unsigned i=0;i<100;i++){need(rd(20)==0,"controller fault");if(rd(24)==seq)return;}throw std::runtime_error("command timeout");}
 void select(bool ram){need(d.frozen,"selection while running");d.persona_b=ram;ramtop=0;observed=0;command(2,ram?0xc002b002:0xc001a001);need(d.running,"resume missing");}
 void boot(uint16_t expected){uint64_t start=cycles;for(unsigned i=0;i<770000000;i++){tick();if(observed==3&&ramtop==expected&&d.cpu_addr==0x04cf){ticks(10000);std::cout<<"BOOT persona="<<int(d.persona_b)<<" RAMTOP="<<std::hex<<ramtop<<std::dec<<" cycles="<<cycles-start<<" CPUwrite-observed=1\n";return;}}std::cerr<<"ramtop="<<std::hex<<ramtop<<" pc="<<d.cpu_addr<<std::dec<<" observed="<<observed<<"\n";throw std::runtime_error("BASIC RAM sizing failed");}
 uint8_t peek(unsigned offset){for(unsigned i=0;i<10000;i++){if(d.diagnostic_address==offset){ticks(7);auto v=d.peek_data;need((rd(36)&255)==v,"CSR LAST_DATA differs from real machine peek");return v;}tick();}throw std::runtime_error("peek scan timeout");}
 void freeze(){command(1,0);need(d.frozen,"freeze missing");freeze_cycle=cycles;auto old=writes;auto retired=d.retired;auto response=d.static_response;ticks(20);need(d.static_response==response,"response changed after Frozen");need(writes==old&&d.retired==retired,"activity after Frozen");std::cout<<"FREEZE cycles="<<freeze_cycle<<" lastRAMwrite="<<last_write<<" remainingWrites=0\n";}
 uint64_t packed(unsigned a,unsigned data,bool write){return (1ull<<45)|(uint64_t(a&0x3fff)<<30)|(1ull<<25)|(3ull<<28)|(uint64_t(!write)<<27)|(uint64_t(data)<<16)|a;}
 void memory_test(){
  d.debug_bus=1;
  for(unsigned pass=0;pass<2;pass++){
   auto pattern=[&](unsigned a){return ((a>>(pass*8))^0xa5)&255;};
   for(unsigned a=0;a<16384;a++){d.debug_request=packed(0x4000+a,pattern(a),true);ticks(4);}
   for(unsigned a=0;a<16384;a++){d.debug_request=packed(0x4000+a,0,false);ticks(6);need((d.debug_response&255)==pattern(a),"RAM pack read/write differs");need(((d.debug_response>>8)&255)==pattern(a),"RAM pack peek differs");}
  }
  d.debug_request=packed(0x4000,0x39,true);ticks(6);
  d.debug_request=packed(0x8000,0xa7,true);ticks(6);
  d.debug_request=packed(0x4000,0,false);ticks(6);need((d.debug_response&255)==0x39,"out-of-range write aliased RAM");
  std::cout<<"RAM read/write/peek checked16384addresses twoaddresspatterns last7fff=1 outOfRange8000NoWrite=1\n";
  // Stop with writes at each pipeline phase, including same-edge command acceptance.
  for(unsigned phase=0;phase<6;phase++){
   d.debug_request=packed(0x7ffe,0x91+phase,true);ticks(phase+1);
   // Debug requests must obey the same reset/run gate as the static request.
   wr(12,0);wr(16,(1u<<24)|++seq);
   d.debug_request&=~(1ull<<45);
   for(unsigned n=0;n<100&&!d.frozen;n++)tick();need(d.frozen,"write-phase freeze timeout");
   auto old=writes;ticks(16);need(writes==old,"write escaped Frozen");
   std::cout<<"DRAIN phase="<<phase<<" postFrozenWrites=0\n";
   if(phase!=5){command(2,0xc002b002);need(d.running,"phase resume failed");}
  }
  d.debug_bus=0;
 }
};
int main(int argc,char**argv){std::cout.setf(std::ios::unitbuf);Verilated::commandArgs(argc,argv);try{Bench b;b.select(false);b.boot(0x4400);need(b.peek(4)==0x00&&b.peek(5)==0x44,"internal RAMTOP peek mismatch");b.freeze();b.select(true);b.boot(0x8000);need(b.peek(4)==0x00&&b.peek(5)==0x80,"pack RAMTOP peek mismatch");b.memory_test();b.select(false);b.boot(0x4400);need(b.peek(5)==0x44,"selection did not reset/re-size machine");b.freeze();std::cout<<"PASS actual ZX81 CPU+BASIC ROM, empty/RAM16K/reset/drain\n";}catch(const std::exception&e){std::cerr<<e.what()<<"\n";return 1;}}
