// SPDX-License-Identifier: GPL-2.0-or-later
// reSID source-oracle replay; see ../rtl/c64_sid.COPYING and the core README.
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#ifdef C64_OSC_ORACLE
#include "wave.h"
#else
#include "Vc64_sid_sim_top.h"
#include "verilated.h"
#include "sid_oscillator_bus_vectors.h"
#endif
int main(int argc,char**argv){
#ifdef C64_OSC_ORACLE
 reSID::WaveformGenerator ref[3];for(auto&r:ref)r.set_chip_model(reSID::MOS6581);
 std::puts("// SPDX-License-Identifier: MIT\n// Unmodified reSID MOS6581 OSC3 under three-voice bus stimulus.\n#pragma once\n#include <cstdint>\nstatic constexpr uint64_t expected_bus[]={");
#else
 Verilated::commandArgs(argc,argv);Vc64_sid_sim_top rtl;
#endif
 uint64_t hash=14695981039346656037ull,edges=0;unsigned chunks=0,reads=0,writes=0;
 auto flush=[&](){
#ifdef C64_OSC_ORACLE
 std::printf("0x%016llxull,\n",(unsigned long long)hash);
#else
 if(chunks>=sizeof(expected_bus)/sizeof(expected_bus[0])||hash!=expected_bus[chunks]){std::fprintf(stderr,"OSC3 bus mismatch SYS=%llu chunk=%u\n",(unsigned long long)edges,chunks);std::exit(1);}
#endif
 ++chunks;hash=14695981039346656037ull;
 };
 auto step=[&](bool phi,bool cs,bool we,unsigned addr,unsigned data,bool reset=false){unsigned dout=0;
#ifdef C64_OSC_ORACLE
 if(reset){for(auto&r:ref)r.reset();}
 else{
  if(phi){for(auto&r:ref)r.clock();for(auto&r:ref)r.set_waveform_output();}
  if(cs&&we && addr<21){unsigned voice=addr/7;switch(addr%7){case 0:ref[voice].writeFREQ_LO(data);break;case 1:ref[voice].writeFREQ_HI(data);break;case 2:ref[voice].writePW_LO(data);break;case 3:ref[voice].writePW_HI(data);break;case 4:ref[voice].writeCONTROL_REG(data);break;}}
 }
 dout=addr==27?ref[2].readOSC():0;
#else
 rtl.clk=0;rtl.reset=reset;rtl.phi=phi;rtl.cs=cs;rtl.we=we;rtl.addr=addr;rtl.din=data;rtl.eval();rtl.clk=1;rtl.eval();
#ifdef C64_SEQUENCED_VOICES
 // The three voice stage edges belong to the Phi step; OSC3 is read after them.
 for(unsigned extra=0;extra<((phi&&!reset)?7u:(cs&&we&&!reset)?2u:0u);++extra){rtl.clk=0;rtl.reset=0;rtl.phi=0;rtl.cs=0;rtl.we=0;rtl.eval();rtl.clk=1;rtl.eval();}
#endif
 dout=rtl.dout;
#endif
 hash^=dout;hash*=1099511628211ull;++edges;if(addr==27)++reads;if(cs&&we&&!reset)++writes;if(edges%8192==0)flush();
 };
 step(false,false,false,0,0,true);
 for(unsigned voice=0;voice<3;++voice){unsigned f=0x1001+voice*0x3211;step(false,true,true,voice*7,f&255);step(false,true,true,voice*7+1,f>>8);step(false,true,true,voice*7+4,0x81);}
 uint32_t fraction=0,rng=0x4516581;unsigned since=0;
 for(unsigned sys=0;sys<1200000;++sys){fraction+=985248;bool phi=fraction>=52224000;if(phi){fraction-=52224000;since=0;}else ++since;rng^=rng<<13;rng^=rng>>17;rng^=rng<<5;unsigned a=27,d=rng>>24;bool we=since==27&&(rng&3)==0,cs=(rng&15)!=0;
  if(we){a=(rng>>16)%32;if(a==28)a=29;if(a<21&&a%7==4){const unsigned waves[]={0,16,32,64,128};d=waves[(rng>>8)%5]|(d&9);}}
  step(phi,cs,we,a,d,sys==400003 || sys==700007);
 }
 if(edges%8192)flush();
#ifdef C64_OSC_ORACLE
 std::puts("};");
#else
 if(chunks!=sizeof(expected_bus)/sizeof(expected_bus[0]))return 1;
 std::printf("SID OSC3 bus PASS: %llu SYS edges, %u reads, %u accepted writes across three voices\n",(unsigned long long)edges,reads,writes);
#endif
}
