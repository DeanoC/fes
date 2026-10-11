// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vst_ym_mixer.h"
#include "verilated.h"
#include "st_ym_mix_reference.hpp"
#include <cstdlib>
#include <iostream>
struct Test {
 Vst_ym_mixer d; unsigned checks=0,maximum=0;
 void require(bool ok,const char *s){++checks;if(!ok){std::cerr<<"YM mixer: "<<s<<"\n";std::exit(1);}}
 void tick(){d.clk=0;d.eval();d.clk=1;d.eval();}
 void reset(){d.reset=1;d.req=0;tick();require(!d.ready&&!d.busy&&d.pcm==0,"reset clears output and job");d.reset=0;tick();}
 void request(unsigned key){
  require(!d.busy,"request has idle owner");d.levels=key;d.req=1;tick();d.req=0;
  unsigned delay=0;
  while(!d.ready){require(++delay<=48,"bounded search latency");d.levels=key^32767;tick();}
  if(delay>maximum)maximum=delay;
  require(d.result_levels==key,"busy input changes cannot replace latched tuple");
  require(d.pcm==st_ym_mix_reference::sample(key),"original C++ nonlinear oracle value");
  require(!d.busy,"completion returns idle");tick();require(!d.ready,"one completion pulse");
 }
 void run(){d.clk=0;d.levels=0;reset();
  for(unsigned key=0;key<32768;++key)request(key);
  for(unsigned phase:{1u,4u,8u,12u,25u,35u}){
   reset();d.levels=12345;d.req=1;tick();d.req=0;for(unsigned i=0;i<phase;++i)tick();reset();
   for(unsigned i=0;i<50;++i){tick();require(!d.ready&&!d.busy,"reset cancels an in-flight tuple");}
   request(32767);
  }
  std::cout<<"YM nonlinear mixer: 32768 ordered triples, "<<checks<<" checks, maximum "<<maximum<<" cycles; original oracle, pulse ownership and reset passed\n";d.final();
 }
};
int main(int argc,char **argv){Verilated::commandArgs(argc,argv);Test t;t.run();}
