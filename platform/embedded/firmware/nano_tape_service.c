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
    if (now<s->last ||now-s->began>=s->engine.hz) return fail(s,FES_TAPE_LIMIT);
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
void fes_nano_tape_service_call(struct fes_nano_tape_service *s,uint64_t operation,
    uint64_t owner,uint64_t generation,uint64_t r[4])
{
    r[0]=FES_EMBEDDED_TAPE_REJECTED;r[1]=(uint64_t)FES_EMBEDDED_TAPE_TAG<<32;r[2]=r[3]=0;
    if (operation!=FES_EMBEDDED_TAPE_LOAD) return;
    if (!s ||!s->initialized) {r[0]=FES_EMBEDDED_TAPE_NOT_READY;return;}
    if (s->used ||s->failed ||owner!=s->engine.owner ||generation!=s->engine.generation) return;
    s->used=1;s->began=s->last=s->engine.io.now(s->engine.io.context);
    r[0]=FES_EMBEDDED_TAPE_ERROR;
    uint8_t second[FES_EMBEDDED_TAPE_HEADER_BYTES];
    if (guard(s)) goto out;
    if (s->io.read(s->io.context,s->header)) {fail(s,FES_TAPE_IO);goto out;}
    if (guard(s)) goto out;
    if (s->io.read(s->io.context,second)) {fail(s,FES_TAPE_IO);goto out;}
    if (guard(s)) goto out;
    if (memcmp(s->header,second,sizeof(second))) {fail(s,FES_TAPE_INTEGRITY);goto out;}
    const uint8_t *h=s->header;
    uint32_t bytes=word(h+12),gpo=word(h+FES_EMBEDDED_TAPE_GPO_OFFSET),crc=word(h+FES_EMBEDDED_TAPE_CRC_OFFSET);
    if (word(h)!=FES_EMBEDDED_TAPE_MAGIC ||word(h+4)!=FES_EMBEDDED_TAPE_VERSION ||
        word(h+8)!=FES_EMBEDDED_TAPE_HEADER_BYTES ||bytes<FES_COMPUTER_ZX81_TAPE_MIN_BYTES ||
        bytes>FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES ||pair(h+FES_EMBEDDED_TAPE_OWNER_OFFSET)!=owner ||
        pair(h+FES_EMBEDDED_TAPE_GENERATION_OFFSET)!=generation ||
        (gpo & ~FES_COMPUTER_REQUEST_MASK)!=(FES_COMPUTER_OPCODE_EXECUTION<<24)) {fail(s,FES_TAPE_ARGUMENT);goto out;}
    for (unsigned i=FES_EMBEDDED_TAPE_RESERVED_OFFSET;i<sizeof(s->header);i++)
        if (h[i]) {fail(s,FES_TAPE_ARGUMENT);goto out;}
    for (unsigned i=0;i<8;i++) {
        unsigned offset=FES_EMBEDDED_TAPE_BUILD_OFFSET+2*i;
        if ((h[offset]|(uint16_t)h[offset+1]<<8)!=s->engine.identity[8+i]) {fail(s,FES_TAPE_IDENTITY);goto out;}
    }
    if (s->io.verify(s->io.context,bytes,h+FES_EMBEDDED_TAPE_SHA384_OFFSET)) {fail(s,FES_TAPE_INTEGRITY);goto out;}
    if (guard(s)) goto out;
    struct fes_nano_tape_receipt receipt;
    int error=fes_nano_tape_load_started(&s->engine,owner,generation,gpo,bytes,crc,&receipt,s->began,s->last);
    if (error) {fail(s,error);goto out;}
    if (!s->engine.complete ||receipt.owner!=owner ||receipt.generation!=generation ||receipt.bytes!=bytes ||receipt.crc!=crc) {fail(s,FES_TAPE_STATE);goto out;}
    if (guard(s)) goto out;
    if (s->io.verify(s->io.context,bytes,h+FES_EMBEDDED_TAPE_SHA384_OFFSET)) {fail(s,FES_TAPE_INTEGRITY);goto out;}
    if (guard(s)) goto out;
    s->complete=1;
    r[0]=FES_EMBEDDED_TAPE_OK;r[1]|=bytes;r[2]=generation;r[3]=receipt.gpo|((uint64_t)receipt.crc<<32);
out:
    if (s->failed &&s->first_error==FES_TAPE_LIMIT) r[0]=FES_EMBEDDED_TAPE_LIMIT;
}
