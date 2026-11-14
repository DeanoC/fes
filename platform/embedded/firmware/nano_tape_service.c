/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_tape_service.h"
#include <string.h>

static uint32_t word(const uint8_t *p)
{return p[0]|(uint32_t)p[1]<<8|(uint32_t)p[2]<<16|(uint32_t)p[3]<<24;}
static uint64_t pair(const uint8_t *p) {return word(p)|((uint64_t)word(p+4)<<32);}
static int fail(struct fes_nano_tape_service *s,int error)
{if (!s->first_error) s->first_error=error;s->failed=1;return s->first_error;}
static int clock_ok(struct fes_nano_tape_service *s)
{
    uint64_t now=s->engine.io.now(s->engine.io.context);
    if (now<s->last ||now-s->began>=s->budget || now-s->step_began>=s->engine.hz) return fail(s,FES_TAPE_LIMIT);
    s->last=now;return 0;
}
static int guard(struct fes_nano_tape_service *s)
{
    if (clock_ok(s)) return s->first_error;
    if (s->io.guard(s->io.context,s->engine.owner,s->engine.generation)) return fail(s,FES_TAPE_OWNER);
    return clock_ok(s);
}
int fes_nano_tape_service_init(struct fes_nano_tape_service *s,struct fes_nano_tape_service_ops io,
    struct fes_nano_tape_ops tape,uint64_t owner,uint64_t generation,const uint16_t identity[16],uint32_t hz)
{
    if (!s ||s->initialized) return FES_TAPE_ARGUMENT;
    s->initialized=1;
    if (!io.guard ||!io.read ||!io.verify ||io.context!=tape.context) return fail(s,FES_TAPE_ARGUMENT);
    s->io=io;
    int error=fes_nano_tape_init(&s->engine,tape,owner,generation,identity,hz);
    return error ? fail(s,error) : 0;
}
static int source(struct fes_nano_tape_service *s,unsigned first)
{
    uint8_t header[FES_EMBEDDED_TAPE_HEADER_BYTES],second[FES_EMBEDDED_TAPE_HEADER_BYTES];
    if (guard(s)) return s->first_error;
    if (s->io.read(s->io.context,header)) return fail(s,FES_TAPE_IO);
    if (guard(s)) return s->first_error;
    if (s->io.read(s->io.context,second)) return fail(s,FES_TAPE_IO);
    if (guard(s)) return s->first_error;
    if (memcmp(header,second,sizeof(header))) return fail(s,FES_TAPE_INTEGRITY);
    if (first) memcpy(s->header,header,sizeof(header));
    else if (memcmp(s->header,header,sizeof(header))) return fail(s,FES_TAPE_INTEGRITY);
    const uint8_t *h=s->header;
    uint32_t bytes=word(h+12),gpo=word(h+FES_EMBEDDED_TAPE_GPO_OFFSET);
    if (word(h)!=FES_EMBEDDED_TAPE_MAGIC ||word(h+4)!=FES_EMBEDDED_TAPE_VERSION ||
        word(h+8)!=FES_EMBEDDED_TAPE_HEADER_BYTES ||bytes<FES_COMPUTER_ZX81_TAPE_MIN_BYTES ||
        bytes>FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES ||pair(h+FES_EMBEDDED_TAPE_OWNER_OFFSET)!=s->engine.owner ||
        pair(h+FES_EMBEDDED_TAPE_GENERATION_OFFSET)!=s->engine.generation ||
        (gpo & ~FES_COMPUTER_REQUEST_MASK)!=(FES_COMPUTER_OPCODE_EXECUTION<<24)) return fail(s,FES_TAPE_ARGUMENT);
    for (unsigned i=FES_EMBEDDED_TAPE_RESERVED_OFFSET;i<sizeof(s->header);i++)
        if (h[i]) return fail(s,FES_TAPE_ARGUMENT);
    for (unsigned i=0;i<8;i++) {
        unsigned offset=FES_EMBEDDED_TAPE_BUILD_OFFSET+2*i;
        if ((h[offset]|(uint16_t)h[offset+1]<<8)!=s->engine.identity[8+i]) return fail(s,FES_TAPE_IDENTITY);
    }
    s->budget=(uint64_t)fes_nano_tape_steps(bytes)*s->engine.hz;
    if (s->io.verify(s->io.context,bytes,h+FES_EMBEDDED_TAPE_SHA384_OFFSET)) return fail(s,FES_TAPE_INTEGRITY);
    return guard(s);
}
void fes_nano_tape_service_call(struct fes_nano_tape_service *s,uint64_t operation,
    uint64_t owner,uint64_t generation,uint64_t r[4])
{
    r[0]=FES_EMBEDDED_TAPE_REJECTED;r[1]=(uint64_t)FES_EMBEDDED_TAPE_TAG<<32;r[2]=r[3]=0;
    if (!s ||!s->initialized) {r[0]=FES_EMBEDDED_TAPE_NOT_READY;return;}
    if (s->calling) {r[0]=FES_EMBEDDED_TAPE_BUSY;return;}
    if (s->failed ||s->complete ||owner!=s->engine.owner ||generation!=s->engine.generation) return;
    unsigned first=!s->used;
    uint64_t sequence=(uint64_t)s->sequence<<FES_EMBEDDED_TAPE_SEQUENCE_SHIFT;
    unsigned cancel=!first && operation==(sequence|FES_EMBEDDED_TAPE_ABORT);
    if (first ? operation!=FES_EMBEDDED_TAPE_LOAD :
        (operation!=(sequence|FES_EMBEDDED_TAPE_STEP) && !cancel)) return;
    /* Consume before all timer/supplier calls. A repeated ordinal cannot issue
     * I/O or advance a later chunk. Pending never returns a final GP receipt. */
    s->used=s->calling=1;s->sequence++;
    r[0]=FES_EMBEDDED_TAPE_ERROR;
    if (cancel) {
        (void)fes_nano_tape_abort(&s->engine);
        fail(s,FES_TAPE_CANCELLED);goto out;
    }
    uint64_t now=s->engine.io.now(s->engine.io.context);
    if (first) {s->began=s->last=now;s->budget=s->engine.hz;}
    if (now<s->last || now-s->began>=s->budget) {fail(s,FES_TAPE_LIMIT);goto out;}
    s->step_began=s->last=now;
    if (source(s,first)) goto out;
    const uint8_t *h=s->header;
    uint32_t bytes=word(h+12),crc=word(h+FES_EMBEDDED_TAPE_CRC_OFFSET);
    struct fes_nano_tape_receipt receipt;
    int error=first ? fes_nano_tape_start(&s->engine,owner,generation,
        word(h+FES_EMBEDDED_TAPE_GPO_OFFSET),bytes,crc,s->step_began,s->last) :
        fes_nano_tape_step_started(&s->engine,&receipt,s->step_began,s->last);
    if (error<0) {fail(s,error);goto out;}
    /* Preserve monotonicity when returning from the engine, too. */
    s->last=s->engine.last;
    /* No NS execution occurs inside a step. Revalidate the same retained
     * bytes at both boundaries; between steps a source mutation is terminal. */
    if (guard(s)) goto out;
    if (s->io.verify(s->io.context,bytes,h+FES_EMBEDDED_TAPE_SHA384_OFFSET)) {fail(s,FES_TAPE_INTEGRITY);goto out;}
    if (guard(s)) goto out;
    if (error==FES_TAPE_PENDING) {
        r[0]=FES_EMBEDDED_TAPE_PENDING;r[1]|=s->engine.next;r[2]=generation;r[3]=s->sequence;
        s->calling=0;return;
    }
    if (!s->engine.complete ||receipt.owner!=owner ||receipt.generation!=generation ||receipt.bytes!=bytes ||receipt.crc!=crc) {
        fail(s,FES_TAPE_STATE);goto out;
    }
    s->complete=1;
    r[0]=FES_EMBEDDED_TAPE_OK;r[1]|=bytes;r[2]=generation;r[3]=receipt.gpo|((uint64_t)receipt.crc<<32);
    s->calling=0;return;
out:
    s->calling=0;
    if (s->engine.busy && !s->engine.failed) (void)fes_nano_tape_abort(&s->engine);
    if (s->first_error==FES_TAPE_LIMIT) r[0]=FES_EMBEDDED_TAPE_LIMIT;
    if (s->first_error==FES_TAPE_CANCELLED) r[0]=FES_EMBEDDED_TAPE_CANCELLED;
}
