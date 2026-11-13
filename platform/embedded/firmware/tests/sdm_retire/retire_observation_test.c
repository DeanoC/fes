/* SPDX-License-Identifier: BSD-3-Clause */
#define main retire_fixture_main
#define FES_TEST_RETIRE_READ fixture_read32
#define time_us fixture_time_us
#include "retire_test.c"
#undef time_us
#undef FES_TEST_RETIRE_READ
#undef main
static unsigned mode,consumer,injected,consumer_calls;
static int time_us(void *p,uint64_t *out)
{
 struct model *m=p;
 if(mode==7 && m->current->commands.stage==CMDQ_FINAL_GUARD && injected!=2){
  injected=2;m->now+=NANO_CMDQ_BOUND_US+1;
 }
 return fixture_time_us(p,out);
}
static int read32(void *p,uint32_t offset,uint32_t *out)
{
 struct model *m=p;struct nano_retire *s=m->current;
 int error=fixture_read32(p,offset,out);
 if(s->commands.stage==CMDQ_PROGRESS && offset==0x9c){
  consumer_calls++;*out=consumer_calls==1 ? consumer:12;
  if(!injected){
   injected=1;
   if(mode==1)m->now+=NANO_CMDQ_BOUND_US+1;
   if(mode==2)m->now+=NANO_RETIRE_US;
   if(mode==3){*out=0xdeadbeef;return -7;}
   if(mode==4)m->fail_owner=m->owners+1;
  }
 }
 if((mode==5 && s->commands.stage==CMDQ_FINAL_FIRST) ||
    (mode==6 && s->commands.stage==CMDQ_FINAL_SECOND)){
  if(!injected || injected==1){injected=2;m->now+=NANO_CMDQ_BOUND_US+1;}
 }
 return error;
}
int main(void)
{
 unsigned total=0;const unsigned values[]={6,7,9,11,12,16,0xdeadbeef};
 for(mode=0;mode<=7;mode++)for(unsigned i=0;i<sizeof(values)/sizeof(values[0]);i++){
  consumer=values[i];injected=consumer_calls=0;
  struct model m;setup(&m);struct nano_retire s={0};m.current=&s;
  struct nano_retire_receipt r={0};struct nano_retire_io io={&m,owns,source,time_us,read32,write32,store,barrier};
  int error=nano_source_retire(&s,binding(),io,&r);
  assert(consumer_calls && s.observed_reads<=s.reads && s.consumer_samples && s.consumer_read<=s.reads);
  if(!mode && consumer<=12){
   assert(!error && s.retired && s.commands.stage==CMDQ_COMPLETE);
   assert(s.observed_consumer==12 && s.consumer_phase==RET_COMPLETE && s.consumer_stage==CMDQ_COMPLETE);
   assert(s.commands.observed==s.commands.last && m.written==5);
  }else{
   assert(error && s.failed && !r.translation_retired && r.memory_retained && !r.ordinary_activation && !r.source_reuse_authorized);
   assert(m.written==4 && s.commands.publish_attempted && s.commands.slots_attempted==6);
   if(mode==3){assert(s.first_error==-7 && s.commands.first_error==-1 && s.observed_consumer==6 && s.consumer_stage==CMDQ_PUBLICATION);}
   else if(mode<=4 || consumer>12){assert(s.observed_consumer==consumer && s.consumer_stage==CMDQ_PROGRESS && s.consumer_poll==1);}
   if(mode==1){assert(s.commands.first_error==-3 && s.commands.observed>s.commands.last && s.commands.stage==CMDQ_PROGRESS);}
   if(mode==2)assert(s.first_error==-110 && s.commands.stage==CMDQ_PROGRESS);
   if(mode==4)assert(s.first_error==-13 && s.commands.stage==CMDQ_PROGRESS);
   if(mode>=5 && consumer<=12)assert(s.commands.stage==(mode==5 ? CMDQ_FINAL_FIRST:mode==6 ? CMDQ_FINAL_SECOND:CMDQ_FINAL_GUARD));
  }
  assert(!r.source_reuse_authorized && !r.ordinary_activation && !r.interconnect_drained);
  unsigned writes=m.written,reads=m.reads,times=m.times;
  assert(nano_source_retire(&s,binding(),io,&r) && m.written==writes && m.reads==reads && m.times==times);
  total++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",total);
}
