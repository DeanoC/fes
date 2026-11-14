/* SPDX-License-Identifier: BSD-3-Clause */
#define main snapshot_fixture_main
#include "command_snapshot_test.c"
#undef main
static unsigned point,mutation,injected,fallback;
static uint64_t elapsed;
static void inject(struct model *m)
{
 if(injected)return;
 injected=1;m->now=m->queue.start+elapsed-1;
 if(mutation==1)m->owned=0;
 if(mutation==2)m->queue.io.barrier=0;
 if(mutation==3)m->queue.binding.owner++;
 if(mutation==4)m->now=0;
 if(mutation==5)m->queue.validation_policy^=1; /* observational only */
}
static int clock_observed(void *p,uint64_t *out)
{
 struct model *m=p;
 if(point==CMDQ_FINAL_GUARD && m->queue.stage==point)inject(m);
 return clock_us(p,out);
}
static int read_observed(void *p,uint32_t offset,uint32_t *out)
{
 struct model *m=p;int error=read32(p,offset,out);
 if(fallback && m->queue.stage==point && offset==0x9c)inject(m);
 return error;
}
static int snapshot_observed(void *p,uint32_t pointer)
{
 struct model *m=p;int error=snapshot(p,pointer);
 if(m->queue.stage==point)inject(m);
 return error;
}
static int progress_observed(void *p,uint32_t target,uint32_t *consumer)
{
 struct model *m=p;assert(target==12);*consumer=m->consumer;
 if(m->queue.stage==point)inject(m);
 return 0;
}
int main(void)
{
 unsigned cases=0;const unsigned points[]={CMDQ_PROGRESS,CMDQ_FINAL_FIRST,CMDQ_FINAL_SECOND,CMDQ_FINAL_GUARD};
 const uint64_t times[]={80000,100001,105704,900000,1000001};
 for(unsigned policy=0;policy<=2;policy++)for(fallback=0;fallback<=1;fallback++)
 for(unsigned n=0;n<4;n++)for(unsigned t=0;t<sizeof(times)/sizeof(times[0]);t++)for(mutation=0;mutation<=5;mutation++){
  point=points[n];elapsed=times[t];injected=0;
  struct model m={.owned=1,.producer=6,.consumer=6};
  struct nano_cmdq_binding b={17,7,4,0x94008000,6};
  struct nano_cmdq_io io={&m,owns,clock_observed,read_observed,write32,store,barrier,
      fallback ? 0:snapshot_observed,fallback ? 0:progress_observed};
  assert(!nano_cmdq_init(&m.queue,b,io));struct nano_cmdq_receipt r={0};
  int error=nano_cmdq_submit(&m.queue,&r,(enum nano_cmdq_validation_policy)policy);
  unsigned bound=policy ? NANO_CMDQ_FINAL_BOUND_US:NANO_CMDQ_BOUND_US;
  unsigned should_pass=(mutation==0 || mutation==5) && elapsed+100<bound;
  if(point==CMDQ_PROGRESS && elapsed>NANO_CMDQ_BOUND_US)should_pass=0;
  assert(injected && m.stores==6 && m.writes==1);
  if(should_pass){
   assert(!error && r.commands_completed && m.queue.stage==CMDQ_COMPLETE);
   assert(m.queue.completion_seen && m.queue.completion_us-m.queue.start<=NANO_CMDQ_BOUND_US);
   assert(fallback ? m.snapshots==0:m.snapshots==5);
  }else{
   assert(error && m.queue.failed && !r.commands_completed);
   if(point==CMDQ_PROGRESS && elapsed>NANO_CMDQ_BOUND_US)assert(!m.queue.completion_seen);
   if(point!=CMDQ_PROGRESS)assert(m.queue.completion_seen);
  }
  assert(!r.interconnect_drained && !r.DMA_access_granted);
  unsigned stores=m.stores,writes=m.writes,reads=m.reads;
  assert(nano_cmdq_submit(&m.queue,&r,(enum nano_cmdq_validation_policy)policy) && m.stores==stores && m.writes==writes && m.reads==reads);
  cases++;
 }
 /* Exact completion boundary: no target first observed after 100 ms can
  * enter final validation, even with completion-looking data. */
 for(fallback=0;fallback<=1;fallback++)for(elapsed=99999;elapsed<=100001;elapsed++){
  point=CMDQ_PROGRESS;mutation=injected=0;
  struct model m={.owned=1,.producer=6,.consumer=6};struct nano_cmdq_binding b={17,7,4,0x94008000,6};
  struct nano_cmdq_io io={&m,owns,clock_observed,read_observed,write32,store,barrier,fallback ? 0:snapshot_observed,fallback ? 0:progress_observed};
  assert(!nano_cmdq_init(&m.queue,b,io));struct nano_cmdq_receipt r={0};
  int error=nano_cmdq_submit(&m.queue,&r,CMDQ_RETAINED_FINAL_VALIDATION);
  assert((error==0)==(elapsed<=100000) && m.queue.completion_seen==(elapsed<=100000));cases++;
 }
 for(int policy=-1;policy<=3;policy+=4){
  struct model m={.owned=1,.producer=6,.consumer=6};struct nano_cmdq_binding b={17,7,4,0x94008000,6};
  struct nano_cmdq_io io={&m,owns,clock_us,read32,write32,store,barrier,snapshot,0};
  assert(!nano_cmdq_init(&m.queue,b,io));struct nano_cmdq_receipt r={0};
  assert(nano_cmdq_submit(&m.queue,&r,(enum nano_cmdq_validation_policy)policy) && m.queue.first_error==-22);
  assert(!m.now && !m.stores && !m.writes && !m.reads && !r.commands_completed);cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",cases);
}
