/* SPDX-License-Identifier: BSD-3-Clause */
#include "command_queue.h"
#define CR0 0x20U
#define CR0ACK 0x24U
#define GERROR 0x60U
#define GERRORN 0x64U
#define BASE 0x90U
#define BASEHI 0x94U
#define PROD 0x98U
#define CONS 0x9cU
static int fail(struct nano_cmdq *s,int error)
{
    s->failed=1;
    if (!s->first_error) s->first_error=error ? error : -1;
    return -1;
}
static int io_same(const struct nano_cmdq_io *a,const struct nano_cmdq_io *b)
{
    return a->context==b->context && a->owns==b->owns && a->time_us==b->time_us &&
        a->read32==b->read32 && a->write32==b->write32 && a->store==b->store &&
        a->barrier==b->barrier && a->snapshot==b->snapshot && a->progress==b->progress;
}
static int unchanged(const struct nano_cmdq *s,const struct nano_cmdq_io *io,
                     const struct nano_cmdq_binding *binding)
{
    const struct nano_cmdq_binding *b=&s->binding;
    return io_same(io,&s->io) && b->owner==binding->owner && b->epoch==binding->epoch &&
        b->generation==binding->generation && b->queue==binding->queue &&
        b->baseline==binding->baseline;
}
static int guard_bound(struct nano_cmdq *s,uint64_t bound)
{
    uint64_t now=0;
    if (s->failed) return -1;
    struct nano_cmdq_io io=s->io;
    const struct nano_cmdq_binding binding=s->binding;
    if (!io.time_us || !io.owns) return fail(s,-22);
    if (io.time_us(io.context,&now)) return fail(s,-2);
    s->observed=now;s->clock_samples++;
    if (!unchanged(s,&io,&binding)) return fail(s,-13);
    if (io.owns(io.context,&s->binding)!=1) return fail(s,-1);
    if (!unchanged(s,&io,&binding)) return fail(s,-13);
    if (!s->timed) {s->start=s->last=now;s->timed=1;}
    if (now<s->last || now-s->start>bound) return fail(s,-3);
    s->last=now;
    return 0;
}
static int guard(struct nano_cmdq *s)
{return guard_bound(s,NANO_CMDQ_BOUND_US);}
static int read_bound(struct nano_cmdq *s,uint32_t offset,uint32_t *value,uint64_t bound)
{
    struct nano_cmdq_io io=s->io;
    const struct nano_cmdq_binding binding=s->binding;
    if (guard_bound(s,bound) || !unchanged(s,&io,&binding)) return fail(s,-13);
    io.barrier(io.context);
    if (!unchanged(s,&io,&binding)) return fail(s,-13);
    s->reads++;
    int error=io.read32(io.context,offset,value);
    if (!unchanged(s,&io,&binding)) return fail(s,-13);
    io.barrier(io.context);
    if (!unchanged(s,&io,&binding)) return fail(s,-13);
    if (error) return fail(s,error);
    return guard_bound(s,bound);
}
static int read32(struct nano_cmdq *s,uint32_t offset,uint32_t *value)
{return read_bound(s,offset,value,NANO_CMDQ_BOUND_US);}
static int checked_bound(struct nano_cmdq *s,uint32_t offset,uint32_t want,uint64_t bound)
{
    uint32_t got=0;
    return read_bound(s,offset,&got,bound) || (got!=want ? fail(s,-4) : 0);
}
static int checked(struct nano_cmdq *s,uint32_t offset,uint32_t want)
{return checked_bound(s,offset,want,NANO_CMDQ_BOUND_US);}
static int snapshot(struct nano_cmdq *s,uint32_t pointer,uint64_t bound)
{
    if (s->io.snapshot) {
        struct nano_cmdq_io io=s->io;
        const struct nano_cmdq_binding binding=s->binding;
        if (guard_bound(s,bound) || !unchanged(s,&io,&binding)) return fail(s,-13);
        int error=io.snapshot(io.context,pointer);
        if (error) return fail(s,error);
        if (!unchanged(s,&io,&binding)) return fail(s,-13);
        return guard_bound(s,bound);
    }
    return checked_bound(s,CR0,8,bound) || checked_bound(s,CR0ACK,8,bound) ||
        checked_bound(s,BASE,(uint32_t)s->binding.queue|3U,bound) ||
        checked_bound(s,BASEHI,(uint32_t)(s->binding.queue>>32),bound) ||
        checked_bound(s,GERROR,0,bound) || checked_bound(s,GERRORN,0,bound) ||
        checked_bound(s,PROD,pointer,bound) || checked_bound(s,CONS,pointer,bound);
}
static int progress(struct nano_cmdq *s,uint32_t target,uint32_t *consumer)
{
    if (s->io.progress) {
        struct nano_cmdq_io io=s->io;
        const struct nano_cmdq_binding binding=s->binding;
        if (guard(s) || !unchanged(s,&io,&binding)) return fail(s,-13);
        uint32_t value=0;
        int error=io.progress(io.context,target,&value);
        if (error) return fail(s,error);
        if (!unchanged(s,&io,&binding)) return fail(s,-13);
        if (guard(s)) return -1;
        *consumer=value;
        return 0;
    }
    return checked(s,GERROR,0) || checked(s,GERRORN,0) ||
        checked(s,PROD,target) || read32(s,CONS,consumer);
}
int nano_cmdq_encode(unsigned index,uint8_t bytes[16])
{
    if (index>=NANO_CMDQ_COMMANDS || !bytes) return -1;
    /* CMD_SYNC: CS_NONE busy polling, ISH/OIWB, no MSI address/data.
     * Match the retained upstream builder and its CS_NONE conversion. */
    const uint64_t word0[6]={4,UINT64_C(0x0fc00046),0x20,UINT64_C(0x0fc00046),0x30,UINT64_C(0x0fc00046)};
    uint64_t word1=index==0 ? 31U : 0U;
    for (unsigned i=0;i<8;i++) {
        bytes[i]=(uint8_t)(word0[index]>>(8*i));
        bytes[8+i]=(uint8_t)(word1>>(8*i));
    }
    return 0;
}
int nano_cmdq_init(struct nano_cmdq *s,struct nano_cmdq_binding binding,struct nano_cmdq_io io)
{
    if (!s || s->attempted) return -1;
    s->attempted=1;s->binding=binding;s->io=io;
    if (!binding.owner || !binding.epoch || !binding.generation || binding.generation==UINT64_MAX ||
        binding.baseline>15 || binding.queue%NANO_CMDQ_BYTES || binding.queue<UINT64_C(0x80000000) ||
        binding.queue>UINT64_C(0xc0000000)-NANO_CMDQ_BYTES ||
        !io.owns || !io.time_us || !io.read32 || !io.write32 || !io.store || !io.barrier ||
        s->initialized || s->submitted || s->failed || s->timed || s->publish_attempted || s->slots_attempted)
        return fail(s,-22);
    s->initialized=1;
    return 0;
}
int nano_cmdq_submit(struct nano_cmdq *s,struct nano_cmdq_receipt *out,enum nano_cmdq_validation_policy policy)
{
    if (!s || !s->initialized || s->submitted || s->failed) return -1;
    s->submitted=1; /* consume before any callback */
    if (!out || (policy!=CMDQ_FULL_OPERATION_BOUND && policy!=CMDQ_RETAINED_FINAL_VALIDATION &&
                 policy!=CMDQ_PREPARATION_FINAL_VALIDATION)) return fail(s,-22);
    s->validation_policy=(unsigned)policy; /* observation; decisions use local policy */
    uintptr_t a=(uintptr_t)s,b=(uintptr_t)out;
    if (a>UINTPTR_MAX-sizeof(*s) || b>UINTPTR_MAX-sizeof(*out) ||
        (a<b+sizeof(*out) && b<a+sizeof(*s))) return fail(s,-22);
    s->stage=CMDQ_INITIAL_FIRST;
    if (snapshot(s,s->binding.baseline,NANO_CMDQ_BOUND_US)) return -1;
    s->stage=CMDQ_INITIAL_SECOND;
    if (snapshot(s,s->binding.baseline,NANO_CMDQ_BOUND_US)) return -1;
    s->stage=CMDQ_STORE;
    for (unsigned n=0;n<NANO_CMDQ_COMMANDS;n++) {
        uint8_t bytes[16];
        if (nano_cmdq_encode(n,bytes) || guard(s)) return -1;
        struct nano_cmdq_io io=s->io;
        const struct nano_cmdq_binding binding=s->binding;
        s->slots_attempted++;
        int error=io.store(io.context,(s->binding.baseline+n)&7U,bytes);
        if (!unchanged(s,&io,&binding)) return fail(s,-13);
        io.barrier(io.context);
        if (!unchanged(s,&io,&binding)) return fail(s,-13);
        if (error) return fail(s,error);
        if (guard(s)) return -1;
    }
    s->stage=CMDQ_PREPUBLICATION;
    if (snapshot(s,s->binding.baseline,NANO_CMDQ_BOUND_US) || guard(s)) return -1;
    uint32_t target=(s->binding.baseline+NANO_CMDQ_COMMANDS)&15U;
    s->stage=CMDQ_PUBLICATION;
    struct nano_cmdq_io io=s->io;
    const struct nano_cmdq_binding binding=s->binding;
    io.barrier(io.context);
    if (!unchanged(s,&io,&binding)) return fail(s,-13);
    s->publish_attempted=1;
    int error=io.write32(io.context,PROD,target);
    if (!unchanged(s,&io,&binding)) return fail(s,-13);
    io.barrier(io.context);
    if (!unchanged(s,&io,&binding)) return fail(s,-13);
    if (error) return fail(s,error);
    if (guard(s)) return -1;
    unsigned previous=0;
    for (unsigned i=0;i<NANO_CMDQ_POLLS;i++) {
        s->stage=CMDQ_PROGRESS;
        uint32_t cons=0;s->polls++;
        if (progress(s,target,&cons)) return -1;
        unsigned count=(cons-s->binding.baseline)&15U;
        if ((cons&~15U) || count>NANO_CMDQ_COMMANDS || count<previous) return fail(s,-5);
        previous=count;
        if (cons==target) {
            /* Completion was observed under the original 100 ms origin and
             * bound. Do not rebase that clock or relax the progress path.
             * Only an explicitly bounded preparation or retirement caller can select the enclosing
             * finite bound for the still-mandatory final observations. */
            s->completion_us=s->last;s->completion_seen=1;
            const uint64_t final_bound=policy==CMDQ_FULL_OPERATION_BOUND ? NANO_CMDQ_BOUND_US:NANO_CMDQ_FINAL_BOUND_US;
            s->stage=CMDQ_FINAL_FIRST;
            if (snapshot(s,target,final_bound)) return -1;
            s->stage=CMDQ_FINAL_SECOND;
            if (snapshot(s,target,final_bound)) return -1;
            s->stage=CMDQ_FINAL_GUARD;
            if (guard_bound(s,final_bound)) return -1;
            struct nano_cmdq_receipt receipt={
                .owner=s->binding.owner,.epoch=s->binding.epoch,.generation=s->binding.generation,
                .baseline=s->binding.baseline,.producer=target,.consumer=cons,.polls=s->polls,
                .commands_completed=1,.interconnect_drained=0,.DMA_access_granted=0};
            *out=receipt;
            s->stage=CMDQ_COMPLETE;
            return 0;
        }
    }
    return fail(s,-110);
}
