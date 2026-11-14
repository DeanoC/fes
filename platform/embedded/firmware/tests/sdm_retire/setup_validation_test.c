/* SPDX-License-Identifier: BSD-3-Clause */
/* Real input preparation, arm and retirement; injected clock/register costs. */
#define main warm_fixture_main
#include "warm_probe_test.c"
#undef main
static unsigned fault,point,injected,owner_lost;
static uint64_t completion_delay,final_delay;
static int validation_time(void *c,uint64_t *out)
{
 struct nano_probe_setup *p=&m.probes[m.slot];struct nano_cmdq *q=&p->command;
 if(q->timed && q->stage==CMDQ_PROGRESS && !injected){m.now=q->start+completion_delay;injected=1;}
 if(q->completion_seen && q->stage==point && injected==1){
  injected=2;
  if(fault==2)m.now=p->start+NANO_PROBE_BOUND_US+1;
  else if(fault==3)m.now=0;
  else m.now=q->start+final_delay;
  if(fault==4)owner_lost=1;
  if(fault==5)m.reg[0x60/4]=1;
 }
 return timer(c,out);
}
static int validation_owned(void *c,const struct nano_probe_binding *b)
{return !owner_lost && owned(c,b);}
static struct nano_probe_io validation_io(void)
{struct nano_probe_io result=io();result.time_us=validation_time;result.owns=validation_owned;return result;}
static void initialize_case(unsigned warm)
{
 if(warm){
  first();struct nano_retire *old=&m.retirees[m.slot];
  m.released_generation=m.input.generation;m.target_generation=m.input.generation+6;
  m.released=1;m.slot=1-m.slot;m.phase=1;m.input.generation=m.target_generation;m.input.digest384[0]++;
  struct nano_probe_binding b={17,7,m.input.generation};
  assert(!nano_probe_init_rearmed_input(&m.probes[m.slot],b,validation_io(),&m.input,source,old,released));
 }else{
  memset(&m,0,sizeof(m));m.reg[0]=0x080f7e3f;m.reg[1]=0x0e739d18;m.reg[0x14/4]=0x400075;m.reg[0x2c/4]=4;
  m.input=(struct nano_sdm_input_binding){.owner=17,.epoch=7,.generation=4,.bytes=1708032,.chunk=1048512,
    .idr0=0x080f7e3f,.idr1=0x0e739d18,.idr5=0x400075};memset(m.input.digest384,1,48);
  struct nano_probe_binding b={17,7,4};
  assert(!nano_probe_init_input(&m.probes[0],b,validation_io(),&m.input,source));
 }
}
int main(void)
{
 unsigned cases=0;
 for(unsigned warm=0;warm<=1;warm++)for(unsigned stage=CMDQ_FINAL_FIRST;stage<=CMDQ_FINAL_GUARD;stage++)
 for(unsigned failure=0;failure<=6;failure++){
  fault=owner_lost=injected=0;initialize_case(warm);
  fault=failure;point=stage;completion_delay=failure==1 ? 100001:79457;final_delay=100672;
  if(failure==6)final_delay=NANO_CMDQ_FINAL_BOUND_US+1;
  struct nano_probe_setup *p=&m.probes[m.slot];struct nano_probe_receipt receipt={0};
  int error=nano_probe_prepare(p,&receipt);
  if(!failure){
   assert(!error && injected==2 && p->prepared && !p->armed);
   assert(p->command.validation_policy==CMDQ_PREPARATION_FINAL_VALIDATION && p->command.completion_seen);
   assert(p->command.completion_us-p->command.start<=NANO_CMDQ_BOUND_US && p->command.last-p->command.start>NANO_CMDQ_BOUND_US);
   assert(p->last-p->start<=NANO_PROBE_BOUND_US);
   assert(!nano_probe_arm(p,&receipt) && receipt.DMA_access_granted && !receipt.interconnect_drained);
   retire_current(); /* Pins the actual completed preparation with its new policy. */
  }else{
   assert(error && p->failed && !p->prepared && !p->armed && !receipt.DMA_access_granted);
   assert(p->command.completion_seen==(failure!=1));
   unsigned reads=m.reads,writes=m.writes,stores=m.stores;
   assert(nano_probe_prepare(p,&receipt) && nano_probe_arm(p,&receipt));
   assert(m.reads==reads && m.writes==writes && m.stores==stores);
  }
  cases++;
 }
 /* Final verification never rebases the parent lifetime for the later arm. */
 fault=owner_lost=injected=0;initialize_case(0);point=CMDQ_FINAL_SECOND;completion_delay=79457;final_delay=100672;
 struct nano_probe_setup *p=&m.probes[0];struct nano_probe_receipt receipt={0};assert(!nano_probe_prepare(p,&receipt));
 m.now=p->start+NANO_PROBE_BOUND_US+1;unsigned writes=m.writes;
 assert(nano_probe_arm(p,&receipt) && p->first_error==-110 && m.writes==writes && !p->armed);
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",cases+1);
}
