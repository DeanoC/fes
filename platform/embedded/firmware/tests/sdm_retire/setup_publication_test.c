/* SPDX-License-Identifier: BSD-3-Clause */
/* Real cold/warm setup: stage longer than 100 ms, consume within 100 ms. */
#define main warm_fixture_main
#include "warm_probe_test.c"
#undef main
static unsigned stage_injected,completion_injected,broken,owner_lost;
static uint64_t staging,completion;
static int publication_clock(void *context,uint64_t *out)
{
 struct nano_probe_setup *p=&m.probes[m.slot];struct nano_cmdq *q=&p->command;
 if(q->timed && q->stage==CMDQ_INITIAL_SECOND && !stage_injected){
  stage_injected=1;m.now=q->start+staging-1;
  if(broken==6)owner_lost=1;
  if(broken==7)m.now=0;
 }
 if(q->publication_seen && q->stage==CMDQ_PROGRESS && !completion_injected){
  completion_injected=1;m.now=q->publication_us+completion-1;
 }
 return timer(context,out);
}
static int publication_owned(void *context,const struct nano_probe_binding *binding)
{return !owner_lost && owned(context,binding);}
static void initialize(unsigned warm)
{
 if(warm){
  first();struct nano_retire *old=&m.retirees[m.slot];
  m.released_generation=m.input.generation;m.target_generation=m.input.generation+6;
  m.released=1;m.slot=1-m.slot;m.phase=1;m.input.generation=m.target_generation;m.input.digest384[0]++;
  struct nano_probe_io ops=io();ops.time_us=publication_clock;ops.owns=publication_owned;
  struct nano_probe_binding b={17,7,m.input.generation};
  assert(!nano_probe_init_rearmed_input(&m.probes[m.slot],b,ops,&m.input,source,old,released));
 }else{
  memset(&m,0,sizeof(m));m.reg[0]=0x080f7e3f;m.reg[1]=0x0e739d18;m.reg[0x14/4]=0x400075;m.reg[0x2c/4]=4;
  m.input=(struct nano_sdm_input_binding){.owner=17,.epoch=7,.generation=4,.bytes=1708032,.chunk=1048512,.idr0=0x080f7e3f,.idr1=0x0e739d18,.idr5=0x400075};memset(m.input.digest384,1,48);
  struct nano_probe_io ops=io();ops.time_us=publication_clock;ops.owns=publication_owned;
  struct nano_probe_binding b={17,7,4};assert(!nano_probe_init_input(&m.probes[0],b,ops,&m.input,source));
 }
}
int main(void)
{
 unsigned cases=0;
 for(unsigned warm=0;warm<=1;warm++)for(unsigned scenario=0;scenario<8;scenario++){
  stage_injected=completion_injected=owner_lost=0;broken=0;initialize(warm);
  struct nano_probe_setup *p=&m.probes[m.slot];struct nano_probe_receipt receipt={0};
  p->timed=1;p->start=p->last=m.now+1;uint64_t origin=p->start;
  if(scenario==2)m.now+=2200000;
  if(scenario==3)m.now+=3900000;
  staging=(scenario==0 || scenario==2) ? 101123:350000;
  if(scenario==4)staging=NANO_CMDQ_FINAL_BOUND_US+1;
  completion=scenario==5 ? 100001:80000;broken=scenario;
  int error=nano_probe_prepare(p,&receipt);
  assert(p->start==origin && stage_injected);
  if(scenario<3){
   assert(!error && p->prepared && !p->failed && p->command.validation_policy==CMDQ_PREPARATION_PUBLICATION_BOUND);
   assert(p->command.publication_seen && p->command.completion_seen && p->command.slots_attempted==6);
   assert(p->command.publication_us-p->command.start>100000);
   assert(p->command.completion_us-p->command.publication_us<=100000);
   assert(p->last-p->start<=NANO_PROBE_BOUND_US);
   assert(!nano_probe_arm(p,&receipt) && receipt.DMA_access_granted);
   retire_current();
  }else{
   assert(error && p->failed && !p->prepared && !p->armed && !receipt.DMA_access_granted);
   assert(!p->command.completion_seen);
   if(scenario!=5)assert(!p->command.publish_attempted && !p->command.slots_attempted);
   unsigned reads=m.reads,writes=m.writes,stores=m.stores;
   assert(nano_probe_prepare(p,&receipt) && nano_probe_arm(p,&receipt));
   assert(m.reads==reads && m.writes==writes && m.stores==stores);
  }
  cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",cases);
}
