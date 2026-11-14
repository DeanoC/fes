/* SPDX-License-Identifier: BSD-3-Clause */
#include "probe_setup.h"
#include <string.h>
static int fail(struct nano_probe_setup *s,int error)
{
    s->failed=1;
    if (!s->first_error) s->first_error=error<0 ? error : -1;
    return s->first_error;
}
static int io_same(const struct nano_probe_io *a,const struct nano_probe_io *b)
{
 return a->context==b->context && a->owns==b->owns && a->time_us==b->time_us &&
  a->read32==b->read32 && a->write32==b->write32 && a->clear==b->clear &&
  a->store64==b->store64 && a->load64==b->load64 && a->store_block==b->store_block &&
  a->load_block==b->load_block && a->barrier==b->barrier;
}
static int integrity(const struct nano_probe_setup *s)
{
 if(!io_same(&s->io,&s->trusted_io) || s->binding.owner!=s->trusted_binding.owner ||
    s->binding.epoch!=s->trusted_binding.epoch || s->binding.generation!=s->trusted_binding.generation ||
    s->input_scope!=s->trusted_input_scope || s->input_selected!=(s->trusted_input_scope!=0) ||
    (s->input_selected ? !nano_sdm_input_same(&s->input,&s->trusted_input) :
     memcmp(&s->input,&s->trusted_input,sizeof(s->input))))return 0;
 if(!s->warm)return !s->previous && !s->pinned_previous && !s->warm_scope && !s->trusted_warm_scope &&
    !s->queue_baseline && s->queue_target==6 && !s->abort;
 return s->warm==1 && s->input_selected==1 && s->previous && s->previous==s->pinned_previous &&
    s->warm_scope && s->warm_scope==s->trusted_warm_scope &&
    !memcmp(s->previous,&s->previous_snapshot,sizeof(s->previous_snapshot)) &&
    s->previous->prepared_engine &&
    !memcmp(s->previous->prepared_engine,&s->previous->prepared_snapshot,sizeof(s->previous->prepared_snapshot)) &&
    s->queue_baseline==s->previous_snapshot.queue_target && s->queue_baseline<=15 &&
    s->queue_target==((s->queue_baseline+6)&15U) && s->abort==0x100000U;
}
static int guard(struct nano_probe_setup *s)
{
    uint64_t now=0;
    if (s->failed) return s->first_error;
    if(!integrity(s))return fail(s,-13);
    if(s->input_selected>1 || (!!s->input_selected)!=(!!s->input_scope))return fail(s,-13);
    if (s->trusted_io.time_us(s->trusted_io.context,&now)) return fail(s,-5);
    if(!integrity(s))return fail(s,-13);
    if (s->trusted_io.owns(s->trusted_io.context,&s->binding)!=1 || !integrity(s)) return fail(s,-13);
    if(s->input_selected && (!s->input_scope || nano_sdm_input_validate(&s->input) ||
       s->input.owner!=s->binding.owner || s->input.epoch!=s->binding.epoch || s->input.generation!=s->binding.generation ||
       s->input_scope(s->io.context,&s->input)!=1 || !integrity(s)))return fail(s,-13);
    if(s->warm && (s->warm_scope(s->io.context,s->previous,&s->input)!=1 || !integrity(s)))return fail(s,-13);
    if (!s->timed) {s->timed=1;s->start=s->last=now;}
    if (now<s->last || now-s->start>NANO_PROBE_BOUND_US) return fail(s,-110);
    s->last=now;return 0;
}
static int rd(struct nano_probe_setup *s,uint32_t offset,uint32_t *value)
{
    if (guard(s)) return s->first_error;
    s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);s->reads++;
    int result=s->trusted_io.read32(s->trusted_io.context,offset,value);
    if(!integrity(s))return fail(s,-13);
    s->last_kind=1;s->last_offset=offset;s->last_value=result ? 0 : *value;s->last_callback_status=result;
    s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
    return result ? fail(s,result) : guard(s);
}
static int equal(struct nano_probe_setup *s,uint32_t offset,uint32_t want)
{
    uint32_t value=0;
    return rd(s,offset,&value) || (value!=want ? fail(s,-5) : 0);
}
static int wr(struct nano_probe_setup *s,uint32_t offset,uint32_t value)
{
    if (guard(s)) return s->first_error;
    s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);s->writes++;
    int result=s->trusted_io.write32(s->trusted_io.context,offset,value);
    if(!integrity(s))return fail(s,-13);
    s->last_kind=2;s->last_offset=offset;s->last_value=value;s->last_callback_status=result;
    s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
    return result ? fail(s,result) : guard(s);
}
static int clear(struct nano_probe_setup *s,uint32_t offset,uint32_t bytes)
{
    if (guard(s)) return s->first_error;
    s->clears++;int result=s->trusted_io.clear(s->trusted_io.context,offset,bytes);
    if(!integrity(s))return fail(s,-13);
    s->last_kind=3;s->last_offset=offset;s->last_value=bytes;s->last_callback_status=result;
    s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
    return result ? fail(s,result) : guard(s);
}
static int controls(struct nano_probe_setup *s,uint32_t control)
{
    return equal(s,0x20,control) || equal(s,0x24,control) ||
        equal(s,0x60,0) || equal(s,0x64,0) || equal(s,0x50,0) || equal(s,0x54,0) ||
        equal(s,0x44,s->abort) || equal(s,0x48,0);
}
static int identity(struct nano_probe_setup *s)
{
    /* Exact observed Nano diagnostic target, not a generic inferred layout.
     * HYP support is required for the six-command invalidation recipe. */
    return equal(s,0,0x080f7e3f) || equal(s,4,0x0e739d18) ||
        equal(s,0x14,0x00400075) || equal(s,0x2c,4);
}
static int control(struct nano_probe_setup *s,uint32_t value)
{
    if (wr(s,0x20,value)) return s->first_error;
    for (unsigned i=0;i<128;i++) {
        uint32_t ack=0;s->ack_polls++;
        if (equal(s,0x20,value) || rd(s,0x24,&ack)) return s->first_error;
        if (ack==value) return controls(s,value);
        uint32_t previous=value==8 ? 0U : value==12 ? 8U : 12U;
        if (ack!=previous) return fail(s,-5);
    }
    return fail(s,-110);
}
static int layout(struct nano_probe_setup *s,uint32_t value,uint32_t pointer)
{
    return identity(s) || controls(s,value) || equal(s,0x28,0x820) ||
        equal(s,0x80,(uint32_t)NANO_PROBE_BASE) || equal(s,0x84,0) || equal(s,0x88,8) ||
        equal(s,0x90,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_CMDQ)|3U) || equal(s,0x94,0) ||
        equal(s,0x98,pointer) || equal(s,0x9c,pointer) ||
        equal(s,0xa0,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_EVTQ)|3U) || equal(s,0xa4,0) ||
        equal(s,0x100a8,0) || equal(s,0x100ac,0);
}
static int configured(struct nano_probe_setup *s,uint32_t value)
{return layout(s,value,s->queue_target);}
static uint64_t descriptor(unsigned word)
{
    if (!word) return 13; /* valid, stage 2 only */
    if (word==1) return UINT64_C(1)<<44; /* observed ATTR_TYPES_OVR */
    if (word==2) return 1U | (UINT64_C(0x2060)<<32) |
        (UINT64_C(1)<<51) | (UINT64_C(1)<<54) | (UINT64_C(1)<<58);
    return NANO_PROBE_BASE+NANO_PROBE_ROOT;
}
static uint64_t table_word(struct nano_probe_setup *s,uint32_t offset)
{
    if(s->input_selected){
        uint64_t value=0;
        int error=nano_sdm_input_words(&s->input,offset,&value,1);
        if(error)fail(s,error);
        return value;
    }

    if (offset>=10*64 && offset<10*64+32) return descriptor((offset-10*64)/8);
    return offset<0x4000 && offset%64==0 ? 1U : 0U;
}
static int memory_block(struct nano_probe_setup *s,uint32_t offset,unsigned words,unsigned writing)
{
    uint64_t values[NANO_PROBE_MEMORY_WORDS];
    if (!words || words>NANO_PROBE_MEMORY_WORDS || offset%8 ||
        !(((s->input_selected ? nano_sdm_input_table_range(offset,words*8) :
           (offset<0x5000 && words*8<=0x5000-offset))) ||
          (!writing && offset>=NANO_PROBE_CMDQ && offset<NANO_PROBE_CMDQ+128 && words*8<=NANO_PROBE_CMDQ+128-offset) ||
          (!writing && offset>=NANO_PROBE_EVTQ && offset<NANO_PROBE_EVTQ+256 && words*8<=NANO_PROBE_EVTQ+256-offset)) ||
        !s->preparing || s->prepared || s->arming || s->armed) return fail(s,-22);
    if (guard(s)) return s->first_error;
    s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
    int result;
    if (writing) {
        for (unsigned n=0;n<words;n++) values[n]=table_word(s,offset+n*8);
        if(s->failed)return s->first_error;
        s->stores+=words;result=s->trusted_io.store_block(s->trusted_io.context,offset,values,words);
    } else {
        s->loads+=words;result=s->trusted_io.load_block(s->trusted_io.context,offset,values,words);
    }
    if(!integrity(s))return fail(s,-13);
    s->last_kind=writing ? 4 : 5;s->last_offset=offset;
    s->last_value=result ? 0 : values[0];s->last_callback_status=result;
    s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
    if (result) return fail(s,result);
    if (guard(s)) return s->first_error;
    if (!writing) for (unsigned n=0;n<words;n++) {
        s->last_offset=offset+n*8;s->last_value=values[n];
        uint64_t want=table_word(s,offset+n*8);
        if(s->failed)return s->first_error;
        if (values[n]!=want) return fail(s,-5);
    }
    return 0;
}
static int separate(const struct nano_probe_setup *s,const struct nano_probe_receipt *out)
{
    uintptr_t a=(uintptr_t)s,b=(uintptr_t)out;
    return out && a<=UINTPTR_MAX-sizeof(*s) && b<=UINTPTR_MAX-sizeof(*out) &&
        (a+sizeof(*s)<=b || b+sizeof(*out)<=a);
}
static void receipt(const struct nano_probe_setup *s,struct nano_probe_receipt *out,uint32_t value)
{
    struct nano_probe_receipt result={.owner=s->binding.owner,.epoch=s->binding.epoch,.generation=s->binding.generation,
        .control=value,.commands=6,.queues_prepared=1,.translation_fault_only=value==13 && !s->input_selected,
        .DMA_access_granted=value==13 && s->input_selected,
        .input_tables_prepared=s->input_selected,.input_pages=s->input_selected ? (s->input.bytes+4095U)/4096U:0};
    *out=result;
}
static int cmd_owns(void *context,const struct nano_cmdq_binding *b)
{
    struct nano_probe_setup *s=context;
    return !guard(s) && b->owner==s->binding.owner && b->epoch==s->binding.epoch &&
        b->generation==s->binding.generation && b->queue==NANO_PROBE_BASE+NANO_PROBE_CMDQ && b->baseline==s->queue_baseline;
}
/* Queue guard supplies the fresh before/after proof for these callbacks.
 * Do not recursively enter the probe guard inside an already guarded I/O.
 * Only the fixed unpublished 16-byte slot callback groups queue memory words.
 * MMIO callbacks each perform exactly one whitelisted register operation. */
static int cmd_time(void *context,uint64_t *value)
{
    struct nano_probe_setup *s=context;
    if(!s || !value || !integrity(s))return -1;
    int error=s->trusted_io.time_us(s->trusted_io.context,value);if(error)return error;
    if(!integrity(s))return fail(s,-13);
    /* The nested queue's first clock sample must belong to its parent's
     * already-started monotonic lifetime. Never silently establish a fresh
     * clock origin after a backwards observation or outside the1s bound. */
    if(s->timed && *value<s->last)return -5;
    if(s->timed && *value-s->start>NANO_PROBE_BOUND_US)return -110;
    return 0;
}
static int cmd_context(struct nano_probe_setup *s)
{
    return s && integrity(s) && s->initialized && s->preparing && !s->prepared && !s->arming && !s->armed && !s->failed &&
        s->command.initialized && s->command.submitted && !s->command.failed;
}
static int cmd_read(void *context,uint32_t offset,uint32_t *value)
{
    struct nano_probe_setup *s=context;
    if (!cmd_context(s) || !value) return -22;
    switch(offset) {case 0x20:case 0x24:case 0x60:case 0x64:case 0x90:case 0x94:case 0x98:case 0x9c:break;default:return -22;}
    s->reads++;int result=s->trusted_io.read32(s->trusted_io.context,offset,value);
    if(!integrity(s))return fail(s,-13);
    s->last_kind=1;s->last_offset=offset;s->last_value=result ? 0 : *value;s->last_callback_status=result;
    return result;
}
static int cmd_write(void *context,uint32_t offset,uint32_t value)
{
    struct nano_probe_setup *s=context;
    if (!cmd_context(s) || offset!=0x98 || value!=s->queue_target) return -22;
    s->writes++;int result=s->trusted_io.write32(s->trusted_io.context,offset,value);
    if(!integrity(s))return fail(s,-13);
    s->last_kind=2;s->last_offset=offset;s->last_value=value;s->last_callback_status=result;
    return result;
}
static void cmd_barrier(void *context) {struct nano_probe_setup *s=context;if(!integrity(s)){fail(s,-13);return;}s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))fail(s,-13);}
static int cmd_store(void *context,unsigned slot,const uint8_t bytes[16])
{
    struct nano_probe_setup *s=context;
    if (!cmd_context(s) || !s->command.slots_attempted || s->command.slots_attempted>6 ||
        slot!=((s->queue_baseline+s->command.slots_attempted-1)&7U) || !bytes || s->command.publish_attempted) return -22;
    for (unsigned word=0;word<2;word++) {
        uint64_t value=0,observed=0;
        for (unsigned n=0;n<8;n++) value|=(uint64_t)bytes[word*8+n]<<(n*8);
        uint32_t offset=NANO_PROBE_CMDQ+slot*16+word*8;
        s->stores++;int result=s->trusted_io.store64(s->trusted_io.context,offset,value);
        if(!integrity(s))return fail(s,-13);
    s->last_kind=4;s->last_offset=offset;s->last_value=value;s->last_callback_status=result;
        s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
        if(result)return result;
        s->loads++;result=s->trusted_io.load64(s->trusted_io.context,offset,&observed);
        if(!integrity(s))return fail(s,-13);
    s->last_kind=5;s->last_value=result ? 0 : observed;s->last_callback_status=result;
        s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
        if(result || value!=observed)return result ? result : -5;
    }
    return 0;
}
static int initialize(struct nano_probe_setup *s,struct nano_probe_binding b,struct nano_probe_io io,
    const struct nano_sdm_input_binding *input,int (*source_scope)(void *,const struct nano_sdm_input_binding *))
{
    if (!s || s->attempted) return -22;
    s->attempted=1;s->binding=b;s->io=io;
    if (!b.owner || !b.epoch || !b.generation || b.generation==UINT64_MAX ||
        !io.owns || !io.time_us || !io.read32 || !io.write32 || !io.clear ||
        !io.store64 || !io.load64 || !io.store_block || !io.load_block || !io.barrier || s->initialized || s->preparing || s->prepared ||
        s->arming || s->armed || s->failed || s->timed) return fail(s,-22);
    if(s->input_selected || s->input_scope || s->warm || s->previous || s->pinned_previous ||
       s->warm_scope || s->trusted_warm_scope || s->queue_baseline || s->queue_target || s->abort)return fail(s,-22);
    if(input){
        if(!io.context || !source_scope || nano_sdm_input_validate(input) || input->owner!=b.owner ||
           input->epoch!=b.epoch || input->generation!=b.generation)return fail(s,-22);
        s->input=*input;s->input_scope=source_scope;s->input_selected=1;
    }else if(source_scope)return fail(s,-22);
    s->queue_target=6;s->trusted_io=io;s->trusted_binding=b;s->trusted_input=s->input;
    s->trusted_input_scope=s->input_scope;
    s->initialized=1;return 0;
}
int nano_probe_init(struct nano_probe_setup *s,struct nano_probe_binding b,struct nano_probe_io io)
{return initialize(s,b,io,0,0);}
int nano_probe_init_input(struct nano_probe_setup *s,struct nano_probe_binding b,struct nano_probe_io io,
    const struct nano_sdm_input_binding *input,int (*source_scope)(void *,const struct nano_sdm_input_binding *))
{
    uintptr_t first=(uintptr_t)s,source=(uintptr_t)input;
    if(!s || !input || !source_scope || first>UINTPTR_MAX-sizeof(*s) || source>UINTPTR_MAX-sizeof(*input) ||
       (first<source+sizeof(*input) && source<first+sizeof(*s))){
        if(s && !s->attempted){s->attempted=1;return fail(s,-22);}return -22;
    }
    return initialize(s,b,io,input,source_scope);
}
static int previous_valid(const struct nano_retire *r,const struct nano_sdm_input_binding *input)
{
 const struct nano_cmdq_receipt *q=&r->command_receipt;
 return r->attempted==1 && r->retired==1 && r->continued==1 && r->continuation_attempted==1 &&
  r->phase==RET_COMPLETE && !r->failed && !r->first_error && r->timed==1 &&
  r->binding.owner==input->owner && r->binding.epoch==input->epoch &&
  r->binding.generation<input->generation && !nano_sdm_input_validate(&r->binding) &&
  r->prepared_engine && r->io.prepared==r->prepared_engine &&
  !memcmp(r->prepared_engine,&r->prepared_snapshot,sizeof(r->prepared_snapshot)) &&
  r->commands.initialized==1 && r->commands.submitted==1 && !r->commands.failed &&
  !r->commands.first_error && r->commands.stage==CMDQ_COMPLETE && r->commands.completion_seen==1 &&
  r->commands.binding.owner==input->owner && r->commands.binding.epoch==input->epoch &&
  r->commands.binding.generation==r->binding.generation && r->commands.binding.queue==NANO_PROBE_BASE+NANO_PROBE_CMDQ &&
  r->commands.binding.baseline==r->queue_baseline && r->queue_baseline<=15 &&
  r->queue_target==((r->queue_baseline+6)&15U) && q->baseline==r->queue_baseline &&
  q->owner==input->owner && q->epoch==input->epoch && q->generation==r->binding.generation &&
  q->producer==r->queue_target && q->consumer==r->queue_target && q->commands_completed==1 &&
  !q->DMA_access_granted && !q->interconnect_drained;
}
int nano_probe_init_rearmed_input(struct nano_probe_setup *s,struct nano_probe_binding b,struct nano_probe_io io,
 const struct nano_sdm_input_binding *input,int (*source_scope)(void *,const struct nano_sdm_input_binding *),
 const struct nano_retire *previous,int (*released)(void *,const struct nano_retire *,const struct nano_sdm_input_binding *))
{
 if(nano_probe_init_input(s,b,io,input,source_scope))return -22;
 uintptr_t a=(uintptr_t)s,p=(uintptr_t)previous;
 if(!previous || !released || a>UINTPTR_MAX-sizeof(*s) || p>UINTPTR_MAX-sizeof(*previous) ||
    (a<p+sizeof(*previous) && p<a+sizeof(*s)) || !previous_valid(previous,input))return fail(s,-22);
 s->previous=s->pinned_previous=previous;memcpy(&s->previous_snapshot,previous,sizeof(*previous));
 s->warm_scope=s->trusted_warm_scope=released;s->warm=1;
 s->queue_baseline=previous->queue_target;s->queue_target=(s->queue_baseline+6)&15U;s->abort=0x100000;
 return 0;
}
static int previous_tables(struct nano_probe_setup *s)
{
 uint64_t values[NANO_PROBE_MEMORY_WORDS];
 for(uint32_t at=0;at<0xc000;at+=sizeof(values)){
  if(!nano_sdm_input_table_range(at,sizeof(values)))continue;
  if(guard(s))return s->first_error;
  s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
  s->loads+=NANO_PROBE_MEMORY_WORDS;
  int error=s->trusted_io.load_block(s->trusted_io.context,at,values,NANO_PROBE_MEMORY_WORDS);
  if(!integrity(s))return fail(s,-13);
  s->trusted_io.barrier(s->trusted_io.context);if(!integrity(s))return fail(s,-13);
  if(error)return fail(s,error);
  if(guard(s))return s->first_error;
  for(unsigned n=0;n<NANO_PROBE_MEMORY_WORDS;n++){
   uint64_t expected=0;
   if(nano_sdm_input_words(&s->previous_snapshot.binding,at+n*8,&expected,1) || values[n]!=expected)return fail(s,-5);
  }
 }
 return 0;
}
int nano_probe_prepare(struct nano_probe_setup *s,struct nano_probe_receipt *out)
{
    if (!s || !s->initialized || s->preparing || s->failed) return -22;
    s->preparing=1;
    if (!separate(s,out)) return fail(s,-22);
    for (unsigned n=0;n<2;n++) {
        if(s->warm){if(layout(s,0,s->queue_baseline))return s->first_error;}
        else if (identity(s) || controls(s,0) || equal(s,0x28,0) || equal(s,0x98,0) || equal(s,0x9c,0) ||
            equal(s,0x100a8,0) || equal(s,0x100ac,0)) return s->first_error;
    }
    if(s->warm && previous_tables(s))return s->first_error;
    uint32_t first_end=s->input_selected ? 0x8000U:0x5000U;
    if (!s->warm && (clear(s,0,first_end) || (s->input_selected && clear(s,0xa000,0x2000)) ||
        clear(s,NANO_PROBE_CMDQ,128) || clear(s,NANO_PROBE_EVTQ,256)))return s->first_error;
    for(unsigned writing=1;writing<3;writing++){
        for(unsigned offset=0;offset<first_end;offset+=NANO_PROBE_MEMORY_WORDS*8)
            if(memory_block(s,offset,NANO_PROBE_MEMORY_WORDS,writing==1))return s->first_error;
        if(s->input_selected)for(unsigned offset=0xa000;offset<0xc000;offset+=NANO_PROBE_MEMORY_WORDS*8)
            if(memory_block(s,offset,NANO_PROBE_MEMORY_WORDS,writing==1))return s->first_error;
    }
    if (!s->warm && (memory_block(s,NANO_PROBE_CMDQ,16,0) || memory_block(s,NANO_PROBE_EVTQ,32,0))) return s->first_error;
    if (!s->warm && (identity(s) || controls(s,0) || wr(s,0x28,0x820) ||
        wr(s,0x80,(uint32_t)NANO_PROBE_BASE) || wr(s,0x84,0) || wr(s,0x88,8) ||
        wr(s,0x90,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_CMDQ)|3U) || wr(s,0x94,0) ||
        equal(s,0x28,0x820) || equal(s,0x80,(uint32_t)NANO_PROBE_BASE) || equal(s,0x84,0) ||
        equal(s,0x88,8) || equal(s,0x90,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_CMDQ)|3U) ||
        equal(s,0x94,0))) return s->first_error;
    if(control(s,8))return s->first_error;
    struct nano_cmdq_binding b={s->binding.owner,s->binding.epoch,s->binding.generation,NANO_PROBE_BASE+NANO_PROBE_CMDQ,s->queue_baseline};
    struct nano_cmdq_io io={s,cmd_owns,cmd_time,cmd_read,cmd_write,cmd_store,cmd_barrier,0,0};
    if (nano_cmdq_init(&s->command,b,io) || nano_cmdq_submit(&s->command,&s->command_receipt,CMDQ_FULL_OPERATION_BOUND)) return fail(s,-5);
    if (!s->warm && (wr(s,0xa0,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_EVTQ)|3U) || wr(s,0xa4,0)))return s->first_error;
    if (
        equal(s,0x100a8,0) || equal(s,0x100ac,0) || control(s,12) ||
        configured(s,12) || configured(s,12) || guard(s)) return s->first_error;
    s->prepared=1;receipt(s,out,12);return 0;
}
int nano_probe_arm(struct nano_probe_setup *s,struct nano_probe_receipt *out)
{
    if (!s || !s->prepared || s->arming || s->failed) return -22;
    s->arming=1;
    if (!separate(s,out)) return fail(s,-22);
    if (configured(s,12) || configured(s,12) || control(s,13) ||
        configured(s,13) || configured(s,13) || guard(s)) return s->first_error;
    s->armed=1;receipt(s,out,13);return 0;
}
