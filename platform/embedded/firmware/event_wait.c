/* SPDX-License-Identifier: BSD-3-Clause */
#include "event_wait.h"
#include "probe_setup.h"
static int fail(struct nano_evt_wait *s,int error)
{
    s->failed=1;if(!s->first_error)s->first_error=error<0 ? error : -5;
    return s->first_error;
}
static int owns(struct nano_evt_wait *s)
{
    if(s->failed)return s->first_error;
    return s->source.owns(s->source.context,&s->binding)==1 ? 0 : fail(s,-13);
}
static int time_read(struct nano_evt_wait *s,uint64_t *value)
{
    if(owns(s))return s->first_error;
    s->times++;int error=s->clock.time(s->clock.context,value);
    if(error)return fail(s,error);
    if(owns(s))return s->first_error;
    if(s->begun && *value<s->last)return fail(s,-5);
    return 0;
}
static int read_producer(struct nano_evt_wait *s,uint32_t *value)
{
    if(owns(s))return s->first_error;
    s->source.barrier(s->source.context);s->reads++;
    int error=s->source.read_producer(s->source.context,value);
    s->source.barrier(s->source.context);
    return error ? fail(s,error) : owns(s);
}
int nano_evt_wait_init(struct nano_evt_wait *s,struct nano_evt_binding b,
    struct nano_evt_wait_io source,struct nano_evt_clock clock)
{
    if(!s)return -22;
    if(s->attempted)return -16;
    s->attempted=1;
    s->binding=b;s->source=source;s->clock=clock;
    if(!b.owner || !b.epoch || !b.generation || b.queue!=NANO_PROBE_BASE+NANO_PROBE_EVTQ || b.consumer ||
       !source.context || !source.owns || !source.read_producer || !source.barrier || !clock.context || !clock.time || s->initialized || s->failed)
        return fail(s,-22);
    s->initialized=1;return 0;
}
int nano_evt_wait_begin(struct nano_evt_wait *s,const struct nano_evt_batch *empty)
{
    if(!s || !empty || !s->initialized)return -22;
    if(s->begin_attempted || s->failed)return s->first_error ? s->first_error : -16;
    s->begin_attempted=1;
    if(empty->owner!=s->binding.owner || empty->epoch!=s->binding.epoch ||
       empty->generation!=s->binding.generation || empty->count || empty->producer || empty->consumer)
        return fail(s,-13);
    uint64_t now=0;if(time_read(s,&now))return s->first_error;
    s->start=s->last=now;s->begun=1;return 0;
}
int nano_evt_wait_finish(struct nano_evt_wait *s)
{
    if(!s || !s->initialized)return -22;
    if(s->finish_attempted)return -16;
    s->finish_attempted=1;
    if(!s->begun || s->failed)return s->first_error ? s->first_error : fail(s,-13);
    while(s->polls<NANO_EVT_WAIT_POLLS){
        uint32_t producer=0;
        s->polls++;
        if(read_producer(s,&producer))return s->first_error;
        s->producer=producer;
        if((producer&~15U) || producer>NANO_EVT_ENTRIES)return fail(s,-5);
        uint64_t now=0;if(time_read(s,&now))return s->first_error;
        s->last=now;s->elapsed=now-s->start;
        if(producer)s->publication_seen=1;
        if(s->elapsed>=NANO_EVT_WAIT_US){s->timed_out=1;return fail(s,-110);}
        if(producer)return 0;
    }
    s->poll_limit=1;return fail(s,-110);
}
