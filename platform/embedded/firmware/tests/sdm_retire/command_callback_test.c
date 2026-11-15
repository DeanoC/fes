/* SPDX-License-Identifier: BSD-3-Clause */
#define main snapshot_fixture_main
#include "command_snapshot_test.c"
#undef main
static unsigned cut,field,hit,barriers;
static int poison(void *p,uint32_t target,uint32_t *consumer)
{(void)p;(void)target;(void)consumer;assert(!"substituted callback executed");return -1;}
static void mutate(struct model *m)
{
 if(hit)return;hit=1;
 switch(field){
 case 0:m->queue.io.context=0;break;
 case 1:m->queue.io.owns=0;break;
 case 2:m->queue.io.time_us=0;break;
 case 3:m->queue.io.read32=0;break;
 case 4:m->queue.io.write32=0;break;
 case 5:m->queue.io.store=0;break;
 case 6:m->queue.io.barrier=0;break;
 case 7:m->queue.io.snapshot=snapshot;break;
 case 8:m->queue.io.progress=poison;break;
 case 9:m->queue.io.publish_timed=0;break;
 default:assert(0);
 }
}
static int read_mutating(void *p,uint32_t offset,uint32_t *out)
{struct model *m=p;int e=read32(p,offset,out);if(cut==2)mutate(m);return e;}
static int store_mutating(void *p,unsigned slot,const uint8_t bytes[16])
{struct model *m=p;int e=store(p,slot,bytes);if(cut==4)mutate(m);return e;}
static int write_mutating(void *p,uint32_t offset,uint32_t value)
{struct model *m=p;int e=write32(p,offset,value);if(cut==7)mutate(m);return e;}
static int publish_mutating(void *p,uint32_t target,uint64_t *issued)
{struct model *m=p;*issued=m->queue.last;return write_mutating(p,0x98,target);}
static void barrier_mutating(void *p)
{
 struct model *m=p;barriers++;
 if((cut==1 && barriers==1) || (cut==3 && barriers==2) ||
    (cut==5 && m->queue.stage==CMDQ_STORE && m->stores==1) ||
    (cut==6 && m->queue.stage==CMDQ_PUBLICATION && !m->queue.publish_attempted) ||
    (cut==8 && m->queue.stage==CMDQ_PUBLICATION && m->queue.publish_attempted))mutate(m);
}
int main(void)
{
 unsigned cases=0;
 const unsigned policies[]={CMDQ_FULL_OPERATION_BOUND,CMDQ_PREPARATION_PUBLICATION_BOUND,CMDQ_RETIREMENT_PUBLICATION_BOUND};
 for(unsigned selected=0;selected<3;selected++)for(cut=1;cut<=8;cut++)for(field=0;field<10;field++){
  unsigned policy=policies[selected];
  struct model m={.owned=1,.producer=6,.consumer=6};hit=barriers=0;
  struct nano_cmdq_binding b={17,7,4,0x94008000,6};
  struct nano_cmdq_io io={&m,owns,clock_us,read_mutating,write_mutating,store_mutating,barrier_mutating,0,0,publish_mutating};
  assert(!nano_cmdq_init(&m.queue,b,io));struct nano_cmdq_receipt r={0};
  assert(nano_cmdq_submit(&m.queue,&r,(enum nano_cmdq_validation_policy)policy));
  assert(hit && m.queue.failed && m.queue.first_error==-13 && !r.commands_completed);
  if(cut<=3)assert(!m.stores && !m.writes);
  if(cut==1)assert(!m.reads);
  if(cut==4 || cut==5)assert(m.stores==1 && !m.writes);
  if(cut==6)assert(m.stores==6 && !m.writes && !m.queue.publish_attempted);
  if(cut>=7)assert(m.stores==6 && m.writes==1 && m.queue.publish_attempted);
  assert(!r.interconnect_drained && !r.DMA_access_granted);
  unsigned reads=m.reads,stores=m.stores,writes=m.writes,barrier_count=barriers;
  assert(nano_cmdq_submit(&m.queue,&r,(enum nano_cmdq_validation_policy)policy));
  assert(m.reads==reads && m.stores==stores && m.writes==writes && barriers==barrier_count);cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",cases);
}
