/* SPDX-License-Identifier: BSD-3-Clause */
#define main retained_retire_fixture_main
#include "retire_test.c"
#undef main
struct continuation_model {
 struct model base;
 struct nano_retire state;
 unsigned active,step,at,fault,mutated,time_at,clock_calls;
};
static void step(struct continuation_model *m)
{
 if(!m->active || ++m->step!=m->at)return;
 m->mutated=1;struct nano_retire *s=&m->state;
 switch(m->fault){
 case 1:s->io.context=0;break;case 2:s->io.owns=0;break;
 case 3:s->io.source_valid=0;break;case 4:s->io.time_us=0;break;
 case 5:s->io.read32=0;break;case 6:s->io.barrier=0;break;
 case 7:s->binding.owner++;break;case 8:s->binding.generation++;break;
 case 9:s->binding.digest384[0]^=1;break;case 10:s->phase=RET_DISABLE;break;
 case 13:s->io.write32=0;break;case 14:s->io.store_command=0;break;
 case 11:s->command_receipt.consumer=6;break;case 12:s->command_receipt.DMA_access_granted=1;break;
 default:assert(0);
 }
}
static int continued_owner(void *p,const struct nano_sdm_input_binding *b)
{struct continuation_model *m=p;int r=owns(&m->base,b);step(m);return r;}
static int continued_source(void *p,const struct nano_sdm_input_binding *b)
{struct continuation_model *m=p;int r=source(&m->base,b);step(m);return r;}
static int continued_time(void *p,uint64_t *v)
{
 struct continuation_model *m=p;int r=time_us(&m->base,v);step(m);
 if(m->active && ++m->clock_calls==m->time_at){
  if(m->fault==15)return -5;
  if(m->fault==16)*v=0;
  if(m->fault==17)*v=m->state.start+NANO_RETIRE_US;
 }
 return r;
}
static int continued_read(void *p,uint32_t o,uint32_t *v)
{struct continuation_model *m=p;int r=read32(&m->base,o,v);step(m);return r;}
static int continued_write(void *p,uint32_t o,uint32_t v)
{return write32(&((struct continuation_model *)p)->base,o,v);}
static int continued_store(void *p,unsigned slot,const uint8_t b[16])
{return store(&((struct continuation_model *)p)->base,slot,b);}
static void continued_barrier(void *p){step(p);}
static struct nano_retire_io io_for(struct continuation_model *m)
{return (struct nano_retire_io){m,continued_owner,continued_source,continued_time,
 continued_read,continued_write,continued_store,continued_barrier,prepared_queue()};}
static void ready(struct continuation_model *m)
{
 memset(m,0,sizeof(*m));setup(&m->base);m->base.current=&m->state;
 struct nano_retire_receipt r={0};assert(!nano_source_retire(&m->state,binding(),io_for(m),&r));
 assert(r.translation_retired && r.memory_retained && !r.source_reuse_authorized && !r.ordinary_activation);
 m->active=1;
}
int main(void)
{
 unsigned count=0;struct continuation_model good;ready(&good);
 unsigned writes=good.base.written,stores=good.base.stores;
 uint64_t before=good.state.last;
 assert(!nano_source_continue(&good.state,binding(),io_for(&good)));
 assert(good.state.continued && good.state.continuation_attempted && !good.state.failed);
 assert(good.state.last==before && good.state.continuation_last>=before);
 assert(good.base.written==writes && good.base.stores==stores);
 unsigned calls=good.step,clocks=good.clock_calls;
 assert(nano_source_continue(&good.state,binding(),io_for(&good)) && good.step==calls);count++;
 for(unsigned fault=1;fault<=14;fault++)for(unsigned at=1;at<=calls;at++){
  struct continuation_model m;ready(&m);m.fault=fault;m.at=at;
  assert(nano_source_continue(&m.state,binding(),io_for(&m)));
  assert(m.mutated && m.state.failed && !m.state.continued);
  assert(m.base.written==writes && m.base.stores==stores);
  unsigned stopped=m.step;assert(nano_source_continue(&m.state,binding(),io_for(&m)) && m.step==stopped);count++;
 }
 for(unsigned fault=15;fault<=17;fault++)for(unsigned at=1;at<=clocks;at++){
  struct continuation_model m;ready(&m);m.fault=fault;m.time_at=at;
  assert(nano_source_continue(&m.state,binding(),io_for(&m)) && m.state.failed && !m.state.continued);
  assert(m.base.written==writes && m.base.stores==stores);count++;
 }
 for(unsigned offset=0;offset<48;offset++){
  struct continuation_model m;ready(&m);m.base.fail_read=m.base.reads+offset+1;
  assert(nano_source_continue(&m.state,binding(),io_for(&m)) && m.state.failed && !m.state.continued);
  assert(m.base.written==writes && m.base.stores==stores);count++;
 }
 for(unsigned fault=0;fault<5;fault++){
  struct continuation_model m;ready(&m);
  if(fault==0)m.state.retired=0;if(fault==1)m.state.failed=1;
  if(fault==2)m.state.command_receipt.commands_completed=0;
  if(fault==3)m.state.command_receipt.interconnect_drained=1;
  if(fault==4)m.state.binding.epoch++;
  assert(nano_source_continue(&m.state,binding(),io_for(&m)) && !m.step && !m.state.continued);count++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"continuation_callbacks\":%u,\"hardware_actions\":false}\n",count,calls);
 return 0;
}
