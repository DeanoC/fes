/* SPDX-License-Identifier: BSD-3-Clause */
#include "source_retire.h"
#include <string.h>
#if defined(FES_NANO_COLD_SERIAL) && FES_NANO_COLD_SERIAL
#include "fes_nano_cold_profile.h"
#endif
#if !defined(FES_NANO_RETIRE_BATCHED_OBSERVE) || FES_NANO_RETIRE_BATCHED_OBSERVE != 1
#error Explicit_bounded_read_batch_profile_required
#endif
_Static_assert(NANO_CMDQ_FINAL_BOUND_US==NANO_RETIRE_US,"same enclosing retirement bound required");
#define GBPA 0x44U
#define ABORT 0x100000U
#define UPDATE 0x80000000U
static int fail(struct nano_retire *s,int error)
{s->failed=1;if(!s->first_error)s->first_error=error ? error:-1;return -1;}
/* The supplier pins the real setup controller while this one-shot retirement
 * and continuation run. Never infer its empty cursor from an MMIO read or a
 * caller's copied receipt, and never reset an uncertain controller. */
static int prepared_same(const struct nano_retire *s)
{
 return s->prepared_engine && s->io.prepared==s->prepared_engine &&
  !memcmp(s->prepared_engine,&s->prepared_snapshot,sizeof(s->prepared_snapshot)) &&
  s->queue_baseline==((s->prepared_snapshot.binding.baseline+NANO_CMDQ_COMMANDS)&15U) &&
  s->queue_target==((s->queue_baseline+NANO_CMDQ_COMMANDS)&15U);
}
static int prepared_valid(const struct nano_cmdq *q,const struct nano_sdm_input_binding *b)
{
 return q && q->binding.owner==b->owner && q->binding.epoch==b->epoch &&
  q->binding.generation==b->generation && q->binding.queue==0x94008000 &&
  q->binding.baseline<=15 && q->attempted==1 && q->initialized==1 &&
  q->submitted==1 && q->timed==1 && !q->failed && !q->first_error &&
  q->slots_attempted==NANO_CMDQ_COMMANDS && q->publish_attempted==1 &&
  q->stage==CMDQ_COMPLETE && q->completion_seen==1 &&
  q->validation_policy==CMDQ_FULL_OPERATION_BOUND && q->last>=q->start &&
  q->completion_us>=q->start && q->completion_us<=q->last &&
  q->last-q->start<=NANO_CMDQ_BOUND_US && q->io.context && q->io.owns &&
  q->io.time_us && q->io.read32 && q->io.write32 && q->io.store && q->io.barrier;
}
static void observed_read(struct nano_retire *s,uint32_t offset,uint32_t value)
{
 s->observed_offset=offset;s->observed_value=value;s->observed_reads++;
 if(offset==0x9c){
  s->observed_consumer=value;s->consumer_samples++;s->consumer_read=s->reads;
  s->consumer_phase=s->phase;s->consumer_stage=s->commands.stage;
  s->consumer_poll=s->commands.polls;
 }
}
static int io_same(const struct nano_retire_io *a,const struct nano_retire_io *b)
{return a->context==b->context && a->owns==b->owns && a->source_valid==b->source_valid &&
 a->time_us==b->time_us && a->read32==b->read32 && a->write32==b->write32 &&
 a->store_command==b->store_command && a->barrier==b->barrier && a->prepared==b->prepared;}
static int unchanged(const struct nano_retire *s,const struct nano_retire_io *io,
 const struct nano_sdm_input_binding *binding)
{
 return !s->failed && io_same(io,&s->io) && prepared_same(s) &&
  nano_sdm_input_same(binding,&s->binding);
}
static int guard(struct nano_retire *s)
{
 uint64_t now=0;
 const struct nano_retire_io io=s->io;
 const struct nano_sdm_input_binding binding=s->binding;
 if(s->failed)return -1;
 if(!prepared_same(s))return fail(s,-13);
 s->guard_calls++;
 if(io.time_us(io.context,&now))return fail(s,-2);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 s->observed=now;s->clock_samples++;
 if(s->phase<7){
  unsigned bit=1U<<s->phase;
  if(!(s->phase_seen&bit)){s->phase_first[s->phase]=now;s->phase_seen|=bit;}
  s->phase_last[s->phase]=now;
 }
 s->owner_checks++;
 if(io.owns(io.context,&s->binding)!=1 || !unchanged(s,&io,&binding))return fail(s,-13);
 if(!s->timed){s->timed=1;s->start=s->last=now;}
 if(now<s->last || now-s->start>=NANO_RETIRE_US)return fail(s,-110);
 s->last=now;return 0;
}
static int read32(struct nano_retire *s,uint32_t offset,uint32_t *value)
{
 const struct nano_retire_io io=s->io;
 const struct nano_sdm_input_binding binding=s->binding;
 if(guard(s) || !unchanged(s,&io,&binding))return fail(s,-13);
 io.barrier(io.context);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 s->reads++;int e=io.read32(io.context,offset,value);
 if(!e)observed_read(s,offset,*value);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 io.barrier(io.context);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 if(e)return fail(s,e);
 return guard(s);
}
static int eq(struct nano_retire *s,uint32_t offset,uint32_t expected)
{
 uint32_t got=0;if(read32(s,offset,&got))return -1;
 if(got!=expected){s->bad_offset=offset;s->bad_value=got;return fail(s,-5);}return 0;
}
static int write_ready(struct nano_retire *,uint32_t,uint32_t);
static int write32(struct nano_retire *s,uint32_t offset,uint32_t value)
{
 const struct nano_retire_io io=s->io;
 const struct nano_sdm_input_binding binding=s->binding;
 if(guard(s) || write_ready(s,offset,value) || !unchanged(s,&io,&binding))return fail(s,-13);
 io.barrier(io.context);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 s->writes++;int e=io.write32(io.context,offset,value);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 io.barrier(io.context);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 if(e)return fail(s,e);
 return guard(s);
}
static int source(struct nano_retire *s)
{
 const struct nano_retire_io io=s->io;
 const struct nano_sdm_input_binding binding=s->binding;
 if(guard(s) || !unchanged(s,&io,&binding))return fail(s,-13);
 s->source_checks++;
 if(io.source_valid(io.context,&s->binding)!=1 || !unchanged(s,&io,&binding))return fail(s,-13);
 s->source_passes++;
 return guard(s);
}
/* These exact register reads have no acknowledgement or publication effect.
 * Ownership/time is checked before and after the bounded read-only batch.
 * Individual read errors and mismatches still stop at their first observation. */
static int observe(struct nano_retire *s,const uint32_t (*rows)[2],unsigned count,const uint32_t (*tail)[2],unsigned extra,uint32_t *consumer)
{
 if(!rows || !count || count>NANO_RETIRE_OBSERVE_MAX || extra>NANO_RETIRE_OBSERVE_MAX-count ||
    (extra && !tail) || (consumer && count+extra==NANO_RETIRE_OBSERVE_MAX))return fail(s,-22);
 struct nano_retire_io io=s->io;
 const struct nano_sdm_input_binding binding=s->binding;
 if(guard(s) || !unchanged(s,&io,&binding))return fail(s,-13);
 uint32_t observed=0;
 for(unsigned n=0;n<count+extra+(consumer!=0);n++){
  const uint32_t *row=n==count+extra ? 0:n<count ? rows[n]:tail[n-count];
  if(!unchanged(s,&io,&binding))return fail(s,-13);
  uint32_t got=0;io.barrier(io.context);
  if(!unchanged(s,&io,&binding))return fail(s,-13);
  s->reads++;int e=io.read32(io.context,row ? row[0]:0x9c,&got);
  if(!e)observed_read(s,row ? row[0]:0x9c,got);
  if(!unchanged(s,&io,&binding))return fail(s,-13);
  io.barrier(io.context);
  if(e)return fail(s,e);
  if(!unchanged(s,&io,&binding))return fail(s,-13);
  if(row && got!=row[1]){s->bad_offset=row[0];s->bad_value=got;return fail(s,-5);}
  if(!row)observed=got;
 }
 if(guard(s))return -1;
 if(consumer)*consumer=observed;
 return 0;
}
static int fixed_view(struct nano_retire *s,const uint32_t (*tail)[2],unsigned extra,uint32_t *consumer)
{
 static const uint32_t rows[][2]={
 {0,0x080f7e3f},{4,0x0e739d18},{0x14,0x00400075},{0x28,0x820},{0x2c,4},
 {0x48,0},{0x50,0},{0x54,0},{0x60,0},{0x64,0},
 {0x80,0x94000000},{0x84,0},{0x88,8},{0x90,0x94008003},{0x94,0},
 {0xa0,0x94009003},{0xa4,0},{0x100a8,0},{0x100ac,0}};
 return observe(s,rows,sizeof(rows)/sizeof(rows[0]),tail,extra,consumer);
}
static int fixed(struct nano_retire *s){return fixed_view(s,0,0,0);}
/* No control/publication write uses a cached observation. The original
 * pre/post-write ownership gates remain, with an additional fresh register
 * view immediately before each of the same five allowed writes. */
static int write_ready(struct nano_retire *s,uint32_t offset,uint32_t value)
{
 uint32_t control=0,pointer=s->queue_baseline,abort=ABORT;
 switch(s->phase){
 case RET_ABORT:if(offset!=GBPA || value!=(UPDATE|ABORT))return fail(s,-22);control=13;abort=0;break;
 case RET_NO_TRANSLATION:if(offset!=0x20 || value!=12)return fail(s,-22);control=13;break;
 case RET_COMMAND_ONLY:if(offset!=0x20 || value!=8)return fail(s,-22);control=12;break;
 case RET_INVALIDATE:if(offset!=0x98 || value!=s->queue_target)return fail(s,-22);control=8;break;
 case RET_DISABLE:if(offset!=0x20 || value!=0)return fail(s,-22);control=8;pointer=s->queue_target;break;
 default:return fail(s,-22);
 }
 const uint32_t rows[][2]={{GBPA,abort},{0x20,control},{0x24,control},{0x98,pointer},{0x9c,pointer}};
 return fixed_view(s,rows,sizeof(rows)/sizeof(rows[0]),0);
}
static int snapshot(struct nano_retire *s,uint32_t control,uint32_t pointer,uint32_t abort)
{const uint32_t tail[][2]={{GBPA,abort},{0x20,control},{0x24,control},{0x98,pointer},{0x9c,pointer}};
 return fixed_view(s,tail,sizeof(tail)/sizeof(tail[0]),0);}
static int transition(struct nano_retire *s,uint32_t before,uint32_t after)
{
 if(source(s) || snapshot(s,before,s->queue_target,ABORT))return -1;
 if(write32(s,0x20,after))return -1;
 for(unsigned n=0;n<NANO_RETIRE_POLLS;n++){
  uint32_t ack=0;s->polls++;
  if(fixed(s) || eq(s,GBPA,ABORT) || eq(s,0x20,after) || read32(s,0x24,&ack))return -1;
  if(ack==after)return snapshot(s,after,s->queue_target,ABORT);
  if(ack!=before){s->bad_offset=0x24;s->bad_value=ack;return fail(s,-5);}
 }
 return fail(s,-110);
}
static int initial_transition(struct nano_retire *s,uint32_t before,uint32_t after)
{
 /* Preserve the setup controller's owned empty cursor until publication. */
 if(source(s) || snapshot(s,before,s->queue_baseline,ABORT) || write32(s,0x20,after))return -1;
 for(unsigned n=0;n<NANO_RETIRE_POLLS;n++){
  uint32_t ack=0;s->polls++;
  if(fixed(s) || eq(s,GBPA,ABORT) || eq(s,0x20,after) || read32(s,0x24,&ack))return -1;
  if(ack==after)return snapshot(s,after,s->queue_baseline,ABORT);
  if(ack!=before){s->bad_offset=0x24;s->bad_value=ack;return fail(s,-5);}
 }
 return fail(s,-110);
}
static int cmd_binding(const struct nano_retire *s,const struct nano_cmdq_binding *b)
{
 return s->phase==RET_INVALIDATE && b->owner==s->binding.owner && b->epoch==s->binding.epoch &&
    b->generation==s->binding.generation && b->queue==0x94008000 && b->baseline==s->queue_baseline;
}
static int cmd_owns(void *context,const struct nano_cmdq_binding *b)
{
 struct nano_retire *s=context;
 if(!cmd_binding(s,b))return 0;
 const uint32_t tail[][2]={{GBPA,ABORT},{0x20,8},{0x24,8}};
 /* fixed_view already checks fresh retirement ownership/time on both sides
  * of this exact read-only batch. No device access precedes its first guard. */
 return !fixed_view(s,tail,sizeof(tail)/sizeof(tail[0]),0) && cmd_binding(s,b);
}
static int cmd_time(void *context,uint64_t *out)
{
 struct nano_retire *s=context;uint64_t now=0;
 if(!s || !out || s->failed || !s->timed || s->phase!=RET_INVALIDATE)return -1;
 struct nano_retire_io io=s->io;
 /* The queue asks for a counter, then calls cmd_owns before any I/O. Keep
  * that clock origin; do not nest a third whole-platform observation here.
  * Reject a changed supplier before the queue can invoke its ownership view. */
 if(!io.time_us || io.time_us(io.context,&now))return fail(s,-2);
 if(s->failed || s->phase!=RET_INVALIDATE || !io_same(&io,&s->io) || !prepared_same(s))return fail(s,-13);
 if(now<s->last || now-s->start>=NANO_RETIRE_US)return fail(s,-110);
 *out=now;return 0;
}
static int cmd_read(void *context,uint32_t offset,uint32_t *value)
{return read32(context,offset,value);}
static int cmd_write(void *context,uint32_t offset,uint32_t value)
{struct nano_retire *s=context;if(offset!=0x98 || value!=s->queue_target)return fail(s,-22);return write32(s,offset,value);}
static int cmd_store(void *context,unsigned slot,const uint8_t data[16])
{
 struct nano_retire *s=context;
 const struct nano_retire_io io=s->io;
 const struct nano_sdm_input_binding binding=s->binding;
 if(guard(s) || !unchanged(s,&io,&binding))return fail(s,-13);
 int e=io.store_command(io.context,slot,data);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 io.barrier(io.context);
 if(!unchanged(s,&io,&binding))return fail(s,-13);
 if(e)return fail(s,e);
 return guard(s);
}
static void cmd_barrier(void *context)
{struct nano_retire *s=context;s->io.barrier(s->io.context);}
static int cmd_snapshot(void *context,uint32_t pointer)
{
 struct nano_retire *s=context;
 if(!s || s->phase!=RET_INVALIDATE || (pointer!=s->queue_baseline && pointer!=s->queue_target))return -22;
 /* The existing 24-register snapshot contains every queue register plus
  * fixed capability, abort, event and error state. It already checks the
  * exact source/owner and callback table before and after a read-only batch.
  * Queue slot stores and the producer write keep their per-operation gates. */
 return snapshot(s,8,pointer,ABORT);
}
static int cmd_progress(void *context,uint32_t target,uint32_t *consumer)
{
 struct nano_retire *s=context;
 if(!s || !consumer || s->phase!=RET_INVALIDATE || target!=s->queue_target)return -22;
 /* Exactly 24 read-only registers: the same fixed capabilities, error/event,
  * queue bases, abort/control state and producer, then a fresh consumer.
  * The result stays private until fresh post-batch ownership/time passes. */
 const uint32_t tail[][2]={{GBPA,ABORT},{0x20,8},{0x24,8},{0x98,target}};
 return fixed_view(s,tail,sizeof(tail)/sizeof(tail[0]),consumer);
}
int nano_source_retire(struct nano_retire *s,struct nano_sdm_input_binding binding,
 struct nano_retire_io io,struct nano_retire_receipt *out)
{
 if(!s || s->attempted)return -1;
 s->attempted=1;
 if(s->failed || s->retired || s->timed || s->phase || s->reads || s->writes || s->polls || s->commands.attempted)return fail(s,-22);
 s->binding=binding;s->io=io;
 uintptr_t a=(uintptr_t)s,b=(uintptr_t)out;
 if(!out || a>UINTPTR_MAX-sizeof(*s) || b>UINTPTR_MAX-sizeof(*out) ||
    (a<b+sizeof(*out) && b<a+sizeof(*s)) || nano_sdm_input_validate(&binding) ||
    !io.context || !io.owns || !io.source_valid || !io.time_us ||
    !io.read32 || !io.write32 || !io.store_command || !io.barrier ||
    !prepared_valid(io.prepared,&binding))return fail(s,-22);
 s->prepared_engine=io.prepared;
 memcpy(&s->prepared_snapshot,io.prepared,sizeof(s->prepared_snapshot));
 s->queue_baseline=(io.prepared->binding.baseline+NANO_CMDQ_COMMANDS)&15U;
 s->queue_target=(s->queue_baseline+NANO_CMDQ_COMMANDS)&15U;
 *out=(struct nano_retire_receipt){.memory_retained=1};
 if(source(s) || snapshot(s,13,s->queue_baseline,0) || snapshot(s,13,s->queue_baseline,0))return -1;
 s->phase=RET_ABORT;if(write32(s,GBPA,UPDATE|ABORT))return -1;
 unsigned settled=0;
 for(unsigned n=0;n<NANO_RETIRE_POLLS;n++){
  uint32_t value=0;s->polls++;
  if(fixed(s) || eq(s,0x20,13) || eq(s,0x24,13) || read32(s,GBPA,&value))return -1;
  if(value==ABORT){settled=1;break;}
  if(value!=(UPDATE|ABORT)){s->bad_offset=GBPA;s->bad_value=value;return fail(s,-5);}
 }
 if(!settled)return fail(s,-110);
 s->phase=RET_NO_TRANSLATION;if(initial_transition(s,13,12))return -1;
 s->phase=RET_COMMAND_ONLY;if(initial_transition(s,12,8))return -1;
 s->phase=RET_INVALIDATE;
 struct nano_cmdq_binding cb={binding.owner,binding.epoch,binding.generation,0x94008000,s->queue_baseline};
 struct nano_cmdq_io ci={s,cmd_owns,cmd_time,cmd_read,cmd_write,cmd_store,cmd_barrier,cmd_snapshot,cmd_progress};
 if(nano_cmdq_init(&s->commands,cb,ci) || nano_cmdq_submit(&s->commands,&s->command_receipt,CMDQ_RETAINED_FINAL_VALIDATION))return fail(s,s->commands.first_error);
 struct nano_cmdq_receipt *r=&s->command_receipt;
 if(r->owner!=binding.owner || r->epoch!=binding.epoch || r->generation!=binding.generation ||
    r->baseline!=s->queue_baseline || r->producer!=s->queue_target || r->consumer!=s->queue_target || r->commands_completed!=1 ||
    r->interconnect_drained || r->DMA_access_granted)return fail(s,-13);
 s->phase=RET_DISABLE;if(transition(s,8,0))return -1;
 s->phase=RET_COMPLETE;
 if(source(s) || snapshot(s,0,s->queue_target,ABORT) || snapshot(s,0,s->queue_target,ABORT) || guard(s))return -1;
 s->phase=RET_COMPLETE;s->retired=1;
 *out=(struct nano_retire_receipt){.owner=binding.owner,.epoch=binding.epoch,.generation=binding.generation,
 .elapsed_us=s->last-s->start,.producer=s->queue_target,.consumer=s->queue_target,.control=0,.abort=ABORT,
 .translation_retired=1,.memory_retained=1};
 return 0;
}

/* Continuation is separate from the immutable retirement observation. The
 * caller supplies its private engine and freshly constructed platform binding,
 * never a copied host/retirement receipt. No write/store callback is called. */
static int continuation_state(struct nano_retire *s,const struct nano_sdm_input_binding *b,
 const struct nano_retire_io *io)
{
 const struct nano_cmdq_receipt *r=&s->command_receipt;
 return s->attempted==1 && s->timed==1 && s->retired==1 && !s->failed && !s->first_error &&
  s->phase==RET_COMPLETE && s->continuation_attempted==1 && !s->continued &&
  nano_sdm_input_same(&s->binding,b) && io_same(&s->io,io) && prepared_same(s) &&
  s->commands.initialized==1 && s->commands.submitted==1 && !s->commands.failed &&
  !s->commands.first_error && r->owner==b->owner && r->epoch==b->epoch &&
  r->generation==b->generation && r->baseline==s->queue_baseline && r->producer==s->queue_target && r->consumer==s->queue_target &&
  r->commands_completed==1 && !r->interconnect_drained && !r->DMA_access_granted;
}
static int continuation_guard(struct nano_retire *s,const struct nano_sdm_input_binding *b,
 const struct nano_retire_io *io)
{
 uint64_t now=0;
 if(!continuation_state(s,b,io))return fail(s,-13);
 int error=io->time_us(io->context,&now);s->continuation_observed=now;
 if(error)return fail(s,error);
 if(!continuation_state(s,b,io))return fail(s,-13);
 if(io->owns(io->context,b)!=1)return fail(s,-13);
 if(!continuation_state(s,b,io))return fail(s,-13);
 if(now<s->continuation_last || now<s->start || now-s->start>=NANO_RETIRE_US)return fail(s,-110);
 s->continuation_last=now;return 0;
}
int nano_source_continue(struct nano_retire *s,struct nano_sdm_input_binding b,
 struct nano_retire_io io)
{
 if(!s || s->continuation_attempted)return -1;
 s->continuation_attempted=1;
 if(nano_sdm_input_validate(&b) || !io.context || !io.owns ||
    !io.source_valid || !io.time_us || !io.read32 || !io.write32 ||
    !io.store_command || !io.barrier || !continuation_state(s,&b,&io))return fail(s,-22);
 s->continuation_last=s->last;
 if(continuation_guard(s,&b,&io))return -1;
 int error=io.source_valid(io.context,&b);
 if(error!=1 || !continuation_state(s,&b,&io))return fail(s,-13);
 if(continuation_guard(s,&b,&io))return -1;
 const uint32_t rows[][2]={
  {0,0x080f7e3f},{4,0x0e739d18},{0x14,0x00400075},{0x28,0x820},{0x2c,4},
  {0x48,0},{0x50,0},{0x54,0},{0x60,0},{0x64,0},
  {0x80,0x94000000},{0x84,0},{0x88,8},{0x90,0x94008003},{0x94,0},
  {0xa0,0x94009003},{0xa4,0},{0x100a8,0},{0x100ac,0},
  {GBPA,ABORT},{0x20,0},{0x24,0},{0x98,s->queue_target},{0x9c,s->queue_target}};
 for(unsigned pass=0;pass<2;pass++){
  if(continuation_guard(s,&b,&io))return -1;
  for(unsigned n=0;n<sizeof(rows)/sizeof(rows[0]);n++){
   if(!continuation_state(s,&b,&io))return fail(s,-13);
   io.barrier(io.context);
   if(!continuation_state(s,&b,&io))return fail(s,-13);
   uint32_t got=0;error=io.read32(io.context,rows[n][0],&got);
   if(error)return fail(s,error);
   if(!continuation_state(s,&b,&io))return fail(s,-13);
   io.barrier(io.context);
   if(!continuation_state(s,&b,&io))return fail(s,-13);
   if(got!=rows[n][1]){s->bad_offset=rows[n][0];s->bad_value=got;return fail(s,-5);}
  }
  if(continuation_guard(s,&b,&io))return -1;
 }
 if(continuation_guard(s,&b,&io))return -1;
 s->continued=1;return 0;
}
