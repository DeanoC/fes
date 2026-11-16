/* SPDX-License-Identifier: BSD-3-Clause */
#define main original_retirement_test_main
#define FES_TEST_RETIRE_READ original_read32
#include "retire_test.c"
#undef FES_TEST_RETIRE_READ
#undef main

static unsigned fault,point,injected;
static struct nano_cmdq *prepared;
static void mutate(unsigned at)
{
 if(point!=at || injected)return;
 injected=1;
 switch(fault){
 case 1:prepared->binding.owner++;break;
 case 2:prepared->binding.epoch++;break;
 case 3:prepared->binding.generation++;break;
 case 4:prepared->binding.queue+=128;break;
 case 5:prepared->binding.baseline^=8;break;
 case 6:prepared->stage=CMDQ_FINAL_GUARD;break;
 case 7:prepared->failed=1;break;
 case 8:prepared->completion_seen=0;break;
 case 9:prepared->io.owns=0;break;
 case 10:prepared->last++;break;
 default:assert(0);
 }
}
static int generation_owned(void *p,const struct nano_sdm_input_binding *b)
{int result=owns(p,b);mutate(1);return result;}
static int generation_clock(void *p,uint64_t *out)
{int result=time_us(p,out);mutate(2);return result;}
static int generation_read(void *p,uint32_t off,uint32_t *out)
{int result=original_read32(p,off,out);mutate(3);return result;}
static int generation_write(void *p,uint32_t off,uint32_t value)
{int result=write32(p,off,value);mutate(4);return result;}
static int generation_store(void *p,unsigned slot,const uint8_t bytes[16])
{int result=store(p,slot,bytes);mutate(5);return result;}
static void generation_barrier(void *p){(void)p;mutate(6);}
static int generation_source(void *p,const struct nano_sdm_input_binding *b)
{int result=source(p,b);mutate(7);return result;}
static struct nano_retire_io generation_io(struct model *m)
{
 return (struct nano_retire_io){m,generation_owned,generation_source,generation_clock,
  generation_read,generation_write,generation_store,generation_barrier,prepared,0};
}
static struct nano_sdm_input_binding generation_setup(struct model *m,uint64_t generation,uint32_t before)
{
 setup(m);m->generation=generation;m->baseline=(before+6)&15U;
 m->reg[0x98/4]=m->reg[0x9c/4]=m->baseline;
 prepared=prepared_queue_at(generation,before);
 struct nano_sdm_input_binding b=binding();b.generation=generation;return b;
}
static int reject_unexpected_publish(void *p,uint32_t target,uint64_t *out)
{(void)p;(void)target;(void)out;assert(0);return -1;}
int main(void)
{
 unsigned generations=0,wraps=0,negative=0;
 /* Each setup and retirement submits the real six-command queue engine.
  * Hold the completed setup instance unchanged through its continuation. */
 for(unsigned odd=0;odd<2;odd++){
  uint32_t before=odd;uint64_t generation=4;
  for(unsigned n=0;n<128;n++){
   struct model m;struct nano_retire s={0};struct nano_retire_receipt r={0};
   struct nano_sdm_input_binding b=generation_setup(&m,generation,before);
   struct nano_retire_io io=generation_io(&m);m.current=&s;
   assert(!nano_source_retire(&s,b,io,&r));
   uint32_t after=(before+12)&15U;if(after<before)wraps++;
   assert(s.retired && r.translation_retired && r.producer==after && r.consumer==after);
   assert(s.queue_baseline==m.baseline && s.queue_target==after && m.written==5 && m.stores==6);
   assert(s.command_receipt.baseline==m.baseline && r.memory_retained &&
    !r.source_reuse_authorized && !r.ordinary_activation && !r.interconnect_drained);
   assert(!nano_source_continue(&s,b,io) && s.continued);
   unsigned writes=m.written,reads=m.reads,stores=m.stores;
   assert(nano_source_retire(&s,b,io,&r) && nano_source_continue(&s,b,io));
   assert(m.written==writes && m.reads==reads && m.stores==stores);
   before=after;generation+=(n&1) ? 5:6;generations++;
  }
 }
 for(unsigned kind=1;kind<=10;kind++)for(unsigned at=1;at<=7;at++){
  struct model m;struct nano_retire s={0};struct nano_retire_receipt r={0};
  struct nano_sdm_input_binding b=generation_setup(&m,10,12);
  struct nano_retire_io io=generation_io(&m);m.current=&s;
  fault=kind;point=at;injected=0;
  assert(nano_source_retire(&s,b,io,&r) && injected && s.failed && !s.retired);
  assert(r.memory_retained && !r.translation_retired && !r.source_reuse_authorized);
  unsigned writes=m.written,reads=m.reads,stores=m.stores;
  assert(nano_source_retire(&s,b,io,&r) && nano_source_continue(&s,b,io));
  assert(m.written==writes && m.reads==reads && m.stores==stores);negative++;
 }
 fault=point=injected=0;
 /* A completed retirement does not let continuation accept a changed setup
  * engine. Continuation has no write/store callback, even on failure. */
 for(unsigned kind=1;kind<=10;kind++)for(unsigned at=1;at<=7;at++){
  if(at==4 || at==5)continue;
  struct model m;struct nano_retire s={0};struct nano_retire_receipt r={0};
  struct nano_sdm_input_binding b=generation_setup(&m,21,4);
  struct nano_retire_io io=generation_io(&m);m.current=&s;
  assert(!nano_source_retire(&s,b,io,&r));
  unsigned writes=m.written,stores=m.stores;
  fault=kind;point=at;injected=0;
  assert(nano_source_continue(&s,b,io) && injected && s.failed && !s.continued);
  assert(m.written==writes && m.stores==stores);
  unsigned reads=m.reads;
  assert(nano_source_continue(&s,b,io) && m.reads==reads);
  fault=point=injected=0;negative++;
 }
 /* Entry rejects an incomplete, failed, stale or changed setup before MMIO. */
 for(unsigned kind=0;kind<16;kind++){
  struct model m;struct nano_retire s={0};struct nano_retire_receipt r={0};
  struct nano_sdm_input_binding b=generation_setup(&m,15,8);
  struct nano_retire_io io=generation_io(&m);
  if(kind==0)io.prepared=0;
  else if(kind==5)prepared->binding.baseline=16;
  else if(kind<=9){fault=kind;point=1;mutate(1);fault=point=injected=0;}
  else if(kind==10)prepared->completion_us=prepared->start+NANO_CMDQ_BOUND_US+1;
  else if(kind==11)prepared->validation_policy=CMDQ_RETIREMENT_PUBLICATION_BOUND;
  else if(kind==12)prepared->validation_policy=3;
  else if(kind==13){prepared->validation_policy=CMDQ_PREPARATION_PUBLICATION_BOUND;prepared->last=prepared->start+NANO_CMDQ_FINAL_BOUND_US+1;}
  else if(kind==14){prepared->validation_policy=CMDQ_PREPARATION_PUBLICATION_BOUND;prepared->completion_us=prepared->start+NANO_CMDQ_BOUND_US+1;prepared->last=prepared->completion_us;}
  else prepared->last=prepared->start+NANO_CMDQ_BOUND_US+1;
  assert(nano_source_retire(&s,b,io,&r) && s.failed && !s.retired);
  assert(!m.reads && !m.written && !m.stores && !r.translation_retired);negative++;
 }
 /* A publication-policy preparation needs its own accepted timestamp history.
  * Reject malformed receipts at entry, before any retirement MMIO. */
 for(unsigned kind=0;kind<10;kind++){
  struct model m;struct nano_retire s={0};struct nano_retire_receipt r={0};
  struct nano_sdm_input_binding b=generation_setup(&m,21,4);
  prepared->validation_policy=CMDQ_PREPARATION_PUBLICATION_BOUND;
  prepared->io.publish_timed=0; /* missing publisher is itself invalid */
  prepared->publication_seen=1;
  prepared->publication_us=prepared->start+350000;
  prepared->completion_us=prepared->publication_us+80000;prepared->last=prepared->completion_us+100;
  /* A real publisher is never called by receipt validation. */
  if(kind)prepared->io.publish_timed=reject_unexpected_publish;
  if(kind==1)prepared->publication_seen=0;
  if(kind==2)prepared->publication_seen=2;
  if(kind==3)prepared->publication_us=prepared->start-1;
  if(kind==4)prepared->publication_us=prepared->completion_us+1;
  if(kind==5){prepared->completion_us=prepared->publication_us+100001;prepared->last=prepared->completion_us;}
  if(kind==6)prepared->last=prepared->start+NANO_CMDQ_FINAL_BOUND_US+1;
  if(kind==7)prepared->completion_us=prepared->last+1;
  if(kind==8)prepared->last=prepared->start-1;
  if(kind==9)prepared->validation_policy=2;
  struct nano_retire_io io=generation_io(&m);
  assert(nano_source_retire(&s,b,io,&r) && s.failed && !s.retired);
  assert(!m.reads && !m.written && !m.stores && !r.translation_retired);negative++;
 }
 printf("{\"status\":\"passed\",\"completed_generations\":%u,\"wraps\":%u,\"negative_cases\":%u,\"hardware_actions\":false}\n",generations,wraps,negative);
}
