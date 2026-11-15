/* SPDX-License-Identifier: BSD-3-Clause */
#define main snapshot_fixture_main
#include "command_snapshot_test.c"
#undef main

static uint64_t staging,prewrite,completion,final_delay,issued;
static unsigned fault,publication_calls,first_snapshot,delay_final,fallback;

static int snapshot_timed(void *p,uint32_t pointer)
{
 struct model *m=p;
 int e=snapshot(p,pointer);
 if(!first_snapshot++){m->now+=staging;}
 if(m->queue.stage==CMDQ_FINAL_FIRST && !delay_final++){m->now+=final_delay;}
 if(fault==8)m->queue.validation_policy=CMDQ_FULL_OPERATION_BOUND;
 return e;
}
static int publish_timed(void *p,uint32_t target,uint64_t *sample)
{
 struct model *m=p;publication_calls++;
 m->now+=prewrite;
 /* Model the supplier's last fresh prewrite guard and its local construction
  * admission before any producer write. No failure can replay this call. */
 uint64_t now=0;assert(!clock_us(p,&now));
 if(!owns(p,&m->queue.binding) || now<m->queue.start ||
    now-m->queue.start>(m->queue.validation_policy==CMDQ_PREPARATION_PUBLICATION_BOUND ? NANO_CMDQ_FINAL_BOUND_US:NANO_CMDQ_RETIRE_STAGE_US))return -110;
 *sample=issued=now;
 int e=write32(p,0x98,target);
 switch(fault){
 case 1:*sample=m->queue.start-1;break;
 case 2:*sample=UINT64_MAX;break;
 case 3:m->owned=0;break;
 case 4:m->queue.io.publish_timed=0;break;
 case 5:m->queue.binding.generation++;break;
 case 6:return -7;
 case 7:m->now=0;break;
 default:break;
 }
 return e;
}
static int progress_timed(void *p,uint32_t target,uint32_t *consumer)
{
 struct model *m=p;assert(target==12);
 int e=snapshot(p,target);if(e)return e;
 *consumer=m->consumer;
 if(completion)m->now=issued+completion-1; /* next postguard sample */
 return 0;
}
static int read_timed(void *p,uint32_t offset,uint32_t *value)
{
 struct model *m=p;int e=read32(p,offset,value);
 if(m->queue.stage==CMDQ_INITIAL_FIRST && !first_snapshot++)m->now+=staging;
 if(m->queue.stage==CMDQ_FINAL_FIRST && !delay_final++)m->now+=final_delay;
 if(m->queue.stage==CMDQ_PROGRESS && offset==0x9c && completion)m->now=issued+completion-1;
 if(fault==8)m->queue.validation_policy=CMDQ_FULL_OPERATION_BOUND;
 return e;
}
static unsigned cases;
static void run_case(unsigned policy,uint64_t stage,uint64_t before_write,
                     uint64_t complete,uint64_t final,unsigned broken,unsigned pass)
{
 const uint64_t stage_bound=policy==CMDQ_PREPARATION_PUBLICATION_BOUND ? NANO_CMDQ_FINAL_BOUND_US:NANO_CMDQ_RETIRE_STAGE_US;
 const uint64_t complete_bound=policy==CMDQ_PREPARATION_PUBLICATION_BOUND ? NANO_CMDQ_BOUND_US:NANO_CMDQ_RETIRE_COMPLETION_US;
 struct model m={.owned=1,.producer=6,.consumer=6};
 staging=stage;prewrite=before_write;completion=complete;final_delay=final;
 fault=broken;publication_calls=first_snapshot=delay_final=0;issued=0;
 struct nano_cmdq_binding b={17,7,4,0x94008000,6};
 struct nano_cmdq_io io={&m,owns,clock_us,fallback ? read_timed:read32,write32,store,barrier,
                       fallback ? 0:snapshot_timed,fallback ? 0:progress_timed,publish_timed};
 assert(!nano_cmdq_init(&m.queue,b,io));struct nano_cmdq_receipt r={0};
 int e=nano_cmdq_submit(&m.queue,&r,(enum nano_cmdq_validation_policy)policy);
 if(pass){
  assert(!e && !m.queue.failed && r.commands_completed && m.writes==1 && m.stores==6);
  assert(m.queue.publication_seen && m.queue.publication_us==issued && m.queue.completion_seen);
  assert(m.queue.completion_us-issued<=complete_bound);
  assert(m.queue.last-m.queue.start<=NANO_CMDQ_RETIRE_FINAL_US);
 }else{
  assert(e && m.queue.failed && !r.commands_completed);
  if(stage>stage_bound || (policy!=CMDQ_RETIREMENT_PUBLICATION_BOUND && policy!=CMDQ_PREPARATION_PUBLICATION_BOUND))
   assert(!m.writes && !publication_calls);
  if(before_write>stage_bound)assert(publication_calls==1 && !m.writes);
 }
 assert(!r.interconnect_drained && !r.DMA_access_granted);
 unsigned writes=m.writes,stores=m.stores,reads=m.reads,publishes=publication_calls;
 assert(nano_cmdq_submit(&m.queue,&r,(enum nano_cmdq_validation_policy)policy));
 assert(m.writes==writes && m.stores==stores && m.reads==reads && publication_calls==publishes);
 cases++;
}
#ifndef FES_TEST_PUBLICATION_MAIN
#define FES_TEST_PUBLICATION_MAIN main
#endif
int FES_TEST_PUBLICATION_MAIN(void)
{
 for(fallback=0;fallback<=1;fallback++){
 run_case(3,0,0,0,0,0,1);
 run_case(3,300000,0,900000,2000000,0,1);
 run_case(3,1900000,0,1000000,500000,0,1);
 for(uint64_t elapsed=999999;elapsed<=1000001;elapsed++)
  run_case(3,300000,0,elapsed,0,0,elapsed<=1000000);
 run_case(3,NANO_CMDQ_RETIRE_STAGE_US+1,0,0,0,0,0);
 run_case(3,0,NANO_CMDQ_RETIRE_STAGE_US+1,0,0,0,0);
 run_case(3,0,0,0,NANO_CMDQ_RETIRE_FINAL_US+1,0,0);
 for(unsigned broken=1;broken<=8;broken++)run_case(3,300000,0,900000,0,broken,broken==8);
 /* The full-operation issuer keeps its original origin; old policy 2 is rejected. */
 run_case(0,300000,0,0,0,0,0);
 run_case(2,300000,0,0,0,0,0);
 /* Removed policy 1 and a missing trusted timed publisher fail before I/O. */
 for(unsigned n=0;n<2;n++){
  struct model m={.owned=1,.producer=6,.consumer=6};
  struct nano_cmdq_binding b={17,7,4,0x94008000,6};
  struct nano_cmdq_io io={&m,owns,clock_us,read32,write32,store,barrier,snapshot,0,0};
  assert(!nano_cmdq_init(&m.queue,b,io));struct nano_cmdq_receipt r={0};
  assert(nano_cmdq_submit(&m.queue,&r,n ? CMDQ_RETIREMENT_PUBLICATION_BOUND:1));
  assert(!m.now && !m.stores && !m.writes && !m.reads && !r.commands_completed);cases++;
 }
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",cases);
 return 0;
}
