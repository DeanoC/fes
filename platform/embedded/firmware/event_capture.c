/* SPDX-License-Identifier: BSD-3-Clause */
#include "event_capture.h"
struct sample {uint32_t error,error_ack,control,control_ack,base_lo,base_hi,consumer,producer;};
static int fail(struct nano_evt_capture *s,int error)
{
    s->failed=1;
    if (!s->first_error) s->first_error=error<0 ? error : -5;
    return s->first_error;
}
static int guard(struct nano_evt_capture *s)
{
    if (s->failed) return s->first_error;
    return s->io.owns(s->io.context,&s->binding)==1 ? 0 : fail(s,-13);
}
static int rd(struct nano_evt_capture *s,uint32_t offset,uint32_t *value)
{
    if (guard(s)) return s->first_error;
    s->io.barrier(s->io.context);s->reads++;
    int error=s->io.read32(s->io.context,offset,value);
    s->io.barrier(s->io.context);
    return error ? fail(s,error) : guard(s);
}
static int sample(struct nano_evt_capture *s,struct sample *v)
{
    /* NS EVTQ producer/consumer are on page 1. The secure EVTQ has different
     * offsets and is deliberately unsupported; there is no page-0 fallback. */
    if (rd(s,0x60,&v->error) || rd(s,0x64,&v->error_ack) || rd(s,0x20,&v->control) ||
        rd(s,0x24,&v->control_ack) || rd(s,0xa0,&v->base_lo) || rd(s,0xa4,&v->base_hi) ||
        rd(s,0x100ac,&v->consumer) || rd(s,0x100a8,&v->producer)) return s->first_error;
    if (v->error || v->error_ack || v->control!=13 || v->control_ack!=13 ||
        v->base_lo!=((uint32_t)s->binding.queue|3U) || v->base_hi ||
        v->consumer!=s->binding.consumer || (v->producer&~15U) ||
        ((v->producer-v->consumer)&15U)>NANO_EVT_ENTRIES) return fail(s,-5);
    return 0;
}
static int separate(const void *a,size_t na,const void *b,size_t nb)
{
    uintptr_t x=(uintptr_t)a,y=(uintptr_t)b;
    return na<=UINTPTR_MAX-x && nb<=UINTPTR_MAX-y && (x+na<=y || y+nb<=x);
}
int nano_evt_init(struct nano_evt_capture *s,struct nano_evt_binding b,struct nano_evt_io io)
{
    if (!s) return -22;
    if (s->attempted) return -16;
    s->attempted=1;s->binding=b;s->io=io;
    if (!b.owner || !b.epoch || !b.generation || b.queue<0x80000000ULL ||
        b.queue>0xc0000000ULL-NANO_EVT_BYTES || (b.queue&(NANO_EVT_BYTES-1)) ||
        b.consumer>15 || !io.owns || !io.read32 || !io.copy_entry || !io.barrier ||
        s->initialized || s->captured || s->failed) return fail(s,-22);
    s->initialized=1;return 0;
}
int nano_evt_snapshot(struct nano_evt_capture *s,struct nano_evt_batch *out)
{
    if (!s || !out || !s->initialized || s->failed ||
        !separate(s,sizeof(*s),out,sizeof(*out))) return -22;
    if (s->captured) return -16;
    s->captured=1; /* Before the first ownership check or read. */
    struct sample a,b;
    if (sample(s,&a)) return s->first_error;
    struct nano_evt_batch result={.owner=s->binding.owner,.epoch=s->binding.epoch,
        .generation=s->binding.generation,.consumer=a.consumer,.producer=a.producer,
        .count=(a.producer-a.consumer)&15U};
    for (unsigned n=0;n<result.count;n++) {
        uint8_t bytes[32];
        if (guard(s)) return s->first_error;
        s->io.barrier(s->io.context);s->copies++;
        int error=s->io.copy_entry(s->io.context,(a.consumer+n)&7U,bytes);
        s->io.barrier(s->io.context);
        if (error) return fail(s,error);
        if (guard(s)) return s->first_error;
        for (unsigned word=0;word<4;word++) {
            uint64_t value=0;
            for (unsigned i=0;i<8;i++) value|=(uint64_t)bytes[word*8+i]<<(8*i);
            result.raw[n][word]=value;
        }
    }
    if (sample(s,&b)) return s->first_error;
    /* No acknowledgement/retry of an overflowing, moving or foreign queue.
     * The producer alone writes event bytes, with publication before PROD;
     * caller independently qualifies that memory/cache contract. */
    if (a.error!=b.error || a.error_ack!=b.error_ack || a.control!=b.control ||
        a.control_ack!=b.control_ack || a.base_lo!=b.base_lo || a.base_hi!=b.base_hi ||
        a.consumer!=b.consumer || a.producer!=b.producer) return fail(s,-11);
    if (guard(s)) return s->first_error;
    *out=result;return 0;
}
int nano_evt_decode(const uint64_t raw[4],struct nano_evt_decoded *out)
{
    if (!raw || !out) return -22;
    struct nano_evt_decoded d={.event=(unsigned)(raw[0]&255),.sid=(uint32_t)(raw[0]>>32),
        .ssid=(uint32_t)((raw[0]>>12)&0xfffff),.ssid_valid=(unsigned)((raw[0]>>11)&1),
        .stalled=(unsigned)((raw[1]>>31)&1),.privileged=(unsigned)((raw[1]>>33)&1),
        .instruction=(unsigned)((raw[1]>>34)&1),.read=(unsigned)((raw[1]>>35)&1),
        .stage2=(unsigned)((raw[1]>>39)&1),.event_class=(unsigned)((raw[1]>>40)&3),
        .table_read=(unsigned)((raw[1]>>44)&1)};
    d.common_fault_format=d.event>=0x10 && d.event<=0x13;
    if (d.common_fault_format) {
        d.input_address=raw[2];d.ipa_page=raw[3]&0x000ffffffffff000ULL;
    }
    d.sdm_read_candidate=d.event==0x10 && d.sid==10 && !d.ssid_valid && !d.stalled &&
        d.read && !d.instruction && d.stage2 && d.event_class==2 && !d.table_read;
    *out=d;return 0;
}
