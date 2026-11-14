/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <stdio.h>
#include <string.h>
#include "probe_setup.h"
struct model {
 struct nano_probe_setup probes[2];struct nano_retire retirees[2];
 struct nano_sdm_input_binding input;
 uint64_t arena[8192],now,released_generation,target_generation;
 uint32_t reg[48];unsigned slot,phase,released,writes,clears,stores,reads,fault,mutated;
};
static struct model m;
static unsigned index_of(uint32_t off)
{if(off==0x100a8)return 44;if(off==0x100ac)return 45;assert(!(off&3) && off<0xc0);return off/4;}
static int same_probe(const struct nano_probe_binding *b)
{return b->owner==m.input.owner && b->epoch==m.input.epoch && b->generation==m.input.generation;}
static int owned(void *c,const struct nano_probe_binding *b)
{return c==&m && same_probe(b);}
static int source(void *c,const struct nano_sdm_input_binding *b)
{return c==&m && nano_sdm_input_same(b,&m.input);}
static void mutate(void)
{
 if(!m.fault || m.mutated)return;m.mutated=1;
 switch(m.fault){
 case 1:m.probes[m.slot].io.read32=0;break;
 case 2:m.probes[m.slot].input.digest384[0]^=1;break;
 case 3:m.retirees[1-m.slot].continued=0;break;
 case 4:m.released=0;break;
 default:assert(0);
 }
}
static int timer(void *c,uint64_t *out){assert(c==&m);*out=++m.now;return 0;}
static int read32(void *c,uint32_t off,uint32_t *out)
{
 assert(c==&m);m.reads++;
 if(off==0x44)m.reg[index_of(off)]&=~0x80000000U;
 if(off==0x9c && m.reg[0x98/4]!=m.reg[0x9c/4]){
  assert(m.reg[0x20/4]&8U);m.reg[0x9c/4]=(m.reg[0x9c/4]+1)&15U;
 }
 *out=m.reg[index_of(off)];return 0;
}
static int write32(void *c,uint32_t off,uint32_t value)
{
 assert(c==&m);m.writes++;
 if(m.phase==1)assert(off==0x20 || off==0x98);
 assert(off!=0x9c); /* The consumer/wrap position is never reset. */
 m.reg[index_of(off)]=value;if(off==0x20)m.reg[0x24/4]=value;
 return 0;
}
static int clear(void *c,uint32_t off,uint32_t bytes)
{assert(c==&m && !m.phase && off+bytes<=0x10000);m.clears++;memset((uint8_t *)m.arena+off,0,bytes);return 0;}
static int store64(void *c,uint32_t off,uint64_t value)
{assert(c==&m && off>=0x8000 && off<0x8080 && !(off&7));m.stores++;m.arena[off/8]=value;return 0;}
static int load64(void *c,uint32_t off,uint64_t *value)
{assert(c==&m && off<0x10000 && !(off&7));*value=m.arena[off/8];return 0;}
static int store_block(void *c,uint32_t off,const uint64_t *values,unsigned words)
{
 assert(c==&m && !m.reg[0x20/4] && nano_sdm_input_table_range(off,words*8));
 m.stores+=words;memcpy(m.arena+off/8,values,words*8);mutate();return 0;
}
static int load_block(void *c,uint32_t off,uint64_t *values,unsigned words)
{assert(c==&m && off+words*8<=0x10000);memcpy(values,m.arena+off/8,words*8);return 0;}
static void barrier(void *c){assert(c==&m);}
static struct nano_probe_io io(void)
{return (struct nano_probe_io){&m,owned,timer,read32,write32,clear,store64,load64,store_block,load_block,barrier};}
static int released(void *c,const struct nano_retire *old,const struct nano_sdm_input_binding *input)
{
 return c==&m && old==&m.retirees[1-m.slot] && m.released==1 &&
  old->binding.generation==m.released_generation && input->generation==m.target_generation &&
  nano_sdm_input_same(input,&m.input);
}
static int retire_owned(void *c,const struct nano_sdm_input_binding *b)
{
 const struct nano_probe_setup *p=&m.probes[m.slot];
 return c==&m && m.phase==2 && p->prepared && p->armed && !p->failed && nano_sdm_input_same(b,&m.input);
}
static int retire_source(void *c,const struct nano_sdm_input_binding *b)
{
 if(!retire_owned(c,b))return 0;
 for(uint32_t off=0;off<0xc000;off+=8){
  if(!nano_sdm_input_table_range(off,8))continue;uint64_t value=0;
  if(nano_sdm_input_words(b,off,&value,1) || value!=m.arena[off/8])return 0;
 }
 return 1;
}
static int retire_store(void *c,unsigned slot,const uint8_t bytes[16])
{assert(c==&m && slot<8);m.stores+=2;memcpy((uint8_t *)m.arena+0x8000+slot*16,bytes,16);return 0;}
static void retire_current(void)
{
 struct nano_probe_setup *p=&m.probes[m.slot];struct nano_retire *r=&m.retirees[m.slot];
 struct nano_retire_io out={&m,retire_owned,retire_source,timer,read32,write32,retire_store,barrier,&p->command,p->abort};
 struct nano_retire_receipt receipt={0};m.phase=2;
 assert(!nano_source_retire(r,m.input,out,&receipt));
 assert(!nano_source_continue(r,m.input,out));
 assert(receipt.translation_retired && receipt.memory_retained && !receipt.source_reuse_authorized && !receipt.interconnect_drained);
 assert(receipt.producer==((p->queue_target+6)&15U) && m.reg[0x44/4]==0x100000 && !m.reg[0x20/4]);
}
static void first(void)
{
 memset(&m,0,sizeof(m));m.reg[0]=0x080f7e3f;m.reg[1]=0x0e739d18;m.reg[0x14/4]=0x400075;m.reg[0x2c/4]=4;
 m.input=(struct nano_sdm_input_binding){.owner=17,.epoch=7,.generation=4,.bytes=1708032,.chunk=1048512,
  .idr0=0x080f7e3f,.idr1=0x0e739d18,.idr5=0x400075};memset(m.input.digest384,1,48);
 struct nano_probe_binding b={17,7,4};struct nano_probe_receipt receipt={0};
 assert(!nano_probe_init_input(&m.probes[0],b,io(),&m.input,source));
 assert(!nano_probe_prepare(&m.probes[0],&receipt) && !nano_probe_arm(&m.probes[0],&receipt));
 retire_current();
}
static int next(unsigned offset,unsigned copied)
{
 struct nano_retire *previous=&m.retirees[m.slot];m.released_generation=m.input.generation;
 m.target_generation=m.input.generation+offset;m.released=1;m.slot=1-m.slot;m.phase=1;
 assert(!m.probes[m.slot].failed && !m.retirees[m.slot].failed);
 memset(&m.probes[m.slot],0,sizeof(m.probes[m.slot]));memset(&m.retirees[m.slot],0,sizeof(m.retirees[m.slot]));
 m.input.generation=m.target_generation;m.input.bytes=m.input.bytes==1708032 ? 2101248:1708032;m.input.digest384[0]++;
 struct nano_probe_binding b={17,7,m.input.generation};struct nano_retire copy;
 if(copied){memcpy(&copy,previous,sizeof(copy));previous=&copy;}
 assert(!nano_probe_init_rearmed_input(&m.probes[m.slot],b,io(),&m.input,source,previous,released));
 struct nano_probe_receipt receipt={0};unsigned clears=m.clears;
 int error=nano_probe_prepare(&m.probes[m.slot],&receipt);
 assert(m.clears==clears && m.reg[0x44/4]==0x100000);
 if(error)return error;
 assert(!nano_probe_arm(&m.probes[m.slot],&receipt) && receipt.DMA_access_granted && !receipt.interconnect_drained);
 return 0;
}
int main(void)
{
 /* The selected implementation still supports its fault-only cold API. */
 memset(&m,0,sizeof(m));m.reg[0]=0x080f7e3f;m.reg[1]=0x0e739d18;m.reg[0x14/4]=0x400075;m.reg[0x2c/4]=4;
 m.input.owner=17;m.input.epoch=7;m.input.generation=4;
 struct nano_probe_binding cold={17,7,4};struct nano_probe_receipt cold_receipt={0};
 assert(!nano_probe_init(&m.probes[0],cold,io()));
 assert(!nano_probe_prepare(&m.probes[0],&cold_receipt) && !nano_probe_arm(&m.probes[0],&cold_receipt));
 assert(cold_receipt.translation_fault_only && !cold_receipt.DMA_access_granted);
 first();unsigned wraps=0;
 for(unsigned n=0;n<64;n++){
  unsigned before=m.reg[0x98/4];m.now+=UINT64_C(90000000000);
  assert(!next((n&1)?5:6,0));if(m.probes[m.slot].queue_target<before)wraps++;
  assert(m.probes[m.slot].warm && m.reg[0x44/4]==0x100000);
  retire_current();
 }
 for(unsigned fault=1;fault<=4;fault++){
  first();m.fault=fault;assert(next(6,0) && m.mutated && m.probes[m.slot].failed);
  unsigned writes=m.writes,stores=m.stores,reads=m.reads;
  struct nano_probe_receipt receipt={0};assert(nano_probe_prepare(&m.probes[m.slot],&receipt));
  assert(m.writes==writes && m.stores==stores && m.reads==reads);
 }
 first();unsigned stores=m.stores,writes=m.writes;assert(next(6,1));
 assert(m.stores==stores && m.writes==writes);
 puts("{\"status\":\"passed\",\"completed_generations\":64,\"hardware_actions\":false}");assert(wraps);
}
