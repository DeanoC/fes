// SPDX-License-Identifier: GPL-2.0-or-later
// reSID source-oracle replay; see ../rtl/c64_sid.COPYING and the core README.
#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#ifdef C64_OSC_ORACLE
#include "wave.h"
class Oracle:public reSID::WaveformGenerator {
public:
 unsigned control=0;
 void restart(){reset();control=0;}
 void write(unsigned a,unsigned d){switch(a){case 0:writeFREQ_LO(d);break;case 1:writeFREQ_HI(d);break;case 2:writePW_LO(d);break;case 3:writePW_HI(d);break;case 4:writeCONTROL_REG(d);control=d;break;}}
 std::array<unsigned,12> state(){return {accumulator,freq,pw,control,shift_register,unsigned(shift_pipeline),unsigned(shift_register_reset),unsigned(floating_output_ttl),pulse_output,waveform_output,readOSC(),0};}
};
#else
#include "Voscillator_sim_top.h"
#include "verilated.h"
#include "sid_oscillator_vectors.h"
#endif
int main(int argc,char**argv){
#ifdef C64_OSC_ORACLE
 Oracle model;model.set_chip_model(reSID::MOS6581);
 std::puts("// SPDX-License-Identifier: MIT\n// Unmodified VICE 3.10 reSID MOS6581 digital oscillator state.\n#pragma once\n#include <cstdint>\nstruct OscExpected {uint64_t hash,edge; unsigned state[12];};\nstatic constexpr OscExpected expected[]={");
#else
 Verilated::commandArgs(argc,argv);Voscillator_sim_top model;
#endif
 const uint64_t trace_from=std::getenv("C64_OSC_TRACE_FROM")?std::strtoull(std::getenv("C64_OSC_TRACE_FROM"),nullptr,10):0;
 const uint64_t trace_to=std::getenv("C64_OSC_TRACE_TO")?std::strtoull(std::getenv("C64_OSC_TRACE_TO"),nullptr,10):0;
 uint64_t edges=0,hash=14695981039346656037ull;unsigned chunks=0,phi_edges=0,writes=0,tests=0;std::array<unsigned,12> state{};const char*scenario="powerup";
 auto flush=[&](){
#ifdef C64_OSC_ORACLE
  std::printf(" {0x%016llxull,%lluull,{",(unsigned long long)hash,(unsigned long long)edges);for(auto v:state)std::printf("%uu,",v);std::puts("}},");
#else
  if(chunks>=sizeof(expected)/sizeof(expected[0]) || expected[chunks].hash!=hash || expected[chunks].edge!=edges){
   std::fprintf(stderr,"oscillator mismatch scenario=%s chunk=%u SYS=%llu hash=%016llx\n",scenario,chunks,(unsigned long long)edges,(unsigned long long)hash);
   for(unsigned i=0;i<state.size();++i)std::fprintf(stderr," field%u got=%x expected=%x\n",i,state[i],chunks<sizeof(expected)/sizeof(expected[0])?expected[chunks].state[i]:0);return false;
  }
#endif
  ++chunks;hash=14695981039346656037ull;return true;
 };
 auto step=[&](bool phi=false,bool we=false,unsigned addr=0,unsigned data=0,bool reset=false){
#ifdef C64_OSC_ORACLE
  if(reset)model.restart();else{if(phi){model.clock();model.set_waveform_output();}if(we)model.write(addr,data);}state=model.state();
#else
  model.clk=0;model.reset=reset;model.phi=phi;model.we=we;model.addr=addr;model.din=data;model.eval();model.clk=1;model.eval();
  state={model.phase,model.freq,model.pw,model.control,model.lfsr,model.shift_pipe,model.reset_count,model.fade_count,model.pulse,model.waveform,model.osc,0};
#endif
  // reSID's shiftreg_bitfade() leaves unused C++ bits above bit22 set.
  // Hardware has 23 cells. Compare the hardware bits and only an active reset
  // countdown; a completed all-ones LFSR has no further visible fade step.
  state[4]&=0x7fffff;
  if(!(state[3]&8) || state[4]==0x7fffff)state[6]=0;
  if(trace_from && edges+1>=trace_from && edges+1<=trace_to){std::fprintf(stderr,"TRACE %llu",(unsigned long long)(edges+1));for(auto v:state)std::fprintf(stderr," %x",v);std::fputc('\n',stderr);}
  for(auto value:state)for(unsigned byte=0;byte<4;++byte){hash^=(value>>(8*byte))&255;hash*=1099511628211ull;}
  ++edges;if(phi&&!reset)++phi_edges;if(we&&!reset)++writes;
  if(edges%8192==0 && !flush())std::exit(1);
 };
 auto boundary=[&](const char*name){if(edges%8192 && !flush())std::exit(1);scenario=name;++tests;};
 auto write=[&](unsigned a,unsigned d,bool phi=false){step(phi,true,a,d);};
 auto freq=[&](unsigned f){write(0,f&255);write(1,f>>8);};
 auto cycles=[&](unsigned n){for(unsigned i=0;i<n;++i)step(true);};
 step(false,false,0,0,true);step();
 const unsigned frequencies[]={0,1,0x7fff,0x8000,0xffff,0x1234,0x0400};
 for(unsigned f:frequencies){boundary("noise frequency and bit19 pipeline");step(false,false,0,0,true);freq(f);write(4,0x81);cycles(f==1?600000:4096);}
 boundary("warm reset phase preservation and simultaneous reset/write/phi");freq(0xfff1);cycles(137);step(true,true,4,0x88,true);step(false,false,0,0,true);write(4,0x81);cycles(9);
 for(unsigned shift=0;shift<4;++shift){
  boundary("TEST short pulses at noise-pipeline offsets");freq(0xffff);write(4,0x81);cycles(29+shift);write(4,0x89,true);for(unsigned n=0;n<53;++n)step();cycles(shift);write(4,0x81,shift&1);cycles(21);
 }
 for(unsigned delay:{0u,1u,34999u,35000u,35001u,35999u,36000u,58001u}){
  boundary("TEST long hold/reset/bitfade/fall");write(4,0x81);freq(0x9b37);cycles(153);write(4,0x89);cycles(delay);write(4,0x81);cycles(7);
 }
 boundary("no-waveform TEST and warm reset while held");write(4,0x09);cycles(35400);step(true,false,0,0,true);cycles(3);write(4,0x81);cycles(20);
 for(unsigned waveform:{0x10u,0x20u,0x40u,0x80u}){
  boundary("baseline single waves and pulse-width writes");write(4,waveform);freq(0x9abc);
  for(unsigned width:{0u,1u,0x555u,0xffeu,0xfffu}){write(2,width&255);write(3,width>>8,true);cycles(145);write(2,(width+1)&255,true);cycles(3);}
  write(4,waveform|8);cycles(3);write(4,waveform);cycles(12);
 }
 boundary("floating OSC3 retention and bitfade");write(4,0x40);write(2,0);write(3,0);cycles(1);write(4,0);cycles(201000);
 boundary("fractional production phi with writes between enabled edges");
 // The native bus write sees old cycle_clock==26: +27 SYS after Phi2.
 uint32_t fraction=0;unsigned since=0;uint32_t rng=0x65811541;auto random=[&](){rng^=rng<<13;rng^=rng>>17;rng^=rng<<5;return rng;};
 for(unsigned sys=0;sys<700000;++sys){fraction+=985248;bool phi=fraction>=52224000;if(phi){fraction-=52224000;since=0;}else ++since;unsigned r=random();unsigned a=(r>>16)%5,d=r>>24;if(a==4){const unsigned waves[]={0,16,32,64,128};d=waves[(r>>8)%5]|(d&9);}bool we=since==27 && (r&7)==0;step(phi,we,a,d);}
 boundary("writes at every relative SYS phase around phi");
 for(unsigned offset=0;offset<54;++offset){for(unsigned sys=0;sys<108;++sys)step(sys%54==0,sys==offset,4,offset&1?0x89:0x81);}
 boundary("seeded simultaneous phi/register mutations single-wave subset");
 for(unsigned i=0;i<100000;++i){unsigned r=random();unsigned a=(r>>16)%5,d=r>>24;if(a==4){const unsigned waves[]={0,16,32,64,128};d=waves[(r>>8)%5]|(d&9);}step((r&3)!=0,(r&15)<3,a,d);}
 // CPU writes prepare every table address through the ordinary accumulator.
 // Keep all preceding regressions intact; no hidden state is injected.
 auto position=[&](unsigned phase){
  write(4,0x28);freq(0xffff);write(4,0x20);
  cycles(phase/0xffff);freq(phase%0xffff);cycles(1);freq(0);
 };
 for(unsigned selected:{0x30u,0x50u,0x60u,0x70u})for(unsigned width:{0u,0xfffu})for(unsigned ring:{0u,4u}){
  boundary("all4096 combined-wave table addresses through CONTROL writes");
  for(unsigned index=0;index<4096;++index){
   position(index<<12);write(2,width&255);write(3,width>>8);
   write(4,selected|ring);write(4,selected|ring);cycles(2);
  }
 }
 for(unsigned selected:{0x30u,0x60u,0x70u})for(unsigned target:{0x7fffffu,0x800000u,0x83f000u,0x85f000u,0xfff000u})for(unsigned width:{0u,0xfffu}){
  boundary("combined saw phase feedback and delayed pulse around MSB");
  position(target);write(2,width&255);write(3,width>>8);write(4,selected);
  freq(1);cycles(3);write(4,selected,true);cycles(2);
  write(2,0,true);write(3,0);freq(0xffff);cycles(7);
  write(4,selected|8,true);cycles(3);write(4,selected);cycles(4);
  step(true,true,4,selected,true);cycles(2);
 }
 for(unsigned selected:{0x1cu,0x5cu})for(unsigned same_phi:{0u,1u}){
  boundary("self-ring TEST rise observes the cleared source phase");
  position(0x800000);write(2,0);write(3,0);
  write(4,selected,same_phi);write(4,selected);cycles(2);
  write(4,selected&~8u);cycles(2);
 }
 boundary("seeded combined-wave writes on Phi and idle SYS edges");
 for(unsigned i=0;i<100000;++i){unsigned r=random(),a=(r>>16)%5,d=r>>24;if(a==4){const unsigned waves[]={0,16,32,48,64,80,96,112,128};d=waves[(r>>8)%9]|(d&9);}step((r&3)!=0,(r&15)<3,a,d);}

 for(unsigned select=9;select<16;++select)for(unsigned f:{0u,0x9b37u,0xffffu}){
  boundary("noise-combination waveform/writeback through CPU controls");
  step(false,false,0,0,true);freq(f);write(2,0x55);write(3,5);write(4,0x81);cycles(37);
  write(4,(select<<4)|1);cycles(4096);write(4,0x81);cycles(27);
  write(4,(select<<4)|9,true);cycles(13);write(4,(select<<4)|1,true);cycles(31);
 }
 for(unsigned before=0;before<16;++before)for(unsigned after=0;after<16;++after)for(unsigned same_phi:{0u,1u}){
  boundary("all selector-pair TEST falling pre-writeback and repeated CONTROL");
  step(false,false,0,0,true);freq(0xffff);write(2,0);write(3,0);write(4,0x81);cycles(79);
  write(4,(before<<4)|8);cycles(3+(before%3));write(4,after<<4,same_phi);cycles(8);write(4,after<<4,same_phi);cycles(4);
 }
 boundary("seeded all16 selector and TEST/register timing mutations");
 for(unsigned i=0;i<300000;++i){unsigned r=random(),a=(r>>16)%5,d=r>>24;step((r&3)!=0,(r&15)<3,a,d);}
 if(edges%8192 && !flush())return 1;
#ifdef C64_OSC_ORACLE
 std::puts("};");
#else
 if(chunks!=sizeof(expected)/sizeof(expected[0]))return 1;
 std::printf("SID oscillator PASS: %u cases, %llu SYS edges, %u Phi2 edges, %u writes, %u independent state hashes\n",tests,(unsigned long long)edges,phi_edges,writes,chunks);
#endif
}
