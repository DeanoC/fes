// SPDX-License-Identifier: MIT
// Deterministic source-oracle replay. Expected hashes cover every observed tick.
#include <cstdint>
#include <cstdio>
#include <initializer_list>
#ifdef GENERATE_COLLISION_VECTORS
extern "C" {void oracle_reset();unsigned oracle_read(bool);void oracle_step(bool,unsigned,unsigned,bool,unsigned,unsigned);unsigned oracle_state(unsigned);}
#else
#include "Vc64_vic_collisions.h"
#include "verilated.h"
#include "collision_vectors.h"
#endif
static uint32_t random_state=0x45ec0781;
static uint32_t random_word(){random_state^=random_state<<13;random_state^=random_state>>17;random_state^=random_state<<5;return random_state;}
static void mix(uint64_t &hash,unsigned value){hash^=value;hash*=UINT64_C(1099511628211);}
int main(int argc,char **argv){
#ifdef GENERATE_COLLISION_VECTORS
 (void)argc;(void)argv;oracle_reset();std::puts("static constexpr uint64_t collision_vectors[]={");
#else
 Verilated::commandArgs(argc,argv);Vc64_vic_collisions t;
 t.clk=0;t.reset=1;t.group_tick=0;t.read_sample=0;t.read_bg=0;t.ack=0;t.ack_mask=0;t.other_events=0;t.enable=0;t.ss_events=0;t.bg_events=0;
 auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};tick();t.reset=0;
#endif
 uint64_t hash=UINT64_C(14695981039346656037);unsigned reads=0,acks=0;
 for(unsigned n=0;n<500000;++n){
  unsigned v=random_word(),group=(n&7)==7,read=(v&31)==0,read_bg=(v>>5)&1,ack=(v&127)==1;
  unsigned ack_mask=(v>>7)&15,events=(v&1023)==2?9:0,enable=(v>>11)&15,ss=(v&3)?((v>>15)&255):0,bg=(v&7)?((v>>23)&255):0;
#ifdef GENERATE_COLLISION_VECTORS
  if(read)mix(hash,oracle_read(read_bg));
  oracle_step(ack,ack_mask,events,group,ss,bg);
  unsigned ss_mask=oracle_state(0),bg_mask=oracle_state(1),flags=oracle_state(2),irq=(flags&enable)!=0,status=0x70|flags|(irq?128:0);
#else
  t.group_tick=group;t.read_sample=read;t.read_bg=read_bg;t.ack=ack;t.ack_mask=ack_mask;t.other_events=events;t.enable=enable;t.ss_events=ss;t.bg_events=bg;t.eval();
  if(read)mix(hash,t.read_value);tick();
  unsigned ss_mask=t.ss_mask,bg_mask=t.bg_mask,flags=t.flags,irq=t.irq,status=t.status;
#endif
  reads+=read;acks+=ack;for(unsigned value:{ss_mask,bg_mask,flags,irq,status})mix(hash,value);
  if(n%1000==999){
#ifdef GENERATE_COLLISION_VECTORS
   std::printf(" UINT64_C(0x%016llx),\n",(unsigned long long)hash);
#else
   if(hash!=collision_vectors[n/1000]){std::printf("collision oracle mismatch ticks%u..%u got%016llx expected%016llx\n",n-999,n,(unsigned long long)hash,(unsigned long long)collision_vectors[n/1000]);return 1;}
#endif
   hash=UINT64_C(14695981039346656037);
  }
 }
#ifdef GENERATE_COLLISION_VECTORS
 std::puts("};");
#else
 std::printf("C64 collision oracle PASS:500000 ticks,%u reads,%u acknowledgements; deferred clears,W1C,mask-independent flags,retrigger and status\n",reads,acks);
#endif
}
