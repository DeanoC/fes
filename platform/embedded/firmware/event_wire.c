/* SPDX-License-Identifier: BSD-3-Clause */
#include "event_wire.h"
struct writer {char *output;size_t length;uint32_t crc;};
static void byte(struct writer *w,char value)
{
    if (w->output) w->output[w->length]=value;
    w->length++;w->crc^=(uint8_t)value;
    for (unsigned i=0;i<8;i++) w->crc=(w->crc>>1)^(0xedb88320U&((uint32_t)0-(w->crc&1U)));
}
static void text(struct writer *w,const char *value)
{while (*value) byte(w,*value++);}
static void number(struct writer *w,uint64_t value,unsigned base,unsigned width)
{
    static const char digits[]="0123456789abcdef";char reverse[20];unsigned n=0;
    do {reverse[n++]=digits[value%base];value/=base;} while(value);
    while(n<width) reverse[n++]='0';
    while(n) byte(w,reverse[--n]);
}
static void render(struct writer *w,const struct nano_evt_wire_context *ctx,const struct nano_evt_batch *batch)
{
    text(w,"FES_NANO_EVTQ_BEGIN version=1 build=");
    for (unsigned i=0;i<16;i++) byte(w,ctx->build[i]);
    text(w," owner=");number(w,ctx->owner,16,0);
    text(w," epoch=");number(w,ctx->epoch,16,0);
    text(w," command_generation=");number(w,ctx->command_generation,10,0);
    text(w," event_generation=");number(w,ctx->event_generation,10,0);
    text(w," consumer=");number(w,batch->consumer,10,0);
    text(w," producer=");number(w,batch->producer,10,0);
    text(w," count=");number(w,batch->count,10,0);
    text(w," recipe=sdm-stage2-hyp-ns-v2 access=unqualified\n");
    for (unsigned i=0;i<batch->count;i++) {
        text(w,"FES_NANO_EVTQ_RAW index=");number(w,i,10,0);
        for (unsigned n=0;n<4;n++) {
            text(w," word");number(w,n,10,0);byte(w,'=');number(w,batch->raw[i][n],16,16);
        }
        byte(w,'\n');
    }
    uint32_t checksum=w->crc^UINT32_MAX;
    text(w,"FES_NANO_EVTQ_END count=");number(w,batch->count,10,0);
    text(w," crc32=");number(w,checksum,16,8);byte(w,'\n');
}
static int separate(const void *a,size_t na,const void *b,size_t nb)
{
    uintptr_t x=(uintptr_t)a,y=(uintptr_t)b;
    return na<=UINTPTR_MAX-x && nb<=UINTPTR_MAX-y && (x+na<=y || y+nb<=x);
}
static int queue(uint64_t address,unsigned size)
{
    return address>=UINT64_C(0x80000000) && address<=UINT64_C(0xc0000000)-size && address%size==0;
}
int nano_evt_wire_format(const struct nano_evt_wire_context *ctx,const struct nano_cmdq *commands,
    const struct nano_cmdq_receipt *receipt,const struct nano_evt_capture *events,const struct nano_evt_batch *batch,
    char *output,size_t capacity,size_t *length)
{
    if (!ctx || !commands || !receipt || !events || !batch || !output || !length || !capacity || capacity>NANO_EVT_WIRE_MAX) return -1;
    const void *inputs[]={ctx,commands,receipt,events,batch};
    const size_t sizes[]={sizeof(*ctx),sizeof(*commands),sizeof(*receipt),sizeof(*events),sizeof(*batch)};
    if (!separate(output,capacity,length,sizeof(*length))) return -1;
    for (unsigned i=0;i<5;i++)
        if (!separate(output,capacity,inputs[i],sizes[i]) || !separate(length,sizeof(*length),inputs[i],sizes[i])) return -1;
    if (ctx->build[16] || !ctx->owner || !ctx->epoch || !ctx->command_generation || !ctx->event_generation) return -1;
    for (unsigned i=0;i<16;i++)
        if (!((ctx->build[i]>='0' && ctx->build[i]<='9') || (ctx->build[i]>='a' && ctx->build[i]<='f'))) return -1;
    if (commands->attempted!=1 || commands->initialized!=1 || commands->submitted!=1 || commands->failed || commands->first_error ||
        commands->timed!=1 || commands->publish_attempted!=1 || commands->slots_attempted!=NANO_CMDQ_COMMANDS || !commands->polls || commands->polls>128 ||
        commands->reads!=40+4*commands->polls || commands->last<commands->start || commands->last-commands->start>NANO_CMDQ_BOUND_US ||
        commands->binding.owner!=ctx->owner || commands->binding.epoch!=ctx->epoch || commands->binding.generation!=ctx->command_generation ||
        commands->binding.baseline>15 || !queue(commands->binding.queue,NANO_CMDQ_BYTES)) return -1;
    if (receipt->owner!=ctx->owner || receipt->epoch!=ctx->epoch || receipt->generation!=ctx->command_generation ||
        receipt->baseline!=commands->binding.baseline || receipt->producer!=((receipt->baseline+NANO_CMDQ_COMMANDS)&15U) ||
        receipt->consumer!=receipt->producer || receipt->polls!=commands->polls || receipt->commands_completed!=1 ||
        receipt->interconnect_drained || receipt->DMA_access_granted) return -1;
    if (events->attempted!=1 || events->initialized!=1 || events->captured!=1 || events->failed || events->first_error ||
        events->reads!=16 || events->copies!=batch->count || events->binding.owner!=ctx->owner ||
        events->binding.epoch!=ctx->epoch || events->binding.generation!=ctx->event_generation ||
        events->binding.consumer>15 || !queue(events->binding.queue,NANO_EVT_BYTES)) return -1;
    uint64_t cq=commands->binding.queue,eq=events->binding.queue;
    if (!(cq+NANO_CMDQ_BYTES<=eq || eq+NANO_EVT_BYTES<=cq)) return -1;
    if (batch->owner!=ctx->owner || batch->epoch!=ctx->epoch || batch->generation!=ctx->event_generation ||
        batch->consumer!=events->binding.consumer || batch->producer>15 || batch->count>8 ||
        ((batch->producer-batch->consumer)&15U)!=batch->count) return -1;
    for (unsigned n=batch->count;n<8;n++) for (unsigned word=0;word<4;word++) if (batch->raw[n][word]) return -1;
    /* Snapshot before sizing/writing; no callbacks or hardware activity occur
     * between passes. This keeps failure output untouched without a large
     * temporary frame on the firmware stack. */
    struct nano_evt_wire_context context=*ctx;struct nano_evt_batch snapshot=*batch;
    struct writer first={.output=0,.length=0,.crc=UINT32_MAX};render(&first,&context,&snapshot);
    if (first.length>capacity || first.length>NANO_EVT_WIRE_MAX) return -1;
    struct writer second={.output=output,.length=0,.crc=UINT32_MAX};render(&second,&context,&snapshot);
    *length=second.length;
    return 0;
}
