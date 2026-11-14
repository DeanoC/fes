/* SPDX-License-Identifier: BSD-3-Clause */
/* Real setup/arm/retirement with injected fixed batch and register costs.
 * These costs test budget behavior; they do not qualify hardware timing. */
#define main warm_fixture_main
#include "warm_probe_test.c"
#undef main
#ifndef NANO_PROBE_SETUP_WORDS
#define NANO_PROBE_SETUP_WORDS NANO_PROBE_MEMORY_WORDS
#endif
static unsigned failure,injected,words_stored,max_words,block_calls,owner_lost;
static unsigned reads_seen[0xc000/8],stores_seen[0xc000/8];
static unsigned scope_cost,register_cost;
static int cost_owned(void *c,const struct nano_probe_binding *b)
{return !owner_lost && owned(c,b);}
static int cost_read(void *c,uint32_t off,uint32_t *out)
{m.now+=register_cost;return read32(c,off,out);}
static int cost_write(void *c,uint32_t off,uint32_t value)
{m.now+=register_cost;return write32(c,off,value);}
static void batch(uint32_t off,unsigned words,unsigned writing)
{
 assert(words && words<=NANO_PROBE_SETUP_WORDS && words*8<=1024);
 block_calls++;if(words>max_words)max_words=words;
 m.now+=scope_cost+2*words;
 if(!nano_sdm_input_table_range(off,words*8))return; /* Separate cold queue reads. */
 for(unsigned n=0;n<words;n++)(writing ? stores_seen:reads_seen)[off/8+n]++;
 if(writing)words_stored+=words;
}
static int cost_store(void *c,uint32_t off,const uint64_t *v,unsigned words)
{batch(off,words,1);return store_block(c,off,v,words);}
static int cost_load(void *c,uint32_t off,uint64_t *v,unsigned words)
{
 batch(off,words,0);int error=load_block(c,off,v,words);
 if(!injected && words_stored==5120 && nano_sdm_input_table_range(off,words*8) &&
    ((failure==1 && !off) || (failure==2 && off+words*8==0xc000) || failure>=3)){
  injected=1;
  if(failure<=2)v[words-1]^=1;
  else if(failure==3)m.now+=NANO_PROBE_BOUND_US+1;
  else owner_lost=1;
 }
 return error;
}
static struct nano_probe_io cost_io(void)
{struct nano_probe_io v=io();v.owns=cost_owned;v.read32=cost_read;v.write32=cost_write;v.store_block=cost_store;v.load_block=cost_load;return v;}
static void initialize(unsigned warm)
{
 failure=injected=words_stored=max_words=block_calls=owner_lost=0;
 memset(reads_seen,0,sizeof(reads_seen));memset(stores_seen,0,sizeof(stores_seen));
 if(warm){
  first();struct nano_retire *old=&m.retirees[m.slot];
  m.released_generation=m.input.generation;m.target_generation=m.input.generation+6;
  m.released=1;m.slot=1-m.slot;m.phase=1;m.input.generation=m.target_generation;m.input.digest384[0]++;
  m.now+=UINT64_C(90000000000); /* The new lifetime cannot reuse the old clock. */
  struct nano_probe_binding b={17,7,m.input.generation};
  assert(!nano_probe_init_rearmed_input(&m.probes[m.slot],b,cost_io(),&m.input,source,old,released));
 }else{
  memset(&m,0,sizeof(m));m.reg[0]=0x080f7e3f;m.reg[1]=0x0e739d18;m.reg[0x14/4]=0x400075;m.reg[0x2c/4]=4;
  m.input=(struct nano_sdm_input_binding){.owner=17,.epoch=7,.generation=4,.bytes=1708032,.chunk=1048512,
    .idr0=0x080f7e3f,.idr1=0x0e739d18,.idr5=0x400075};memset(m.input.digest384,1,48);
  struct nano_probe_binding b={17,7,4};assert(!nano_probe_init_input(&m.probes[0],b,cost_io(),&m.input,source));
 }
}
int main(void)
{
 unsigned cases=0;
 for(unsigned warm=0;warm<=1;warm++)for(unsigned scenario=0;scenario<6;scenario++){
  initialize(warm);failure=scenario<2 ? 0:scenario-1;
  register_cost=scenario ? 900:800;scope_cost=warm ? 3600:2700;
  struct nano_probe_setup *p=&m.probes[m.slot];struct nano_probe_receipt receipt={0};
  unsigned before=m.writes;int error=nano_probe_prepare(p,&receipt);
  if(!failure){
   assert(!error && p->prepared && !p->failed && !nano_probe_arm(p,&receipt));
   assert(p->armed && receipt.DMA_access_granted && !receipt.interconnect_drained && p->last-p->start<=NANO_PROBE_BOUND_US);
   for(uint32_t off=0;off<0xc000;off+=8){
    unsigned table=nano_sdm_input_table_range(off,8);
    assert(stores_seen[off/8]==table && reads_seen[off/8]==table*(warm ? 2U:1U));
   }
   assert(words_stored==5120 && max_words==NANO_PROBE_SETUP_WORDS);
   assert(block_calls==(warm ? 3:2)*(5120/NANO_PROBE_SETUP_WORDS)+(warm ? 0:2));
   retire_current();
  }else{
   assert(error && injected && p->failed && !p->prepared && !p->armed && !receipt.DMA_access_granted);
   assert(m.writes==before); /* Every readback must pass before any new MMIO write. */
   unsigned reads=m.reads,writes=m.writes,stores=m.stores,calls=block_calls;
   assert(nano_probe_prepare(p,&receipt) && nano_probe_arm(p,&receipt));
   assert(m.reads==reads && m.writes==writes && m.stores==stores && block_calls==calls);
  }
  cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"max_words\":%u,\"hardware_actions\":false}\n",cases,NANO_PROBE_SETUP_WORDS);
}
