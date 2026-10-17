// SPDX-License-Identifier: MIT
// Same deterministic register stimulus for the independent reSID recorder and
// the offline RTL replay. No user firmware, recordings or reference engine is
// required by the regular simulation target.
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#ifdef C64_SID_ORACLE
#include "envelope.h"
class EnvelopeOracle:public reSID::EnvelopeGenerator {
public: unsigned level() const {return envelope_counter;}
};
#else
#include "Vc64_sid_sim_top.h"
#include "verilated.h"
#include "sid_vectors.h"
#endif
int main(int argc,char**argv){
#ifdef C64_SID_ORACLE
 EnvelopeOracle ref[3];unsigned control[3]={0,0,0},volume=0;
 std::puts("// SPDX-License-Identifier: MIT\n// Generated from unmodified VICE 3.10 reSID EnvelopeGenerator.\n#pragma once\n#include <cstdint>\nstatic constexpr uint64_t c64_sid_hashes[] = {");
#else
 Verilated::commandArgs(argc,argv);Vc64_sid_sim_top rtl;
#endif
 uint64_t hash=14695981039346656037ull,edges=0,writes=0,reads=0;unsigned chunk=0;
 auto flush=[&](){
#ifdef C64_SID_ORACLE
  std::printf("    0x%016llxull,\n",(unsigned long long)hash);
#else
  if(chunk>=sizeof(c64_sid_hashes)/sizeof(c64_sid_hashes[0]) || hash!=c64_sid_hashes[chunk]){
   std::fprintf(stderr,"SID reference mismatch chunk %u, SYS edge %llu, hash %016llx\n",chunk,(unsigned long long)edges,(unsigned long long)hash);std::exit(1);
  }
#endif
  ++chunk;hash=14695981039346656037ull;
 };
 auto step=[&](bool phi,bool cs,bool we,unsigned addr,unsigned data,bool reset=false){
  unsigned levels[3],dout;uint16_t sample;
#ifdef C64_SID_ORACLE
  int pcm=0;
  for(unsigned v=0;v<3;++v)pcm+=((control[v]&64)?127:-128)*int(ref[v].level());
  sample=uint16_t((pcm*int(volume))>>6);
  if(reset){for(auto &r:ref)r.reset();for(auto &c:control)c=0;volume=0;sample=0;}
  else {
   if(phi)for(auto &r:ref)r.clock();
   if(cs&&we){
    for(unsigned v=0;v<3;++v){
     if(addr==v*7+4){ref[v].writeCONTROL_REG(data);control[v]=data;}
     if(addr==v*7+5)ref[v].writeATTACK_DECAY(data);
     if(addr==v*7+6)ref[v].writeSUSTAIN_RELEASE(data);
    }
    if(addr==24)volume=data&15;
   }
  }
  for(unsigned v=0;v<3;++v)levels[v]=ref[v].level();
  dout=addr==28?ref[2].readENV():0;
#else
  rtl.clk=0;rtl.reset=reset;rtl.phi=phi;rtl.cs=cs;rtl.we=we;rtl.addr=addr;rtl.din=data;rtl.eval();
  rtl.clk=1;rtl.eval();levels[0]=rtl.e0;levels[1]=rtl.e1;levels[2]=rtl.e2;dout=rtl.dout;sample=rtl.sample;
#ifdef C64_SEQUENCED_VOICES
  // The three voice stage edges belong to the Phi step. The direct mix
  // register was read above, one edge after Phi, as in the direct build.
  for(unsigned extra=0;extra<((phi&&!reset)?7u:(cs&&we&&!reset)?2u:0u);++extra){
   rtl.clk=0;rtl.reset=0;rtl.phi=0;rtl.cs=0;rtl.we=0;rtl.eval();rtl.clk=1;rtl.eval();
  }
  levels[0]=rtl.e0;levels[1]=rtl.e1;levels[2]=rtl.e2;dout=rtl.dout;
#endif
#endif
  // PCM stimulus uses constant pulse output (frequency/width zero). This tests
  // envelope/gain arithmetic, not a claim about SID waveform or filter fidelity.
  const unsigned bytes[]={levels[0],levels[1],levels[2],dout,unsigned(sample&255),unsigned(sample>>8)};
  for(unsigned b:bytes){
   hash^=b;hash*=1099511628211ull;
  }
  ++edges;if(cs&&we&&!reset)++writes;if(addr==28&&!reset)++reads;
  if(edges%262144==0)flush();
 };
 uint32_t random=0x15416581;
 auto rng=[&](){random^=random<<13;random^=random>>17;random^=random<<5;return random;};
 step(false,false,false,0,0,true);step(true,false,false,0,0);
 for(unsigned cycle=0;cycle<300000;++cycle){
  unsigned r=rng(),v=(r>>8)%3,kind=(r>>16)%4;
  unsigned addr=kind==3?24:v*7+4+kind;
  unsigned data=kind==0?64|((r>>24)&1):(r>>24);
  bool we=cycle<100 || (r&7)==0,cs=(r&31)!=31;
  for(unsigned phase=0;phase<52;++phase)
   step(phase==0,cs,we&&phase==26,phase==26?addr:phase==43?28:0,data);
 }
 if(edges%262144)flush();
#ifdef C64_SID_ORACLE
 std::puts("};");
#else
 if(chunk!=sizeof(c64_sid_hashes)/sizeof(c64_sid_hashes[0]))return 1;
 std::printf("SID reSID replay PASS: %llu SYS edges, %llu writes, %llu ENV3 reads, three envelopes and signed linear-volume PCM\n",(unsigned long long)edges,(unsigned long long)writes,(unsigned long long)reads);
#endif
}
