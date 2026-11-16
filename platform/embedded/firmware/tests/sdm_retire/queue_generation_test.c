/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual bounded queue engine over injected MMIO; no source-reuse authority. */
#include <assert.h>
#include <stdio.h>
#include <string.h>
#include "command_queue.h"
struct model {
 struct nano_cmdq q;
 struct nano_cmdq_binding expected;
 uint32_t producer,consumer;
 uint64_t time;
 unsigned stores,publishes,mode,mutate,field,mutations,barriers;
};
static void corrupt(struct model *m,unsigned point)
{
 if(m->mutate!=point || m->mutations)return;
 m->mutations++;
 switch(m->field){
 case 0:m->q.binding.owner++;break;
 case 1:m->q.binding.epoch++;break;
 case 2:m->q.binding.generation++;break;
 case 3:m->q.binding.queue+=128;break;
 case 4:m->q.binding.baseline=(m->q.binding.baseline+1)&15U;break;
 default:assert(0);
 }
}
static int same(const struct nano_cmdq_binding *a,const struct nano_cmdq_binding *b)
{return a->owner==b->owner && a->epoch==b->epoch && a->generation==b->generation && a->queue==b->queue && a->baseline==b->baseline;}
static int owns(void *p,const struct nano_cmdq_binding *b)
{
 struct model *m=p;if(!same(b,&m->expected))return 0;
 /* The last owned callback is the regression: there is no subsequent
  * ownership call to repair a missing post-callback binding check. */
 if(m->q.stage==CMDQ_FINAL_GUARD)corrupt(m,2);
 return 1;
}
static int clock_us(void *p,uint64_t *v)
{struct model *m=p;*v=++m->time;if(m->q.stage==CMDQ_FINAL_GUARD)corrupt(m,1);return 0;}
static int read32(void *p,uint32_t off,uint32_t *v)
{
 struct model *m=p;
 switch(off){
 case 0x20:case 0x24:*v=8;break;
 case 0x60:case 0x64:*v=0;break;
 case 0x90:*v=(uint32_t)m->expected.queue|3U;break;
 case 0x94:*v=0;break;
 case 0x98:*v=m->producer;break;
 case 0x9c:
  if(m->publishes && m->consumer!=m->producer)m->consumer=(m->consumer+1)&15U;
  *v=m->consumer;break;
 default:assert(0);
 }
 if(m->q.stage==CMDQ_FINAL_SECOND)corrupt(m,3);
 return 0;
}
static int publish(void *p,uint32_t off,uint32_t v)
{struct model *m=p;assert(off==0x98 && v==((m->expected.baseline+6)&15U));m->producer=v;m->publishes++;corrupt(m,4);return 0;}
static int prepare_publish_sample(void *p,uint32_t target,uint64_t *issued)
{struct model *m=p;uint64_t now=0;if(clock_us(p,&now) || !owns(p,&m->q.binding) || now<m->q.start || now-m->q.start>NANO_CMDQ_FINAL_BOUND_US)return -110;*issued=now;return publish(p,0x98,target);}
static int store(void *p,unsigned slot,const uint8_t bytes[16])
{struct model *m=p;uint8_t expected[16];assert(m->stores<6 && !nano_cmdq_encode(m->stores,expected));assert(slot==((m->expected.baseline+m->stores)&7U) && !memcmp(bytes,expected,16));m->stores++;corrupt(m,5);return 0;}
static void barrier(void *p)
{struct model *m=p;m->barriers++;if(m->q.stage==CMDQ_FINAL_GUARD || m->q.stage==CMDQ_PREPUBLICATION)corrupt(m,8);}
static int snapshot(void *p,uint32_t pointer)
{struct model *m=p;assert(m->producer==pointer && m->consumer==pointer);if(m->q.stage==CMDQ_FINAL_SECOND)corrupt(m,6);return 0;}
static int progress(void *p,uint32_t target,uint32_t *consumer)
{struct model *m=p;assert(m->producer==target);m->consumer=(m->consumer+1)&15U;*consumer=m->consumer;corrupt(m,7);return 0;}
static void init(struct model *m,unsigned mode,unsigned baseline,uint64_t generation)
{
 *m=(struct model){.producer=baseline,.consumer=baseline,.mode=mode,.expected={17,7,generation,0x94008000,baseline}};
 struct nano_cmdq_io io={m,owns,clock_us,read32,publish,store,barrier,mode ? snapshot:0,mode==2 ? progress:0,prepare_publish_sample};
 assert(!nano_cmdq_init(&m->q,m->expected,io));
}
int main(void)
{
 unsigned cases=0,wraps=0;
 const unsigned policies[]={CMDQ_FULL_OPERATION_BOUND,CMDQ_PREPARATION_PUBLICATION_BOUND};
 for(unsigned mode=0;mode<3;mode++)for(unsigned selected=0;selected<2;selected++){
  unsigned policy=policies[selected];
  unsigned baseline=0;
  for(unsigned generation=1;generation<=64;generation++){
   struct model m;init(&m,mode,baseline,generation);struct nano_cmdq_receipt r={0};
   assert(!nano_cmdq_submit(&m.q,&r,(enum nano_cmdq_validation_policy)policy));
   unsigned target=(baseline+6)&15U;if(target<baseline)wraps++;
   assert(r.owner==17 && r.epoch==7 && r.generation==generation && r.baseline==baseline && r.producer==target && r.consumer==target && r.commands_completed);
   assert(m.stores==6 && m.publishes==1 && !r.interconnect_drained && !r.DMA_access_granted);
   assert(nano_cmdq_submit(&m.q,&r,(enum nano_cmdq_validation_policy)policy));
   assert(m.stores==6 && m.publishes==1);baseline=target;cases++;
  }
 }
 for(unsigned point=1;point<=8;point++)for(unsigned field=0;field<5;field++){
  /* Direct I/O, snapshot and progress paths all run the real engine. */
  unsigned mode=(point==6 ? 1:point==7 ? 2:0);struct model m;init(&m,mode,12,9);m.mutate=point;m.field=field;
  struct nano_cmdq_receipt r={0};int error=nano_cmdq_submit(&m.q,&r,CMDQ_FULL_OPERATION_BOUND);
  assert(m.mutations==1 && error && m.q.failed && !r.commands_completed);
  unsigned stores=m.stores,writes=m.publishes;
  assert(nano_cmdq_submit(&m.q,&r,CMDQ_FULL_OPERATION_BOUND) && stores==m.stores && writes==m.publishes);cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"completed_generations\":384,\"wraps\":%u,\"hardware_actions\":false}\n",cases,wraps);
}
