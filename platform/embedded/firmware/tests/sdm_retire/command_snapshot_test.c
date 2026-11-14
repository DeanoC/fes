/* SPDX-License-Identifier: BSD-3-Clause */
#include "command_queue.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>
struct model {
 struct nano_cmdq queue;
 uint64_t now;
 unsigned snapshots,stores,writes,reads,mode,at,owned;
 uint32_t producer,consumer;
};
static int foreign(void *p,const struct nano_cmdq_binding *b)
{(void)p;(void)b;assert(!"mutated callback invoked");return 0;}
static int owns(void *p,const struct nano_cmdq_binding *b)
{struct model *m=p;return m->owned && b->owner==17 && b->epoch==7 && b->generation==4;}
static int clock_us(void *p,uint64_t *out)
{struct model *m=p;*out=++m->now;return 0;}
static int read32(void *p,uint32_t offset,uint32_t *out)
{
 struct model *m=p;m->reads++;
 switch(offset){
 case 0x20:case 0x24:*out=8;break;
 case 0x90:*out=0x94008003;break;
 case 0x94:case 0x60:case 0x64:*out=0;break;
 case 0x98:*out=m->producer;break;
 case 0x9c:*out=m->consumer;break;
 default:assert(!"unexpected queue register");
 }return 0;
}
static int store(void *p,unsigned slot,const uint8_t bytes[16])
{
 struct model *m=p;uint8_t expected[16];
 assert(!m->writes && m->stores<6 && slot==((6+m->stores)&7));
 assert(!nano_cmdq_encode(m->stores,expected) && !memcmp(expected,bytes,16));
 m->stores++;return 0;
}
static int write32(void *p,uint32_t offset,uint32_t value)
{
 struct model *m=p;assert(m->stores==6 && !m->writes && offset==0x98 && value==12);
 m->writes++;m->producer=m->consumer=value;return 0;
}
static void barrier(void *p){(void)p;}
static int snapshot(void *p,uint32_t pointer)
{
 struct model *m=p;
 static const uint32_t offsets[]={0x20,0x24,0x90,0x94,0x60,0x64,0x98,0x9c};
 const uint32_t expected[]={8,8,0x94008003,0,0,0,pointer,pointer};
 m->snapshots++;
 for(unsigned n=0;n<8;n++){
  uint32_t value;assert(!read32(m,offsets[n],&value));
  if(m->mode==1 && m->snapshots==m->at && n==2)return -7;
  if(value!=expected[n])return -5;
 }
 if(m->snapshots==m->at){
  if(m->mode==2)m->queue.io.owns=foreign;
  if(m->mode==3)m->owned=0;
  if(m->mode==4)m->now+=NANO_CMDQ_BOUND_US+1;
  if(m->mode==5)m->queue.io.snapshot=0;
 }
 return 0;
}
int main(void)
{
 unsigned cases=0;
 for(unsigned mode=0;mode<=5;mode++)for(unsigned at=1;at<=5;at++){
  struct model m={.mode=mode,.at=at,.owned=1,.producer=6,.consumer=6};
  struct nano_cmdq_binding b={17,7,4,0x94008000,6};
  struct nano_cmdq_io io={&m,owns,clock_us,read32,write32,store,barrier,snapshot,0};
  assert(!nano_cmdq_init(&m.queue,b,io));
  struct nano_cmdq_receipt r={0};int e=nano_cmdq_submit(&m.queue,&r,CMDQ_FULL_OPERATION_BOUND);
  if(!mode){assert(!e && m.snapshots==5 && m.stores==6 && m.writes==1 && r.commands_completed);}
  else{
   assert(e && m.queue.failed && !r.commands_completed && m.snapshots==at);
   if(at<=3)assert(!m.writes && !m.queue.publish_attempted);
   if(at<=2)assert(!m.stores);
   if(mode==4)assert(m.queue.first_error==-3);
  }
  assert(!r.interconnect_drained && !r.DMA_access_granted);
  unsigned writes=m.writes,stores=m.stores;
  assert(nano_cmdq_submit(&m.queue,&r,CMDQ_FULL_OPERATION_BOUND) && m.writes==writes && m.stores==stores);
  cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",cases);
 return 0;
}
