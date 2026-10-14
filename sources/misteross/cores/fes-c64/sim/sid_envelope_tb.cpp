// SPDX-License-Identifier: MIT
// Independent source-oracle recording and offline RTL replay use the same inputs.
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#ifdef C64_SID_ORACLE
#include "envelope.h"
class EnvelopeOracle:public reSID::EnvelopeGenerator {
public: unsigned level()const{return envelope_counter;}
};
#else
#include "Vc64_sid_envelope.h"
#include "verilated.h"
#include "sid_envelope_vectors.h"
#endif
int main(int argc,char**argv){
#ifdef C64_SID_ORACLE
 EnvelopeOracle ref;
 std::puts("// SPDX-License-Identifier: MIT\n// Generated from unmodified VICE 3.10 reSID EnvelopeGenerator.\n#pragma once\n#include <cstdint>\nstatic constexpr uint64_t c64_sid_envelope_hashes[] = {");
#else
 Verilated::commandArgs(argc,argv);Vc64_sid_envelope rtl;
#endif
 uint64_t hash=14695981039346656037ull,edges=0,writes=0,resets=0,held=0;unsigned chunk=0;bool sampled=false;
 auto flush=[&](){
#ifdef C64_SID_ORACLE
  std::printf("    0x%016llxull,\n",(unsigned long long)hash);
#else
  if(chunk>=sizeof(c64_sid_envelope_hashes)/sizeof(c64_sid_envelope_hashes[0]) || hash!=c64_sid_envelope_hashes[chunk]){
   std::fprintf(stderr,"SID envelope mismatch chunk %u, edge %llu\n",chunk,(unsigned long long)edges);std::exit(1);
  }
#endif
  ++chunk;hash=14695981039346656037ull;
 };
 auto step=[&](bool phi,int addr=-1,unsigned data=0,bool reset=false){
  unsigned level,env3;
  if(!reset&&phi)sampled=true;
#ifdef C64_SID_ORACLE
  if(reset)ref.reset();
  else {
   if(phi)ref.clock();
   if(addr==0)ref.writeCONTROL_REG(data);
   if(addr==1)ref.writeATTACK_DECAY(data);
   if(addr==2)ref.writeSUSTAIN_RELEASE(data);
  }
  level=ref.level();env3=sampled?ref.readENV():0;
#else
  rtl.clk=0;rtl.reset=reset;rtl.phi=phi;rtl.we=addr>=0;rtl.addr=addr<0?0:addr;rtl.din=data;rtl.eval();
  rtl.clk=1;rtl.eval();level=rtl.level;env3=sampled?rtl.env3:0;
#endif
  hash^=level;hash*=1099511628211ull;hash^=env3;hash*=1099511628211ull;
  ++edges;if(reset)++resets;else{if(!phi)++held;if(addr>=0)++writes;}
  if(edges%262144==0)flush();
 };
 step(false,-1,0,true);step(true);
 for(unsigned rate=0;rate<16;++rate){
  step(false,0,0);step(false,1,rate*17);step(false,2,rate*17);step(false,0,1);
  for(unsigned i=0;i<600000;++i)step(true);
  step(false,0,0);for(unsigned i=0;i<600000;++i)step(true);
 }
 step(false,1,0);step(false,2,0);step(false,0,1);
 for(unsigned i=0;i<20000;++i)step(true);
 step(false,0,0);for(unsigned i=0;i<20000;++i)step(true);
 uint32_t random=0x06581c64;
 for(unsigned i=0;i<2000000;++i){
  random^=random<<13;random^=random>>17;random^=random<<5;
  int addr=random%11==0?int((random>>8)%3):-1;
  step((random&7)!=0,addr,(random>>16)&255,random%200003==0);
 }
 if(edges%262144)flush();
#ifdef C64_SID_ORACLE
 std::puts("};");
#else
 if(chunk!=sizeof(c64_sid_envelope_hashes)/sizeof(c64_sid_envelope_hashes[0]))return 1;
 std::printf("SID envelope replay PASS: %llu edges, %llu writes, %llu held clocks, %llu resets, all rates and complete fast attack/release\n",(unsigned long long)edges,(unsigned long long)writes,(unsigned long long)held,(unsigned long long)resets);
#endif
}
