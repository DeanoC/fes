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
#ifdef C64_PREPARED_PHASE
 bool previous_phi=false;uint64_t prepared_idle=0;
#endif
 std::array<unsigned,3> clocked{};
#ifdef C64_COUPLING_LIVE
 std::array<unsigned,3> expected_clocked{};
#endif
 uint64_t edges=0,phi_edges=0,writes=0,hash=14695981039346656037ull;unsigned chunks=0,cases=0;const char *scenario="powerup";
 uint64_t rise_sync_seen=0,suppressed=0,clears=0,test_held_rise=0;unsigned ring_sources=0;uint64_t blocked_writes=0;
 auto flush=[&](){
#ifdef C64_OSC_ORACLE
  std::printf("{0x%016llxull,%lluull},\n",(unsigned long long)hash,(unsigned long long)edges);
#elif !defined(C64_COUPLING_LIVE)
  if(chunks>=std::size(expected_coupling)||expected_coupling[chunks].hash!=hash||expected_coupling[chunks].edge!=edges){std::fprintf(stderr,"coupling mismatch scenario=%s chunk=%u SYS=%llu hash=%016llx\n",scenario,chunks,(unsigned long long)edges,(unsigned long long)hash);std::exit(1);}
#endif
  ++chunks;hash=14695981039346656037ull;
 };
 auto step=[&](bool phi=false,bool we=false,unsigned addr=0,unsigned data=0,bool reset=false,bool selected=true){
#if defined(C64_OSC_ORACLE) || defined(C64_COUPLING_LIVE)
  if(reset){for(auto&r:ref)r.restart();}else{
   if(phi){for(auto&r:ref)r.clock();for(auto&r:ref)r.synchronize();for(auto&r:ref)r.set_waveform_output();}
   for(unsigned v=0;v<3;++v)clocked[v]=ref[v].state()[9];
#ifdef C64_COUPLING_LIVE
   expected_clocked=clocked;
#endif
   if(we&&selected&&addr<21)ref[addr/7].write(addr%7,data);
  }
#endif
#ifndef C64_OSC_ORACLE
#ifdef C64_PREPARED_PHASE
  // Extra no-write SYS edges leave the logical source-oracle sequence intact.
  // Dense logical Phi calls exercise the exactly2-clock preparation boundary.
  if(phi&&previous_phi){
   rtl.clk=0;rtl.reset=0;rtl.phi=0;rtl.we=0;rtl.cs=0;rtl.eval();
   rtl.clk=1;rtl.eval();++prepared_idle;
  }
  previous_phi=phi;
#endif
#ifdef C64_SEQUENCED_VOICES
  // One logical Phi is the Phi edge, the three voice stage edges and then
  // the write edge: the source oracle clocks before it writes, and the
  // stage window must be write-free. Audio captures on the third stage.
  // Three three-edge lookup/finish stages follow Phi (Phi+1..Phi+9); audio
  // captures on Phi+10. A write finishes two edges later and one more idle
  // edge refreshes the prepared phase sum, as the motherboard guarantees.
  auto idle=[&](){rtl.clk=0;rtl.reset=0;rtl.phi=0;rtl.we=0;rtl.cs=0;rtl.eval();rtl.clk=1;rtl.eval();++prepared_idle;};
  if(phi){
   rtl.clk=0;rtl.reset=reset;rtl.phi=1;rtl.we=0;rtl.cs=0;rtl.addr=addr;rtl.din=data;rtl.eval();rtl.clk=1;rtl.eval();
   if(!reset){
    for(unsigned stage=0;stage<9;++stage)idle();
    rtl.clk=0;rtl.reset=0;rtl.phi=0;rtl.we=0;rtl.cs=0;rtl.eval();
    for(unsigned v=0;v<3;++v)clocked[v]=rtl.clocked[v];
#ifdef C64_COUPLING_LIVE
    if(clocked!=expected_clocked){std::fprintf(stderr,"pre-write audio waveform mismatch %s SYS=%llu\n",scenario,(unsigned long long)edges);std::exit(1);}
#endif
    rtl.clk=1;rtl.eval();++prepared_idle;
   }
   if(we&&!reset){rtl.clk=0;rtl.reset=0;rtl.phi=0;rtl.we=1;rtl.cs=selected;rtl.addr=addr;rtl.din=data;rtl.eval();rtl.clk=1;rtl.eval();idle();idle();idle();}
  }else{
   rtl.clk=0;rtl.reset=reset;rtl.phi=0;rtl.we=we;rtl.cs=we&&selected;rtl.addr=addr;rtl.din=data;rtl.eval();
   for(unsigned v=0;v<3;++v)clocked[v]=rtl.clocked[v];
   rtl.clk=1;rtl.eval();
   if(we&&!reset){idle();idle();idle();}
  }
#else
  rtl.clk=0;rtl.reset=reset;rtl.phi=phi;rtl.we=we;rtl.cs=we&&selected;rtl.addr=addr;rtl.din=data;rtl.eval();
  for(unsigned v=0;v<3;++v)clocked[v]=rtl.clocked[v];
#ifdef C64_COUPLING_LIVE
  if(phi&&!reset&&clocked!=expected_clocked){std::fprintf(stderr,"pre-write audio waveform mismatch %s SYS=%llu\n",scenario,(unsigned long long)edges);std::exit(1);}
#endif
  rtl.clk=1;rtl.eval();
#endif
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
   if(phi&&!reset)for(unsigned b=0;b<4;++b){hash^=(clocked[v]>>(8*b))&255;hash*=1099511628211ull;}
  }
  if(phi&&!reset){rise_sync_seen|=1ull<<(rise_mask*8+sync_mask);for(unsigned v=0;v<3;++v)if((sync_mask&(1u<<v))&&(rise_mask&(1u<<((v+2)%3)))){if((sync_mask&(1u<<((v+2)%3)))&&(rise_mask&(1u<<((v+1)%3))))++suppressed;else ++clears;}}
  ++edges;if(phi&&!reset)++phi_edges;if(we&&!reset){if(selected)++writes;else ++blocked_writes;}if(edges%8192==0)flush();
 };
 auto boundary=[&](const char *name){if(edges%8192)flush();scenario=name;++cases;};
 auto wr=[&](unsigned v,unsigned a,unsigned d,bool phi=false){step(phi,true,v*7+a,d);};
 auto freq=[&](unsigned v,unsigned f){wr(v,0,f&255);wr(v,1,f>>8);};
 auto clocks=[&](unsigned n){for(unsigned i=0;i<n;++i)step(true);};
 step(false,false,0,0,true);
 if(argc==1){
 for(unsigned selected:{0x10u,0x50u})for(unsigned rises=0;rises<8;++rises)for(unsigned sync=0;sync<8;++sync)for(unsigned ring=0;ring<8;++ring){
  boundary("all simultaneous rise/sync/ring masks");
  for(unsigned v=0;v<3;++v){wr(v,4,selected|8);freq(v,(rises&(1<<v))?0x8000:0);}
  for(unsigned v=0;v<3;++v)wr(v,4,selected|((sync>>v)&1)*2|((ring>>v)&1)*4);
  clocks(520);
 }
 for(unsigned source=0;source<3;++source){
  boundary("TEST retains a just-risen sync source edge latch");
  for(unsigned v=0;v<3;++v){wr(v,4,0x18);freq(v,v==source?0x8000:0xffff);wr(v,4,0x17);}
  clocks(256);wr(source,4,0x1f);clocks(19);wr(source,4,0x17);clocks(19);
 }
 for(unsigned wave:{0u,0x10u,0x20u,0x40u,0x50u,0x80u,0x90u,0xa0u,0xb0u,0xc0u,0xd0u,0xe0u,0xf0u})for(unsigned mask=0;mask<8;++mask){
  boundary("single waves/noise with asymmetric hard sync and ring");
  for(unsigned v=0;v<3;++v){freq(v,0xfff1-v*0x3311);wr(v,2,v*79);wr(v,3,2+v);wr(v,4,wave|((mask>>v)&1)*2|5);}
  clocks(3333);
 }
 boundary("warm reset and simultaneous phi/write/reset");step(true,true,4,0x1f,true);clocks(13);
 boundary("fractional53/54 cadence, native write phase27");
 uint32_t fraction=0,rng=0x6581c064;unsigned since=0;
 auto random=[&](){rng^=rng<<13;rng^=rng>>17;rng^=rng<<5;return rng;};
 for(unsigned sys=0;sys<700000;++sys){fraction+=985248;bool phi=fraction>=52224000;if(phi){fraction-=52224000;since=0;}else ++since;unsigned r=random(),a=(r>>16)%21,d=r>>24;if(a%7==4){const unsigned waves[]={0,16,32,48,64,80,96,112,128,144,160,176,192,208,224,240};d=waves[(r>>8)%16]|(d&15);}step(phi,since==27&&(r&3)==0,a,d);}
 boundary("control writes at every SYS phase around phi");
 for(unsigned offset=0;offset<54;++offset)for(unsigned v=0;v<3;++v)for(unsigned sys=0;sys<108;++sys)step(sys%54==0,sys==offset,v*7+4,0x11|((offset&7)<<1));
 boundary("dense simultaneous phi/register mutation");
 for(unsigned i=0;i<100000;++i){unsigned r=random(),a=(r>>16)%21,d=r>>24;if(a%7==4){const unsigned waves[]={0,16,32,48,64,80,96,112,128,144,160,176,192,208,224,240};d=waves[(r>>8)%16]|(d&15);}step((r&3)!=0,(r&15)<3,a,d);}
 // All original scenarios above remain; the source oracle also hashes the
 // post-Phi/pre-write waveform used by the audio pipeline.
 auto position=[&](unsigned v,unsigned phase){
  wr(v,4,0x28);freq(v,0xffff);wr(v,4,0x20);
  clocks(phase/0xffff);freq(v,phase%0xffff);clocks(1);freq(v,0);
 };
 for(unsigned source=0;source<3;++source)for(unsigned selected:{0x30u,0x60u,0x70u,0xa0u,0xb0u,0xe0u,0xf0u})for(unsigned ring_wave:{0x14u,0x54u})for(unsigned same_phi:{0u,1u})for(unsigned cadence:{2u,53u,54u,64u}){
  boundary("combined-saw feedback in every source/ring rotation and write view");
  for(unsigned v=0;v<3;++v){freq(v,0);wr(v,4,0x28);}
  unsigned dest=(source+1)%3;
  position(dest,0x245678);position(source,0x7fffff);
  wr(dest,2,0);wr(dest,3,0);wr(dest,4,ring_wave);
  wr(source,2,0);wr(source,3,0);wr(source,4,selected);freq(source,1);
  // This Phi crosses bit23 before source feedback. A same-edge destination
  // CONTROL write observes final feedback; its audio capture observes Phi output.
  step(true,same_phi,dest*7+4,ring_wave);
  for(unsigned idle=1;idle<cadence;++idle)step();
  for(unsigned i=0;i<8;++i){step(true);for(unsigned idle=1;idle<cadence;++idle)step();}
  // A last-idle source CONTROL write clears phase after the prepared helper
  // would have computed the old sum. Exactly one idle edge is available.
  wr(source,4,0x20);position(source,0x800000);freq(source,0xffff);
  wr(source,4,selected);step(true);step();step(true);
  wr(source,4,selected,true);step();step(true);
 }
 boundary("fractional53/54 native phase27 writes including all16 waveform selectors");
 fraction=0;since=0;rng=0xc0640367;unsigned combined_seen=0;
 for(unsigned sys=0;sys<700000;++sys){
  fraction+=985248;bool phi=fraction>=52224000;if(phi){fraction-=52224000;since=0;}else ++since;
  unsigned r=random(),a=(r>>16)%21,d=r>>24;bool we=since==27&&(r&3)==0,selected=(r&15)!=0;
  if(a%7==4){const unsigned waves[]={0,16,32,48,64,80,96,112,128,144,160,176,192,208,224,240};d=waves[(r>>8)%16]|(d&15);if(we&&selected)combined_seen|=1u<<(d>>4);}
  step(phi,we,a,d,false,selected);
 }
 if((combined_seen&0xe8)!=0xe8||!blocked_writes){std::fprintf(stderr,"incomplete native combined/CS coverage mask=%x blocked=%llu\n",combined_seen,(unsigned long long)blocked_writes);return 1;}
 boundary("seeded all16 waveform selectors, coupled sync/ring and same-Phi writes");
 for(unsigned i=0;i<200000;++i){unsigned r=random(),a=(r>>16)%21,d=r>>24;if(a%7==4){const unsigned waves[]={0,16,32,48,64,80,96,112,128,144,160,176,192,208,224,240};d=waves[(r>>8)%16]|(d&15);}step((r&3)!=0,(r&15)<3,a,d);}
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
#ifdef C64_PREPARED_PHASE
 std::printf("SID phase preparation: %llu extra idle SYS edges; unchanged source-oracle logical sequence\n",(unsigned long long)prepared_idle);
#endif
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
