/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_tape.h"
#include <string.h>

static int fail(struct fes_nano_tape *s,int error)
{
    if (!s->first_error) s->first_error=error;
    s->failed=1;return s->first_error;
}
static int clock_ok(struct fes_nano_tape *s)
{
    uint64_t now=s->io.now(s->io.context);
    if (now<s->last || now-s->began>=s->budget || now-s->step_began>=s->hz) return fail(s,FES_TAPE_LIMIT);
    s->last=now;return 0;
}
static int check(struct fes_nano_tape *s)
{
    if (clock_ok(s)) return s->first_error;
    if (!s->framed || s->io.owns(s->io.context,s->owner,s->generation)!=1)
        return fail(s,FES_TAPE_OWNER);
    return clock_ok(s);
}
static int begin(struct fes_nano_tape *s,unsigned phase,uint32_t offset)
{
    if (s->framed || clock_ok(s)) return fail(s,FES_TAPE_STATE);
    if (s->io.begin(s->io.context,s->owner,s->generation,phase,offset))
        return fail(s,FES_TAPE_OWNER);
    /* A successful supplier begin must always be consumed, even if its
     * elapsed time or the first independent owns check subsequently fails. */
    s->framed=1;s->phase=phase;s->frame_offset=offset;
    return check(s);
}
static int end(struct fes_nano_tape *s)
{
    if (check(s)) return s->first_error;
    s->framed=0;
    if (s->io.end(s->io.context,s->owner,s->generation,s->phase,s->frame_offset,1))
        return fail(s,FES_TAPE_OWNER);
    return clock_ok(s);
}
static int read_reg(struct fes_nano_tape *s,uint32_t offset,uint32_t *v)
{
    s->offset=offset;
    if (check(s)) return s->first_error;
    s->reads++;
    if (s->io.read32(s->io.context,offset,v)) return fail(s,FES_TAPE_IO);
    (s->io.barrier)(s->io.context);
    return check(s);
}
struct snapshot {uint32_t status,gpo,gpi,fault;};
static int snapshot(struct fes_nano_tape *s,struct snapshot *v)
{
    if (read_reg(s,FES_DE25_COMPUTER_STATUS_OFFSET,&v->status) ||
        read_reg(s,FES_DE25_COMPUTER_GPO_OFFSET,&v->gpo) ||
        read_reg(s,FES_DE25_COMPUTER_GPI_OFFSET,&v->gpi) ||
        read_reg(s,FES_DE25_COMPUTER_FAULT_OFFSET,&v->fault)) return s->first_error;
    if (v->fault || (v->status & ~(FES_DE25_COMPUTER_STATUS_READY|FES_DE25_COMPUTER_STATUS_BUSY)) ||
        !(v->status & FES_DE25_COMPUTER_STATUS_READY) ||
        (v->gpi & ~(FES_COMPUTER_ACK_MASK|FES_COMPUTER_ERROR_MASK|FES_COMPUTER_RESPONSE_MASK))!=FES_COMPUTER_SIGNATURE)
        return fail(s,FES_TAPE_STATE);
    return 0;
}
static int acknowledged(const struct snapshot *v)
{return !!(v->gpo & FES_COMPUTER_REQUEST_MASK)==!!(v->gpi & FES_COMPUTER_ACK_MASK);}
static int stable(struct fes_nano_tape *s,struct snapshot *v)
{
    struct snapshot first;
    if (snapshot(s,&first) || snapshot(s,v)) return s->first_error;
    if (first.status!=v->status || first.gpo!=v->gpo || first.gpi!=v->gpi || first.fault!=v->fault) return 1;
    if ((!(v->status & FES_DE25_COMPUTER_STATUS_BUSY))!=acknowledged(v)) return fail(s,FES_TAPE_STATE);
    return 0;
}
static int exchange(struct fes_nano_tape *s,uint32_t opcode,uint32_t index,uint16_t argument,uint16_t *response)
{
    struct snapshot v;
    s->opcode=opcode;s->index=index;s->polls=0;
    if (stable(s,&v) || v.gpo!=s->gpo || !acknowledged(&v)) return fail(s,FES_TAPE_STATE);
    uint32_t word=((s->gpo^FES_COMPUTER_REQUEST_MASK)&FES_COMPUTER_REQUEST_MASK)|opcode<<24|index<<16|argument;
    s->offset=FES_DE25_COMPUTER_GPO_OFFSET;
    if (check(s)) return s->first_error;
    s->writes++;
    /* Exactly one toggle. No lost/uncertain write is ever replayed. */
    if (s->io.write32(s->io.context,FES_DE25_COMPUTER_GPO_OFFSET,word)) return fail(s,FES_TAPE_IO);
    (s->io.barrier)(s->io.context);
    if (check(s)) return s->first_error;
    for (unsigned poll=0;poll<FES_NANO_TAPE_POLLS;poll++) {
        s->polls=poll+1;
        int status=stable(s,&v);
        if (status<0) return status;
        if (!status) {
            if (v.gpo!=word) return fail(s,FES_TAPE_STATE);
            if (acknowledged(&v)) {
                if (v.gpi & FES_COMPUTER_ERROR_MASK) return fail(s,FES_TAPE_STATE);
                s->gpo=word;*response=(uint16_t)(v.gpi & FES_COMPUTER_RESPONSE_MASK);return 0;
            }
        }
    }
    return fail(s,FES_TAPE_LIMIT);
}
static int mutate(struct fes_nano_tape *s,uint32_t opcode,uint32_t index,uint16_t value)
{
    uint16_t response;
    if (exchange(s,opcode,index,value,&response)) return s->first_error;
    return response ? fail(s,FES_TAPE_STATE) : 0;
}
static int identity(struct fes_nano_tape *s)
{
    uint32_t value;
    if (read_reg(s,FES_DE25_COMPUTER_ID_OFFSET,&value)) return s->first_error;
    if (value!=FES_DE25_COMPUTER_IDENTITY) return fail(s,FES_TAPE_IDENTITY);
    if (read_reg(s,FES_DE25_COMPUTER_VERSION_OFFSET,&value)) return s->first_error;
    if (value!=FES_DE25_COMPUTER_VERSION) return fail(s,FES_TAPE_IDENTITY);
    for (unsigned i=0;i<FES_NANO_TAPE_IDENTITY_WORDS;i++) {
        uint16_t response;
        if (exchange(s,FES_COMPUTER_OPCODE_IDENTITY,i,0,&response)) return s->first_error;
        if (response!=s->identity[i]) return fail(s,FES_TAPE_IDENTITY);
    }
    return 0;
}
static int fetch(struct fes_nano_tape *s,uint32_t *count,uint32_t *address)
{
    if (read_reg(s,FES_DE25_COMPUTER_FETCH_COUNT_OFFSET,count) ||
        read_reg(s,FES_DE25_COMPUTER_FETCH_ADDRESS_OFFSET,address)) return s->first_error;
    return *address>UINT16_MAX ? fail(s,FES_TAPE_STATE) : 0;
}
static int tape_info(struct fes_nano_tape *s,unsigned final)
{
    uint16_t info[6];
    for (unsigned i=0;i<6;i++)
        if (exchange(s,FES_COMPUTER_OPCODE_MEDIA_INFO,i,0,&info[i])) return s->first_error;
    if (((uint32_t)info[0]|(uint32_t)info[1]<<16)!=FES_COMPUTER_ZX81_TAPE_MIN_BYTES ||
        ((uint32_t)info[2]|(uint32_t)info[3]<<16)!=FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES ||
        info[4]!=FES_COMPUTER_MEDIA_CHUNK_MAX_BYTES ||
        (info[5]!=FES_COMPUTER_MEDIA_STATE_READY && (final || info[5]!=FES_COMPUTER_MEDIA_STATE_EMPTY)))
        return fail(s,FES_TAPE_STATE);
    return 0;
}
static uint32_t crc_bytes(uint32_t crc,const uint8_t *bytes,uint32_t count)
{
    for (uint32_t i=0;i<count;i++) {
        crc^=bytes[i];
        for (unsigned bit=0;bit<8;bit++) crc=(crc>>1)^((0U-(crc&1U))&UINT32_C(0xedb88320));
    }
    return crc;
}
static int image(struct fes_nano_tape *s,uint32_t offset,uint32_t count)
{
    if (!count || count>sizeof(s->chunk) || offset>s->bytes || count>s->bytes-offset) return fail(s,FES_TAPE_ARGUMENT);
    if (check(s)) return s->first_error;
    if (s->io.image(s->io.context,offset,s->chunk,count)) return fail(s,FES_TAPE_IO);
    return check(s);
}
int fes_nano_tape_init(struct fes_nano_tape *s,struct fes_nano_tape_ops io,
    uint64_t owner,uint64_t generation,const uint16_t words[FES_NANO_TAPE_IDENTITY_WORDS],uint32_t hz)
{
    if (!s || s->initialized) return FES_TAPE_ARGUMENT;
    s->initialized=1;
    if (!owner || !generation || !words || !hz || hz>1000000000U ||
        !io.owns || !io.begin || !io.end || !io.read32 || !io.write32 || !io.image || !io.now || !io.barrier)
        return fail(s,FES_TAPE_ARGUMENT);
    if (words[0]!=FES_COMPUTER_IDENTITY_MAGIC0 || words[1]!=FES_COMPUTER_IDENTITY_MAGIC1 ||
        words[2]!=FES_COMPUTER_TRANSPORT_MAJOR || words[3]!=FES_COMPUTER_TRANSPORT_MINOR ||
        words[4]!=FES_COMPUTER_ABI_TAG || words[5]!=FES_COMPUTER_ABI_MAJOR || words[6]!=0)
        return fail(s,FES_TAPE_ARGUMENT);
    unsigned nonzero=0;
    for (unsigned i=FES_COMPUTER_IDENTITY_BUILD_IDSTART_INDEX;i<FES_NANO_TAPE_IDENTITY_WORDS;i++) nonzero|=words[i];
    if (!nonzero) return fail(s,FES_TAPE_ARGUMENT);
    s->io=io;s->owner=owner;s->generation=generation;s->hz=hz;
    memcpy(s->identity,words,sizeof(s->identity));return 0;
}
/* Every return closes the supplier frame. Progress remains private and the
 * receiver remains held; the next call must re-establish its own live frame. */
static int leave(struct fes_nano_tape *s,int status)
{
    if (s->framed) {
        s->framed=0;
        (void)s->io.end(s->io.context,s->owner,s->generation,s->phase,s->frame_offset,0);
    }
    s->executing=0;
    if (s->failed || s->complete) s->busy=0;
    return s->failed ? s->first_error : status;
}
int fes_nano_tape_start(struct fes_nano_tape *s,uint64_t owner,uint64_t generation,
    uint32_t gpo,uint32_t bytes,uint32_t crc,uint64_t origin,uint64_t previous)
{
    if (!s || !s->initialized || s->failed || !owner ||
        owner!=s->owner || generation!=s->generation || bytes<FES_COMPUTER_ZX81_TAPE_MIN_BYTES ||
        bytes>FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES ||
        (gpo & ~FES_COMPUTER_REQUEST_MASK)!=(FES_COMPUTER_OPCODE_EXECUTION<<24|
            FES_COMPUTER_CONTROL_INDEX<<16|FES_COMPUTER_EXECUTION_HOLD_RESET)) return FES_TAPE_ARGUMENT;
    if (s->busy) return FES_TAPE_BUSY;
    if (s->used) return FES_TAPE_ARGUMENT;
    s->busy=s->used=s->executing=1;s->gpo=gpo;s->bytes=bytes;s->crc=crc;
    s->began=s->step_began=origin;
    s->budget=(uint64_t)fes_nano_tape_steps(bytes)*s->hz;
    s->last=s->io.now(s->io.context);
    if (previous<origin || s->last<previous || clock_ok(s)) {
        fail(s,FES_TAPE_LIMIT);return leave(s,0);
    }
    if (begin(s,FES_TAPE_PREPARE,0)) return leave(s,0);
    struct snapshot initial;
    if (stable(s,&initial) || initial.gpo!=gpo || !acknowledged(&initial) ||
        (initial.gpi & (FES_COMPUTER_ERROR_MASK|FES_COMPUTER_RESPONSE_MASK))) {
        fail(s,FES_TAPE_STATE);return leave(s,0);
    }
    if (fetch(s,&s->fetches,&s->fetch_address) || identity(s) || tape_info(s,0)) return leave(s,0);
    uint32_t calculated=UINT32_MAX;
    for (uint32_t offset=0;offset<bytes;) {
        uint32_t count=bytes-offset;if (count>sizeof(s->chunk)) count=sizeof(s->chunk);
        if (image(s,offset,count)) return leave(s,0);
        calculated=crc_bytes(calculated,s->chunk,count);offset+=count;
    }
    if (~calculated!=crc) {fail(s,FES_TAPE_INTEGRITY);return leave(s,0);}
    for (unsigned i=0;i<4;i++) {
        uint16_t word=i==0 ? (uint16_t)bytes : i==1 ? (uint16_t)(bytes>>16) :
            i==2 ? (uint16_t)crc : (uint16_t)(crc>>16);
        if (mutate(s,FES_COMPUTER_OPCODE_MEDIA_BEGIN,i,word)) return leave(s,0);
    }
    if (end(s)) return leave(s,0);
    s->calculated=UINT32_MAX;
    return leave(s,FES_TAPE_PENDING);
}
int fes_nano_tape_step_started(struct fes_nano_tape *s,struct fes_nano_tape_receipt *receipt,
    uint64_t origin,uint64_t previous)
{
    if (!s || !s->initialized || !s->used || !s->busy || s->failed || s->complete || !receipt)
        return FES_TAPE_ARGUMENT;
    if (s->executing) return FES_TAPE_BUSY;
    s->executing=1;
    uint64_t now=s->io.now(s->io.context);
    if (previous<s->last || origin>previous || now<previous || now-s->began>=s->budget || now-origin>=s->hz) {
        fail(s,FES_TAPE_LIMIT);return leave(s,0);
    }
    s->last=now;s->step_began=origin;
    if (s->next<s->bytes) {
        uint32_t offset=s->next,count=s->bytes-offset;
        if (count>sizeof(s->chunk)) count=sizeof(s->chunk);
        if (begin(s,FES_TAPE_CHUNK,offset) || image(s,offset,count)) return leave(s,0);
        s->calculated=crc_bytes(s->calculated,s->chunk,count);
        if (mutate(s,FES_COMPUTER_OPCODE_MEDIA_CHUNK,0,(uint16_t)offset) ||
            mutate(s,FES_COMPUTER_OPCODE_MEDIA_CHUNK,1,(uint16_t)(offset>>16)) ||
            mutate(s,FES_COMPUTER_OPCODE_MEDIA_CHUNK,2,(uint16_t)count)) return leave(s,0);
        for (uint32_t i=0;i<count;i+=2) {
            uint16_t word=s->chunk[i];if (i+1<count) word|=(uint16_t)s->chunk[i+1]<<8;
            if (mutate(s,FES_COMPUTER_OPCODE_MEDIA_DATA,i/2,word)) return leave(s,0);
        }
        if (end(s)) return leave(s,0);
        s->next+=count;
        return leave(s,FES_TAPE_PENDING);
    }
    if (begin(s,FES_TAPE_FINISH,s->bytes)) return leave(s,0);
    if (~s->calculated!=s->crc) {fail(s,FES_TAPE_INTEGRITY);return leave(s,0);}
    if (mutate(s,FES_COMPUTER_OPCODE_MEDIA_COMMIT,FES_COMPUTER_ZX81_TAPE_UNIT,0) ||
        tape_info(s,1) || identity(s)) return leave(s,0);
    uint32_t fetches,address;
    if (fetch(s,&fetches,&address)) return leave(s,0);
    if (fetches!=s->fetches || address!=s->fetch_address) {fail(s,FES_TAPE_STATE);return leave(s,0);}
    if (end(s)) return leave(s,0);
    *receipt=(struct fes_nano_tape_receipt){s->owner,s->generation,s->bytes,s->crc,s->gpo,fetches,(uint16_t)address};
    s->complete=1;
    return leave(s,0);
}
int fes_nano_tape_step(struct fes_nano_tape *s,struct fes_nano_tape_receipt *receipt)
{
    if (!s || !s->initialized || !s->busy || s->failed || s->complete) return FES_TAPE_ARGUMENT;
    if (s->executing) return FES_TAPE_BUSY;
    uint64_t origin=s->io.now(s->io.context);
    return fes_nano_tape_step_started(s,receipt,origin,origin);
}
int fes_nano_tape_abort(struct fes_nano_tape *s)
{
    if (!s || !s->used || !s->busy || s->complete || s->failed) return FES_TAPE_ARGUMENT;
    if (s->executing) return FES_TAPE_BUSY;
    fail(s,FES_TAPE_CANCELLED);
    return leave(s,0);
}
int fes_nano_tape_load(struct fes_nano_tape *s,uint64_t owner,uint64_t generation,
    uint32_t gpo,uint32_t bytes,uint32_t crc,struct fes_nano_tape_receipt *receipt)
{
    if (!s || !s->initialized || !receipt || s->failed || !owner ||
        owner!=s->owner || generation!=s->generation || bytes<FES_COMPUTER_ZX81_TAPE_MIN_BYTES ||
        bytes>FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES ||
        (gpo & ~FES_COMPUTER_REQUEST_MASK)!=(FES_COMPUTER_OPCODE_EXECUTION<<24|
            FES_COMPUTER_CONTROL_INDEX<<16|FES_COMPUTER_EXECUTION_HOLD_RESET)) return FES_TAPE_ARGUMENT;
    if (s->busy) return FES_TAPE_BUSY;
    if (s->used) return FES_TAPE_ARGUMENT;
    uint64_t origin=s->io.now(s->io.context);
    int error=fes_nano_tape_start(s,owner,generation,gpo,bytes,crc,origin,origin);
    while (error==FES_TAPE_PENDING) error=fes_nano_tape_step(s,receipt);
    return error;
}
