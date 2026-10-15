// SPDX-License-Identifier: GPL-2.0-or-later
// Pinned reSID three-voice digital coupling oracle and portable replay.
#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <string>
#if defined(C64_OSC_ORACLE) || defined(C64_COUPLING_LIVE)
#include "wave.h"
class Oracle:public reSID::WaveformGenerator {
public:
 unsigned control=0;
 void restart(){reset();control=0;}
 void write(unsigned a,unsigned d){switch(a){case 0:writeFREQ_LO(d);break;case 1:writeFREQ_HI(d);break;case 2:writePW_LO(d);break;case 3:writePW_HI(d);break;case 4:writeCONTROL_REG(d);control=d;break;}}
 std::array<unsigned,12> state(){return {accumulator,freq,pw,control,shift_register,unsigned(shift_pipeline),unsigned(shift_register_reset),unsigned(floating_output_ttl),pulse_output,waveform_output,readOSC(),unsigned(msb_rising)};}
};
#endif
#ifndef C64_OSC_ORACLE
#include "Vsid_coupling_sim_top.h"
#include "verilated.h"
#ifndef C64_COUPLING_LIVE
#include "sid_coupling_vectors.h"
#endif
#endif
int main(int argc,char **argv){
#if defined(C64_OSC_ORACLE) || defined(C64_COUPLING_LIVE)
 Oracle ref[3];for(unsigned v=0;v<3;++v){ref[v].set_chip_model(reSID::MOS6581);ref[v].set_sync_source(&ref[(v+2)%3]);}
#endif
#ifndef C64_OSC_ORACLE
 Verilated::commandArgs(argc,argv);Vsid_coupling_sim_top rtl;
#else
 std::puts("// SPDX-License-Identifier: MIT\n// Unmodified VICE3.10 reSID MOS6581 three-voice hard-sync/triangle-ring state.\n#pragma once\n#include <cstdint>\nstruct CouplingExpected {uint64_t hash,edge;};\nstatic constexpr CouplingExpected expected_coupling[]={");
#endif
 uint64_t edges=0,phi_edges=0,writes=0,hash=14695981039346656037ull;unsigned chunks=0,cases=0;const char *scenario="powerup";
 uint64_t rise_sync_seen=0,suppressed=0,clears=0,test_held_rise=0;unsigned ring_sources=0;
 auto flush=[&](){
#ifdef C64_OSC_ORACLE
  std::printf("{0x%016llxull,%lluull},\n",(unsigned long long)hash,(unsigned long long)edges);
#elif !defined(C64_COUPLING_LIVE)
  if(chunks>=std::size(expected_coupling)||expected_coupling[chunks].hash!=hash||expected_coupling[chunks].edge!=edges){std::fprintf(stderr,"coupling mismatch scenario=%s chunk=%u SYS=%llu hash=%016llx\n",scenario,chunks,(unsigned long long)edges,(unsigned long long)hash);std::exit(1);}
#endif
  ++chunks;hash=14695981039346656037ull;
 };
 auto step=[&](bool phi=false,bool we=false,unsigned addr=0,unsigned data=0,bool reset=false){
#if defined(C64_OSC_ORACLE) || defined(C64_COUPLING_LIVE)
  if(reset){for(auto&r:ref)r.restart();}else{
   if(phi){for(auto&r:ref)r.clock();for(auto&r:ref)r.synchronize();for(auto&r:ref)r.set_waveform_output();}
   if(we&&addr<21)ref[addr/7].write(addr%7,data);
  }
#endif
#ifndef C64_OSC_ORACLE
  rtl.clk=0;rtl.reset=reset;rtl.phi=phi;rtl.we=we;rtl.cs=we;rtl.addr=addr;rtl.din=data;rtl.eval();rtl.clk=1;rtl.eval();
#endif
  unsigned rise_mask=0,sync_mask=0;
  for(unsigned v=0;v<3;++v){
#ifdef C64_OSC_ORACLE
   auto state=ref[v].state();
#else
   std::array<unsigned,12> state={rtl.phase[v],rtl.freq[v],rtl.pw[v],rtl.control[v],rtl.lfsr[v],rtl.shift_pipe[v],rtl.reset_count[v],rtl.fade_count[v],rtl.pulse[v],rtl.waveform[v],rtl.osc[v],unsigned((rtl.rise>>v)&1)};
#endif
   auto normalize=[](std::array<unsigned,12> s){s[4]&=0x7fffff;if(!(s[3]&8)||s[4]==0x7fffff)s[6]=0;return s;};state=normalize(state);
#ifdef C64_COUPLING_LIVE
   auto want=normalize(ref[v].state());if(state!=want){std::fprintf(stderr,"LIVE coupling mismatch %s SYS=%llu voice=%u phi=%u we=%u addr=%u data=%x\n",scenario,(unsigned long long)edges,v,phi,we,addr,data);for(unsigned f=0;f<12;++f)std::fprintf(stderr," field%u got=%x expected=%x\n",f,state[f],want[f]);std::exit(1);}
#endif
   rise_mask|=(state[11]&1)<<v;sync_mask|=((state[3]>>1)&1)<<v;
   if(phi&&!reset&&(state[3]&8)&&state[11])++test_held_rise;
   if((state[3]&0xf4)==0x14){
#ifdef C64_OSC_ORACLE
    unsigned source_msb=(ref[(v+2)%3].state()[0]>>23)&1;
#else
    unsigned source_msb=(rtl.phase[(v+2)%3]>>23)&1;
#endif
    ring_sources|=1u<<(v*2+source_msb);
   }
   for(auto value:state)for(unsigned b=0;b<4;++b){hash^=(value>>(8*b))&255;hash*=1099511628211ull;}
  }
  if(phi&&!reset){rise_sync_seen|=1ull<<(rise_mask*8+sync_mask);for(unsigned v=0;v<3;++v)if((sync_mask&(1u<<v))&&(rise_mask&(1u<<((v+2)%3)))){if((sync_mask&(1u<<((v+2)%3)))&&(rise_mask&(1u<<((v+1)%3))))++suppressed;else ++clears;}}
  ++edges;if(phi&&!reset)++phi_edges;if(we&&!reset)++writes;if(edges%8192==0)flush();
 };
 auto boundary=[&](const char *name){if(edges%8192)flush();scenario=name;++cases;};
 auto wr=[&](unsigned v,unsigned a,unsigned d,bool phi=false){step(phi,true,v*7+a,d);};
 auto freq=[&](unsigned v,unsigned f){wr(v,0,f&255);wr(v,1,f>>8);};
 auto clocks=[&](unsigned n){for(unsigned i=0;i<n;++i)step(true);};
 step(false,false,0,0,true);
 if(argc==1){
 for(unsigned rises=0;rises<8;++rises)for(unsigned sync=0;sync<8;++sync)for(unsigned ring=0;ring<8;++ring){
  boundary("all simultaneous rise/sync/ring masks");
  for(unsigned v=0;v<3;++v){wr(v,4,0x18);freq(v,(rises&(1<<v))?0x8000:0);}
  for(unsigned v=0;v<3;++v)wr(v,4,0x10|((sync>>v)&1)*2|((ring>>v)&1)*4);
  clocks(520);
 }
 for(unsigned source=0;source<3;++source){
  boundary("TEST retains a just-risen sync source edge latch");
  for(unsigned v=0;v<3;++v){wr(v,4,0x18);freq(v,v==source?0x8000:0xffff);wr(v,4,0x17);}
  clocks(256);wr(source,4,0x1f);clocks(19);wr(source,4,0x17);clocks(19);
 }
 for(unsigned wave:{0u,0x10u,0x20u,0x40u,0x80u})for(unsigned mask=0;mask<8;++mask){
  boundary("single waves/noise with asymmetric hard sync and ring");
  for(unsigned v=0;v<3;++v){freq(v,0xfff1-v*0x3311);wr(v,2,v*79);wr(v,3,2+v);wr(v,4,wave|((mask>>v)&1)*2|5);}
  clocks(3333);
 }
 boundary("warm reset and simultaneous phi/write/reset");step(true,true,4,0x1f,true);clocks(13);
 boundary("fractional53/54 cadence, native write phase27");
 uint32_t fraction=0,rng=0x6581c064;unsigned since=0;
 auto random=[&](){rng^=rng<<13;rng^=rng>>17;rng^=rng<<5;return rng;};
 for(unsigned sys=0;sys<700000;++sys){fraction+=985248;bool phi=fraction>=52224000;if(phi){fraction-=52224000;since=0;}else ++since;unsigned r=random(),a=(r>>16)%21,d=r>>24;if(a%7==4){const unsigned waves[]={0,16,32,64,128};d=waves[(r>>8)%5]|(d&15);}step(phi,since==27&&(r&3)==0,a,d);}
 boundary("control writes at every SYS phase around phi");
 for(unsigned offset=0;offset<54;++offset)for(unsigned v=0;v<3;++v)for(unsigned sys=0;sys<108;++sys)step(sys%54==0,sys==offset,v*7+4,0x11|((offset&7)<<1));
 boundary("dense simultaneous phi/register mutation");
 for(unsigned i=0;i<100000;++i){unsigned r=random(),a=(r>>16)%21,d=r>>24;if(a%7==4){const unsigned waves[]={0,16,32,64,128};d=waves[(r>>8)%5]|(d&15);}step((r&3)!=0,(r&15)<3,a,d);}
 }
 if(argc>1){
#ifndef C64_COUPLING_LIVE
  std::fprintf(stderr,"externaltrace requires live source-oracle build\n");return 2;
#endif
  boundary("external reference music register stream");
  step(false,false,0,0,true);
  std::ifstream input(argv[1]);if(!input){std::perror("music trace");return 2;}
  const uint64_t limit=argc>2?std::strtoull(argv[2],nullptr,10):60ull*985248;
  uint64_t time=argc>3?std::strtoull(argv[3],nullptr,10):0,delta=0;unsigned a=0,d=0;uint64_t now=time,count=0;
  while(input>>delta>>a>>d){time+=delta;if(time>limit)break;if(a>=32||d>255)return 2;
   while(now<time){step(true);++now;}
   step(false,true,a,d);++count;
  }
  while(now<limit){step(true);++now;}
  std::fprintf(stderr,"Music digital replay: %llu writes through guest cycle %llu; idle SYS edges coalesced, Phi/write ordering retained\n",(unsigned long long)count,(unsigned long long)now);
 }
 if(edges%8192)flush();
 if(argc==1&&(rise_sync_seen!=~0ull||ring_sources!=63||!suppressed||!clears||!test_held_rise)){std::fprintf(stderr,"incomplete coupling coverage risesync=%016llx rings=%x suppression=%llu clears=%llu TEST=%llu\n",(unsigned long long)rise_sync_seen,ring_sources,(unsigned long long)suppressed,(unsigned long long)clears,(unsigned long long)test_held_rise);return 1;}
 if(argc==1)std::fprintf(stderr,"Coverage: all 64 rise/sync masks, all 6 voice/source-MSB ring pairs; suppression=%llu clear=%llu TEST-held-rise=%llu\n",(unsigned long long)suppressed,(unsigned long long)clears,(unsigned long long)test_held_rise);
#ifdef C64_OSC_ORACLE
 std::puts("};");
#else
#ifndef C64_COUPLING_LIVE
 if(chunks!=std::size(expected_coupling))return 1;
#endif
 std::printf("SID coupling PASS: %u cases, %llu SYS edges, %llu Phi2, %llu writes, %u hashes\n",cases,(unsigned long long)edges,(unsigned long long)phi_edges,(unsigned long long)writes,chunks);
#endif
}
