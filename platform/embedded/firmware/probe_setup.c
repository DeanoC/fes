/* SPDX-License-Identifier: BSD-3-Clause */
#include "probe_setup.h"
static int fail(struct nano_probe_setup *s,int error)
{
    s->failed=1;
    if (!s->first_error) s->first_error=error<0 ? error : -1;
    return s->first_error;
}
static int guard(struct nano_probe_setup *s)
{
    uint64_t now=0;
    if (s->failed) return s->first_error;
    if (s->io.time_us(s->io.context,&now)) return fail(s,-5);
    if (s->io.owns(s->io.context,&s->binding)!=1) return fail(s,-13);
    if (!s->timed) {s->timed=1;s->start=s->last=now;}
    if (now<s->last || now-s->start>NANO_PROBE_BOUND_US) return fail(s,-110);
    s->last=now;return 0;
}
static int rd(struct nano_probe_setup *s,uint32_t offset,uint32_t *value)
{
    if (guard(s)) return s->first_error;
    s->io.barrier(s->io.context);s->reads++;
    int result=s->io.read32(s->io.context,offset,value);
    s->last_kind=1;s->last_offset=offset;s->last_value=result ? 0 : *value;s->last_callback_status=result;
    s->io.barrier(s->io.context);
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
    s->io.barrier(s->io.context);s->writes++;
    int result=s->io.write32(s->io.context,offset,value);
    s->last_kind=2;s->last_offset=offset;s->last_value=value;s->last_callback_status=result;
    s->io.barrier(s->io.context);
    return result ? fail(s,result) : guard(s);
}
static int clear(struct nano_probe_setup *s,uint32_t offset,uint32_t bytes)
{
    if (guard(s)) return s->first_error;
    s->clears++;int result=s->io.clear(s->io.context,offset,bytes);
    s->last_kind=3;s->last_offset=offset;s->last_value=bytes;s->last_callback_status=result;
    s->io.barrier(s->io.context);
    return result ? fail(s,result) : guard(s);
}
static int controls(struct nano_probe_setup *s,uint32_t control)
{
    return equal(s,0x20,control) || equal(s,0x24,control) ||
        equal(s,0x60,0) || equal(s,0x64,0) || equal(s,0x50,0) || equal(s,0x54,0) ||
        equal(s,0x44,0) || equal(s,0x48,0);
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
static int configured(struct nano_probe_setup *s,uint32_t value)
{
    return identity(s) || controls(s,value) || equal(s,0x28,0x820) ||
        equal(s,0x80,(uint32_t)NANO_PROBE_BASE) || equal(s,0x84,0) || equal(s,0x88,8) ||
        equal(s,0x90,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_CMDQ)|3U) || equal(s,0x94,0) ||
        equal(s,0x98,6) || equal(s,0x9c,6) ||
        equal(s,0xa0,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_EVTQ)|3U) || equal(s,0xa4,0) ||
        equal(s,0x100a8,0) || equal(s,0x100ac,0);
}
static uint64_t descriptor(unsigned word)
{
    if (!word) return 13; /* valid, stage 2 only */
    if (word==1) return UINT64_C(1)<<44; /* observed ATTR_TYPES_OVR */
    if (word==2) return 1U | (UINT64_C(0x2060)<<32) |
        (UINT64_C(1)<<51) | (UINT64_C(1)<<54) | (UINT64_C(1)<<58);
    return NANO_PROBE_BASE+NANO_PROBE_ROOT;
}
static uint64_t table_word(uint32_t offset)
{
    if (offset>=10*64 && offset<10*64+32) return descriptor((offset-10*64)/8);
    return offset<0x4000 && offset%64==0 ? 1U : 0U;
}
static int memory_block(struct nano_probe_setup *s,uint32_t offset,unsigned words,unsigned writing)
{
    uint64_t values[NANO_PROBE_MEMORY_WORDS];
    if (!words || words>NANO_PROBE_MEMORY_WORDS || offset%8 ||
        !((offset<0x5000 && words*8<=0x5000-offset) ||
          (!writing && offset>=NANO_PROBE_CMDQ && offset<NANO_PROBE_CMDQ+128 && words*8<=NANO_PROBE_CMDQ+128-offset) ||
          (!writing && offset>=NANO_PROBE_EVTQ && offset<NANO_PROBE_EVTQ+256 && words*8<=NANO_PROBE_EVTQ+256-offset)) ||
        !s->preparing || s->prepared || s->arming || s->armed) return fail(s,-22);
    if (guard(s)) return s->first_error;
    s->io.barrier(s->io.context);
    int result;
    if (writing) {
        for (unsigned n=0;n<words;n++) values[n]=table_word(offset+n*8);
        s->stores+=words;result=s->io.store_block(s->io.context,offset,values,words);
    } else {
        s->loads+=words;result=s->io.load_block(s->io.context,offset,values,words);
    }
    s->last_kind=writing ? 4 : 5;s->last_offset=offset;
    s->last_value=result ? 0 : values[0];s->last_callback_status=result;
    s->io.barrier(s->io.context);
    if (result) return fail(s,result);
    if (guard(s)) return s->first_error;
    if (!writing) for (unsigned n=0;n<words;n++) {
        s->last_offset=offset+n*8;s->last_value=values[n];
        uint64_t want=offset<0x5000 ? table_word(offset+n*8) : 0;
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
    struct nano_probe_receipt result={s->binding.owner,s->binding.epoch,s->binding.generation,
        value,6,1,value==13,0,0};
    *out=result;
}
static int cmd_owns(void *context,const struct nano_cmdq_binding *b)
{
    struct nano_probe_setup *s=context;
    return !guard(s) && b->owner==s->binding.owner && b->epoch==s->binding.epoch &&
        b->generation==s->binding.generation && b->queue==NANO_PROBE_BASE+NANO_PROBE_CMDQ && !b->baseline;
}
/* Queue guard supplies the fresh before/after proof for these callbacks.
 * Do not recursively enter the probe guard inside an already guarded I/O.
 * Only the fixed unpublished 16-byte slot callback groups queue memory words.
 * MMIO callbacks each perform exactly one whitelisted register operation. */
static int cmd_time(void *context,uint64_t *value)
{
    struct nano_probe_setup *s=context;
    return !s || !value ? -1 : s->io.time_us(s->io.context,value);
}
static int cmd_context(struct nano_probe_setup *s)
{
    return s && s->initialized && s->preparing && !s->prepared && !s->arming && !s->armed && !s->failed &&
        s->command.initialized && s->command.submitted && !s->command.failed;
}
static int cmd_read(void *context,uint32_t offset,uint32_t *value)
{
    struct nano_probe_setup *s=context;
    if (!cmd_context(s) || !value) return -22;
    switch(offset) {case 0x20:case 0x24:case 0x60:case 0x64:case 0x90:case 0x94:case 0x98:case 0x9c:break;default:return -22;}
    s->reads++;int result=s->io.read32(s->io.context,offset,value);
    s->last_kind=1;s->last_offset=offset;s->last_value=result ? 0 : *value;s->last_callback_status=result;
    return result;
}
static int cmd_write(void *context,uint32_t offset,uint32_t value)
{
    struct nano_probe_setup *s=context;
    if (!cmd_context(s) || offset!=0x98 || value!=6) return -22;
    s->writes++;int result=s->io.write32(s->io.context,offset,value);
    s->last_kind=2;s->last_offset=offset;s->last_value=value;s->last_callback_status=result;
    return result;
}
static void cmd_barrier(void *context) {struct nano_probe_setup *s=context;s->io.barrier(s->io.context);}
static int cmd_store(void *context,unsigned slot,const uint8_t bytes[16])
{
    struct nano_probe_setup *s=context;
    if (!cmd_context(s) || slot>=6 || !bytes || s->command.publish_attempted) return -22;
    for (unsigned word=0;word<2;word++) {
        uint64_t value=0,observed=0;
        for (unsigned n=0;n<8;n++) value|=(uint64_t)bytes[word*8+n]<<(n*8);
        uint32_t offset=NANO_PROBE_CMDQ+slot*16+word*8;
        s->stores++;int result=s->io.store64(s->io.context,offset,value);
        s->last_kind=4;s->last_offset=offset;s->last_value=value;s->last_callback_status=result;
        s->io.barrier(s->io.context);
        if(result)return result;
        s->loads++;result=s->io.load64(s->io.context,offset,&observed);
        s->last_kind=5;s->last_value=result ? 0 : observed;s->last_callback_status=result;
        s->io.barrier(s->io.context);
        if(result || value!=observed)return result ? result : -5;
    }
    return 0;
}
int nano_probe_init(struct nano_probe_setup *s,struct nano_probe_binding b,struct nano_probe_io io)
{
    if (!s || s->attempted) return -22;
    s->attempted=1;s->binding=b;s->io=io;
    if (!b.owner || !b.epoch || !b.generation || b.generation==UINT64_MAX ||
        !io.owns || !io.time_us || !io.read32 || !io.write32 || !io.clear ||
        !io.store64 || !io.load64 || !io.store_block || !io.load_block || !io.barrier || s->initialized || s->preparing || s->prepared ||
        s->arming || s->armed || s->failed || s->timed) return fail(s,-22);
    s->initialized=1;return 0;
}
int nano_probe_prepare(struct nano_probe_setup *s,struct nano_probe_receipt *out)
{
    if (!s || !s->initialized || s->preparing || s->failed) return -22;
    s->preparing=1;
    if (!separate(s,out)) return fail(s,-22);
    for (unsigned n=0;n<2;n++)
        if (identity(s) || controls(s,0) || equal(s,0x28,0) || equal(s,0x98,0) || equal(s,0x9c,0) ||
            equal(s,0x100a8,0) || equal(s,0x100ac,0)) return s->first_error;
    if (clear(s,0,0x5000) || clear(s,NANO_PROBE_CMDQ,128) || clear(s,NANO_PROBE_EVTQ,256)) return s->first_error;
    for (unsigned offset=0;offset<0x5000;offset+=NANO_PROBE_MEMORY_WORDS*8)
        if (memory_block(s,offset,NANO_PROBE_MEMORY_WORDS,1)) return s->first_error;
    for (unsigned offset=0;offset<0x5000;offset+=NANO_PROBE_MEMORY_WORDS*8)
        if (memory_block(s,offset,NANO_PROBE_MEMORY_WORDS,0)) return s->first_error;
    if (memory_block(s,NANO_PROBE_CMDQ,16,0) || memory_block(s,NANO_PROBE_EVTQ,32,0)) return s->first_error;
    if (identity(s) || controls(s,0) || wr(s,0x28,0x820) ||
        wr(s,0x80,(uint32_t)NANO_PROBE_BASE) || wr(s,0x84,0) || wr(s,0x88,8) ||
        wr(s,0x90,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_CMDQ)|3U) || wr(s,0x94,0) ||
        equal(s,0x28,0x820) || equal(s,0x80,(uint32_t)NANO_PROBE_BASE) || equal(s,0x84,0) ||
        equal(s,0x88,8) || equal(s,0x90,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_CMDQ)|3U) ||
        equal(s,0x94,0) || control(s,8)) return s->first_error;
    struct nano_cmdq_binding b={s->binding.owner,s->binding.epoch,s->binding.generation,NANO_PROBE_BASE+NANO_PROBE_CMDQ,0};
    struct nano_cmdq_io io={s,cmd_owns,cmd_time,cmd_read,cmd_write,cmd_store,cmd_barrier,0,0};
    if (nano_cmdq_init(&s->command,b,io) || nano_cmdq_submit(&s->command,&s->command_receipt)) return fail(s,-5);
    if (wr(s,0xa0,(uint32_t)(NANO_PROBE_BASE+NANO_PROBE_EVTQ)|3U) || wr(s,0xa4,0) ||
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
