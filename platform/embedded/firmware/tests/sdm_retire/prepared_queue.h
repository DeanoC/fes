/* SPDX-License-Identifier: BSD-3-Clause */
/* A real setup command submission over isolated injected registers. */
#ifndef NANO_TEST_PREPARED_QUEUE_H
#define NANO_TEST_PREPARED_QUEUE_H
#include "command_queue.h"
#include <assert.h>
#include <string.h>
struct setup_queue_model {
 struct nano_cmdq command;
 struct nano_cmdq_binding binding;
 uint32_t producer,consumer;
 uint64_t now;
 unsigned stores,writes;
};
static struct setup_queue_model setup_queue;
static int setup_queue_owns(void *p,const struct nano_cmdq_binding *b)
{
 struct setup_queue_model *m=p;
 return b->owner==m->binding.owner && b->epoch==m->binding.epoch &&
  b->generation==m->binding.generation && b->queue==m->binding.queue &&
  b->baseline==m->binding.baseline;
}
static int setup_queue_time(void *p,uint64_t *out)
{struct setup_queue_model *m=p;*out=++m->now;return 0;}
static int setup_queue_read(void *p,uint32_t off,uint32_t *out)
{
 struct setup_queue_model *m=p;
 switch(off){
 case 0x20:case 0x24:*out=8;break;
 case 0x60:case 0x64:case 0x94:*out=0;break;
 case 0x90:*out=0x94008003;break;
 case 0x98:*out=m->producer;break;
 case 0x9c:if(m->writes)m->consumer=m->producer;*out=m->consumer;break;
 default:assert(0);
 }
 return 0;
}
static int setup_queue_write(void *p,uint32_t off,uint32_t value)
{
 struct setup_queue_model *m=p;
 assert(off==0x98 && !m->writes && value==((m->binding.baseline+6)&15U));
 m->writes++;m->producer=value;return 0;
}
static int setup_queue_store(void *p,unsigned slot,const uint8_t bytes[16])
{
 struct setup_queue_model *m=p;uint8_t expected[16];
 assert(m->stores<6 && !nano_cmdq_encode(m->stores,expected));
 assert(slot==((m->binding.baseline+m->stores)&7U) && !memcmp(expected,bytes,16));
 m->stores++;return 0;
}
static void setup_queue_barrier(void *p){(void)p;}
static struct nano_cmdq *prepared_queue_at(uint64_t generation,uint32_t before)
{
 struct setup_queue_model *m=&setup_queue;
 *m=(struct setup_queue_model){.binding={17,7,generation,0x94008000,before},.producer=before,.consumer=before};
 struct nano_cmdq_io io={m,setup_queue_owns,setup_queue_time,setup_queue_read,
  setup_queue_write,setup_queue_store,setup_queue_barrier,0,0};
 struct nano_cmdq_receipt out={0};
 assert(!nano_cmdq_init(&m->command,m->binding,io));
 assert(!nano_cmdq_submit(&m->command,&out,CMDQ_FULL_OPERATION_BOUND));
 assert(m->stores==6 && m->writes==1 && out.commands_completed &&
  out.producer==m->producer && out.consumer==m->consumer);
 return &m->command;
}
static struct nano_cmdq *prepared_queue(void)
{
 if(!setup_queue.command.initialized)prepared_queue_at(4,0);
 return &setup_queue.command;
}
#endif
