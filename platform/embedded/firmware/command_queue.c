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
static int guard(struct nano_cmdq *s)
{
    uint64_t now=0;
    if (s->failed) return -1;
    if (s->io.time_us(s->io.context,&now)) return fail(s,-2);
    if (s->io.owns(s->io.context,&s->binding)!=1) return fail(s,-1);
    if (!s->timed) {s->start=s->last=now;s->timed=1;}
    if (now<s->last || now-s->start>NANO_CMDQ_BOUND_US) return fail(s,-3);
    s->last=now;
    return 0;
}
static int read32(struct nano_cmdq *s,uint32_t offset,uint32_t *value)
{
    if (guard(s)) return -1;
    s->io.barrier(s->io.context);
    s->reads++;
    int error=s->io.read32(s->io.context,offset,value);
    s->io.barrier(s->io.context);
    if (error) return fail(s,error);
    return guard(s);
}
static int checked(struct nano_cmdq *s,uint32_t offset,uint32_t want)
{
    uint32_t got=0;
    return read32(s,offset,&got) || (got!=want ? fail(s,-4) : 0);
}
static int snapshot(struct nano_cmdq *s,uint32_t pointer)
{
    return checked(s,CR0,8) || checked(s,CR0ACK,8) ||
        checked(s,BASE,(uint32_t)s->binding.queue|3U) ||
        checked(s,BASEHI,(uint32_t)(s->binding.queue>>32)) ||
        checked(s,GERROR,0) || checked(s,GERRORN,0) ||
        checked(s,PROD,pointer) || checked(s,CONS,pointer);
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
int nano_cmdq_submit(struct nano_cmdq *s,struct nano_cmdq_receipt *out)
{
    if (!s || !s->initialized || s->submitted || s->failed) return -1;
    s->submitted=1; /* consume before any callback */
    if (!out) return fail(s,-22);
    uintptr_t a=(uintptr_t)s,b=(uintptr_t)out;
    if (a>UINTPTR_MAX-sizeof(*s) || b>UINTPTR_MAX-sizeof(*out) ||
        (a<b+sizeof(*out) && b<a+sizeof(*s))) return fail(s,-22);
    if (snapshot(s,s->binding.baseline) || snapshot(s,s->binding.baseline)) return -1;
    for (unsigned n=0;n<NANO_CMDQ_COMMANDS;n++) {
        uint8_t bytes[16];
        if (nano_cmdq_encode(n,bytes) || guard(s)) return -1;
        s->slots_attempted++;
        int error=s->io.store(s->io.context,(s->binding.baseline+n)&7U,bytes);
        s->io.barrier(s->io.context);
        if (error) return fail(s,error);
        if (guard(s)) return -1;
    }
    if (snapshot(s,s->binding.baseline) || guard(s)) return -1;
    uint32_t target=(s->binding.baseline+NANO_CMDQ_COMMANDS)&15U;
    s->io.barrier(s->io.context);s->publish_attempted=1;
    int error=s->io.write32(s->io.context,PROD,target);
    s->io.barrier(s->io.context);
    if (error) return fail(s,error);
    if (guard(s)) return -1;
    unsigned previous=0;
    for (unsigned i=0;i<NANO_CMDQ_POLLS;i++) {
        uint32_t cons=0;s->polls++;
        if (checked(s,GERROR,0) || checked(s,GERRORN,0) ||
            checked(s,PROD,target) || read32(s,CONS,&cons)) return -1;
        unsigned count=(cons-s->binding.baseline)&15U;
        if ((cons&~15U) || count>NANO_CMDQ_COMMANDS || count<previous) return fail(s,-5);
        previous=count;
        if (cons==target) {
            if (snapshot(s,target) || snapshot(s,target) || guard(s)) return -1;
            struct nano_cmdq_receipt receipt={
                .owner=s->binding.owner,.epoch=s->binding.epoch,.generation=s->binding.generation,
                .baseline=s->binding.baseline,.producer=target,.consumer=cons,.polls=s->polls,
                .commands_completed=1,.interconnect_drained=0,.DMA_access_granted=0};
            *out=receipt;
            return 0;
        }
    }
    return fail(s,-110);
}
