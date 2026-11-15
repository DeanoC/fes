/* SPDX-License-Identifier: BSD-3-Clause */
/* Charge pure descriptor-generation calls, separately from I/O/guard costs.
 * Injected costs are counterexamples, not measured hardware attribution. */
#define main warm_fixture_main
#include "warm_probe_test.c"
#undef main
#include <stdlib.h>
static unsigned measuring,cost,calls,words_seen,largest;
int fixture_nano_sdm_input_words(const struct nano_sdm_input_binding *b,uint32_t off,uint64_t *out,unsigned words)
{
 if(measuring){
  assert(words && words<=32);calls++;words_seen+=words;
  if(words>largest)largest=words;
  m.now+=cost;
 }
 return nano_sdm_input_words(b,off,out,words);
}
int main(int argc,char **argv)
{
 assert(argc==2);cost=(unsigned)strtoul(argv[1],0,10);
 first();struct nano_retire *previous=&m.retirees[m.slot];
 struct nano_retire saved=*previous;struct nano_cmdq old=*previous->prepared_engine;
 m.released_generation=m.input.generation;m.target_generation=m.input.generation+6;
 m.released=1;m.slot=1-m.slot;m.phase=1;m.input.generation=m.target_generation;
 m.input.bytes=1654784;m.input.digest384[0]++;
 struct nano_probe_binding binding={17,7,m.input.generation};
 struct nano_probe_setup *p=&m.probes[m.slot];struct nano_probe_receipt receipt={0};
 assert(!nano_probe_init_rearmed_input(p,binding,io(),&m.input,source,previous,released));
 unsigned before=m.writes;measuring=1;
 int error=nano_probe_prepare(p,&receipt);measuring=0;
 assert(!memcmp(previous,&saved,sizeof(saved)) && !memcmp(previous->prepared_engine,&old,sizeof(old)));
 if(!error){
  /* Independently compare every table word, including untouched gaps/edges. */
  for(uint32_t off=0;off<0xc000;off+=8){
   if(!nano_sdm_input_table_range(off,8))continue;
   uint64_t expected=0;assert(!nano_sdm_input_words(&m.input,off,&expected,1));
   assert(m.arena[off/8]==expected);
  }
  assert(p->prepared && !p->failed && !nano_probe_arm(p,&receipt));
  assert(p->armed && receipt.DMA_access_granted && !receipt.interconnect_drained);
 }else{
  assert(error==-110 && p->failed && !p->prepared && !p->armed && m.writes==before);
  unsigned reads=m.reads,writes=m.writes,stores=m.stores,generated=calls;
  assert(nano_probe_prepare(p,&receipt) && nano_probe_arm(p,&receipt));
  assert(m.reads==reads && m.writes==writes && m.stores==stores && calls==generated);
 }
 printf("{\"status\":\"passed\",\"cost_us\":%u,\"prepared\":%u,\"error\":%d,"
  "\"calls\":%u,\"words\":%u,\"largest\":%u,\"elapsed_us\":%llu,"
  "\"register_writes\":%u,\"previous_unchanged\":true,\"hardware_actions\":false}\n",
  cost,p->prepared,error,calls,words_seen,largest,(unsigned long long)(p->last-p->start),m.writes-before);
}
