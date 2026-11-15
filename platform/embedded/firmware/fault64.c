/* SPDX-License-Identifier: BSD-3-Clause */
#include "fault64.h"
static int fail(struct nano_fault64 *s,int error)
{
    s->failed=1;if(!s->first_error)s->first_error=error<0 ? error : -5;return s->first_error;
}
static int private_scope(struct nano_fault64 *s,unsigned edge)
{
    const struct nano_probe_setup *p=s->setup;
    const struct fes_nano_configuration *c=s->configuration;
    const struct fes_nano_program *r=s->program;
    const struct fes_nano_bridges *b=c ? c->bridges : 0;
    if (!p || !p->initialized || p->failed || !p->prepared || !p->armed ||
        p->binding.owner!=s->owner || p->binding.epoch!=s->epoch || p->binding.generation!=s->generation ||
        !b || !b->initialized || b->failed || b->device_lease ||
        b->epoch!=s->epoch || b->state.owner!=s->owner || b->state.generation!=s->generation ||
        b->state.reset!=15 || b->state.never_enabled!=1 || b->state.masters_contained!=1 ||
        !c->initialized || c->failed || !c->prepared || !c->started || c->finished || c->rearmed ||
        c->owner!=s->owner || c->generation!=s->generation || c->base!=UINT64_C(0x94800000) ||
        c->completed || !c->bytes || c->bytes>c->maximum || c->bytes<=64 || c->chunk<64 || (c->chunk&63) ||
        !r || !r->initialized || !r->bound || r->failed || r->owner!=s->owner || r->generation!=s->generation ||
        r->bytes!=c->bytes || r->chunk!=c->chunk || r->mixed_roles!=1 ||
        r->role!=FES_NANO_PROGRAM_ROLE_MENU || r->control_abi!=FES_NANO_PROGRAM_MENU_ABI)
        return fail(s,-13);
    if (!edge) {
        if (c->issued!=FES_NANO_CONFIG_START || c->stage!=FES_CONFIG_START || c->pending)return fail(s,-13);
    } else if (!((c->issued==FES_NANO_CONFIG_WRITE && c->stage==FES_CONFIG_WRITE) ||
                 (c->issued==FES_NANO_CONFIG_COMPLETED && c->stage==FES_CONFIG_POLL)) ||
               c->pending<(c->bytes<c->chunk ? c->bytes : c->chunk) || c->pending>c->bytes ||
               (c->pending!=c->bytes && c->pending%c->chunk))return fail(s,-13);
    return 0;
}
static int owns(struct nano_fault64 *s,unsigned edge)
{
    if (private_scope(s,edge))return s->first_error;
    if(s->io.owns(s->io.context,s->owner,s->epoch,s->generation)!=1)return fail(s,-13);
    return private_scope(s,edge);
}
int nano_fault64_init(struct nano_fault64 *s,const struct nano_probe_setup *p,
    const struct fes_nano_configuration *c,const struct fes_nano_program *r,
    struct nano_fault64_io io,struct nano_evt_io events,struct nano_evt_wait_io publication)
{
    if(!s)return -22;
    if(s->attempted)return -16;
    s->attempted=1;s->setup=p;s->configuration=c;s->program=r;s->io=io;
    if(!p || !io.owns || !io.time || s->initialized || s->failed)return fail(s,-22);
    s->owner=p->binding.owner;s->epoch=p->binding.epoch;s->generation=p->binding.generation;
    if(!s->owner || !s->epoch || !s->generation || owns(s,0))return fail(s,-13);
    struct nano_evt_binding b={s->owner,s->epoch,s->generation,NANO_PROBE_BASE+NANO_PROBE_EVTQ,0};
    int error=nano_evt_init(&s->baseline,b,events);if(error)return fail(s,error);
    error=nano_evt_init(&s->events,b,events);if(error)return fail(s,error);
    error=nano_evt_wait_init(&s->waiter,b,publication,(struct nano_evt_clock){io.context,io.time});
    if(error)return fail(s,error);
    if(owns(s,0))return s->first_error;
    s->initialized=1;return 0;
}
int nano_fault64_before(struct nano_fault64 *s,const struct nano_fault64_vendor *v,uint32_t *selected)
{
    if(!s || !s->initialized || !v || !selected)return -22;
    if(s->edge_attempted || s->failed)return s->first_error ? s->first_error : -16;
    s->edge_attempted=1;
    if(owns(s,1))return s->first_error;
    uint32_t first=s->configuration->bytes;
    if(first>s->configuration->chunk)first=s->configuration->chunk;
    uint32_t expected=first<v->block_bytes ? first : v->block_bytes;
    if(v->address!=UINT64_C(0x14800000) || v->buffer_address!=v->address ||
        v->offered!=expected || v->offered<64 || v->buffer_size!=first ||
        v->written || v->requested!=1 || v->subblocks || v->block_number ||
        v->current_buffer!=(unsigned)(first<=v->block_bytes) || v->current_block || v->read_block || v->blocks_submitted!=1 ||
        v->send_id || v->active_buffers!=1 || !v->credits || v->credits>INT32_MAX ||
        v->block_bytes<64 || v->first_buffer!=1 || v->reconfiguration!=1 || v->full_configuration!=1)return fail(s,-22);
    if(owns(s,1))return s->first_error;
    s->address=(uint32_t)v->address;s->offered=v->offered;s->selected=64;s->validated=1;
    *selected=64;return 0;
}
/* Validation and the empty baseline precede serial SUBMIT logging. Start
 * the one-use observation clock only at the actual mailbox edge, after that
 * logging. The waiter supplies fresh complete ownership before/after its
 * pure counter read; the local scope is checked on both sides as well. */
int nano_fault64_submit(struct nano_fault64 *s)
{
    if(!s || !s->initialized)return -22;
    if(s->submit_attempted)return -16;
    s->submit_attempted=1;
    if(s->failed || !s->validated || !s->edge_attempted || s->after_attempted || s->selected!=64)return fail(s,-13);
    if(private_scope(s,1))return s->first_error;
    /* START itself can generate an unrelated event. Establish a fresh empty
     * queue immediately before this DATA intent, without acknowledging it. */
    int baseline=nano_evt_snapshot(&s->baseline,&s->before_batch);
    if(baseline)return fail(s,baseline);
    if(s->before_batch.count || s->before_batch.producer || s->before_batch.consumer)return fail(s,-5);
    if(owns(s,1))return s->first_error;
    int status=nano_evt_wait_begin(&s->waiter,&s->before_batch);
    if(status)return fail(s,status);
    if(private_scope(s,1))return s->first_error;
    s->intent=1;s->submitted=1;return 0;
}
int nano_fault64_after(struct nano_fault64 *s,int mailbox_status)
{
    if(!s || !s->initialized)return -22;
    if(s->after_attempted)return -16;
    s->after_attempted=1;s->mailbox_status=mailbox_status;
    if(s->failed || !s->intent || !s->edge_attempted || !s->submitted || s->selected!=64)return fail(s,-13);
    /* A failed asynchronous call may have posted some words. Capture once
     * under unchanged ownership, retain the failure, and never retry it. */
    if(owns(s,1))return s->first_error;
    s->wait_status=nano_evt_wait_finish(&s->waiter);
    /* Timeout/poll exhaustion still permits one raw final snapshot while
     * ownership is valid. Clock, queue or ownership failures do not. */
    if(s->wait_status && s->wait_status!=-110)return fail(s,s->wait_status);
    if(owns(s,1))return s->first_error;
    s->capture_attempted=1;s->event_status=nano_evt_snapshot(&s->events,&s->batch);
    if(s->event_status)return fail(s,s->event_status);
    s->captured=1;
    if(owns(s,1))return s->first_error;
    if(mailbox_status)return fail(s,mailbox_status);
    if(s->wait_status)return fail(s,s->wait_status);
    return 0;
}
int nano_fault64_format(struct nano_fault64 *s,const char build[17],char *out,size_t capacity,size_t *length)
{
    if(!s || !build || !s->captured || !s->after_attempted || !s->capture_attempted || s->event_status ||
       !s->baseline.captured || s->baseline.failed || s->before_batch.count)return -22;
    /* Formatting raw captured data remains available after a mailbox failure;
     * it never clears that failure or turns it into a successful transfer. */
    struct nano_evt_wire_context context={.owner=s->owner,.epoch=s->epoch,
        .command_generation=s->generation,.event_generation=s->generation};
    for(unsigned i=0;i<17;i++)context.build[i]=build[i];
    s->wire_status=nano_evt_wire_format(&context,&s->setup->command,&s->setup->command_receipt,
        &s->events,&s->batch,out,capacity,length);return s->wire_status;
}
