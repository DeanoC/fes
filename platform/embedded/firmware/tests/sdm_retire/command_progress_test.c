/* SPDX-License-Identifier: BSD-3-Clause */
#define main snapshot_fixture_main
#include "command_snapshot_test.c"
#undef main
static unsigned progress_mode,progress_at,progress_calls;
static int progress(void *p,uint32_t target,uint32_t *consumer)
{
 struct model *m=p;static const uint32_t steps[]={6,10,12};
 assert(target==12 && m->stores==6 && m->writes==1);
 progress_calls++;m->consumer=steps[progress_calls<=3 ? progress_calls-1:2];
 if(progress_mode==11)m->consumer=6;
 uint32_t value=0;assert(!read32(m,0x60,&value) && !value);
 assert(!read32(m,0x64,&value) && !value);
 assert(!read32(m,0x98,&value) && value==target);
 assert(!read32(m,0x9c,&value));
 if(progress_calls==progress_at){
  switch(progress_mode){
  case 1:return -7;
  case 2:m->queue.io.progress=0;break;
  case 3:m->queue.io.context=0;break;
  case 4:m->queue.io.owns=foreign;break;
  case 5:m->owned=0;break;
  case 6:m->now+=NANO_CMDQ_BOUND_US+1;break;
  case 7:m->now=0;break;
  case 8:value=16;break;
  case 9:value=13;break;
  case 10:assert(progress_at==3);value=8;break;
  case 12:m->queue.io.snapshot=0;break;
  case 13:m->queue.io.read32=0;break;
  case 14:*consumer=12;return -7;
  case 15:m->queue.binding.generation++;break;
  case 16:m->queue.io.barrier=0;break;
  default:break;
  }
 }
 *consumer=value;return 0;
}
int main(void)
{
 unsigned cases=0;
 for(unsigned mode=0;mode<=16;mode++)for(unsigned at=1;at<=3;at++){
  if(mode==10 && at!=3)continue;
  struct model m={.owned=1,.producer=6,.consumer=6};progress_mode=mode;progress_at=at;progress_calls=0;
  struct nano_cmdq_binding b={17,7,4,0x94008000,6};
  struct nano_cmdq_io io={&m,owns,clock_us,read32,write32,store,barrier,snapshot,progress};
  assert(!nano_cmdq_init(&m.queue,b,io));
  struct nano_cmdq_receipt r={0};int error=nano_cmdq_submit(&m.queue,&r,CMDQ_FULL_OPERATION_BOUND);
  if(!mode){assert(!error && progress_calls==3 && r.commands_completed && r.producer==12 && r.consumer==12 && m.snapshots==5);}
  else{
   assert(error && m.queue.failed && !r.commands_completed && m.stores==6 && m.writes==1 && m.snapshots==3);
   assert(progress_calls==(mode==11 ? NANO_CMDQ_POLLS:at));
   if(mode==6 || mode==7)assert(m.queue.first_error==-3);
   if(mode==8 || mode==9 || mode==10)assert(m.queue.first_error==-5);
   if(mode==11)assert(m.queue.first_error==-110);
  }
  assert(!r.interconnect_drained && !r.DMA_access_granted);
  assert(nano_cmdq_submit(&m.queue,&r,CMDQ_FULL_OPERATION_BOUND) && m.stores==6 && m.writes==1);cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",cases);
}
