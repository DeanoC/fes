/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_program.h"
#include <string.h>

static uint32_t le32(const uint8_t *p)
{ return (uint32_t)p[0]|(uint32_t)p[1]<<8|(uint32_t)p[2]<<16|(uint32_t)p[3]<<24; }
static uint64_t le64(const uint8_t *p)
{ return (uint64_t)le32(p)|(uint64_t)le32(p+4)<<32; }
static unsigned nonzero(const uint8_t *p, unsigned bytes)
{
    unsigned value=0;
    for(unsigned i=0;i<bytes;i++)value|=p[i];
    return value!=0;
}
static int fail(struct fes_nano_program *p)
{ p->failed=1;p->bound=0;return -1; }

int fes_nano_program_init(struct fes_nano_program *p, struct fes_nano_program_ops io,
                         const uint8_t context[32], uint32_t maximum, uint32_t chunk,
                         unsigned qualified)
{
    if(!p || p->initialized)return -1;
    p->initialized=1;
    if(qualified!=1 || !io.guard || !io.read || !context || !nonzero(context,32) ||
       !maximum || maximum>FES_NANO_PROGRAM_MAX_BYTES || (maximum&3U) ||
       !chunk || chunk>0xffff0U || (chunk&63U))return fail(p);
    p->io=io;p->maximum=maximum;p->chunk=chunk;
    memcpy(p->context,context,32);
    return 0;
}

int fes_nano_program_capture(struct fes_nano_program *p, uint64_t owner, uint64_t generation)
{
    if(!p || !p->initialized || p->failed)return -1;
    if(!owner || !generation || (p->owner && p->owner!=owner) || generation<=p->generation)
        return -1;
    /* Consume the attempt before reading any non-secure byte. The second sample
     * detects a changed publication; all later checks use this secure copy. */
    p->owner=owner;p->generation=generation;p->bound=0;
    uint8_t second[FES_NANO_PROGRAM_HEADER_BYTES];
    if(p->io.guard(p->io.context,owner,generation) || p->io.read(p->io.context,p->header) ||
       p->io.guard(p->io.context,owner,generation) || p->io.read(p->io.context,second) ||
       p->io.guard(p->io.context,owner,generation) || memcmp(p->header,second,sizeof(second)))
        return fail(p);
    const uint8_t *h=p->header;
    uint32_t bytes=le32(h+12),chunk=le32(h+16),chunks=le32(h+20);
    if(le32(h)!=FES_NANO_PROGRAM_MAGIC || le32(h+4)!=FES_NANO_PROGRAM_VERSION ||
       le32(h+8)!=FES_NANO_PROGRAM_HEADER_BYTES || !bytes || (bytes&3U) || bytes>p->maximum ||
       chunk!=p->chunk || !chunks || chunks>FES_NANO_PROGRAM_MAX_CHUNKS ||
       chunks!=(bytes-1)/chunk+1 || le64(h+24)!=owner || le64(h+32)!=generation ||
       memcmp(h+FES_NANO_PROGRAM_CONTEXT,p->context,32) ||
       !nonzero(h+FES_NANO_PROGRAM_PACKAGE,32) || !nonzero(h+FES_NANO_PROGRAM_BUILD,16) ||
       !nonzero(h+FES_NANO_PROGRAM_SHA256,32) || !nonzero(h+FES_NANO_PROGRAM_SHA384,48))
        return fail(p);
    for(unsigned i=0;i<chunks;i++)
        if(!nonzero(h+FES_NANO_PROGRAM_CHUNKS+i*48,48))return fail(p);
    if(nonzero(h+FES_NANO_PROGRAM_CHUNKS+chunks*48,
               FES_NANO_PROGRAM_HEADER_BYTES-FES_NANO_PROGRAM_CHUNKS-chunks*48))return fail(p);
    if(p->io.guard(p->io.context,owner,generation))return fail(p);
    p->bytes=bytes;p->chunks=chunks;p->bound=1;
    return 0;
}

const uint8_t *fes_nano_program_expected(const struct fes_nano_program *p,
                                       uint32_t offset, uint32_t bytes, unsigned whole)
{
    if(!p || !p->bound || p->failed || whole>1 || offset>p->bytes || !bytes || bytes>p->bytes-offset)
        return 0;
    if(whole)return !offset && bytes==p->bytes ? p->header+FES_NANO_PROGRAM_SHA384 : 0;
    uint32_t amount=p->bytes-offset;
    if(amount>p->chunk)amount=p->chunk;
    uint32_t index=offset/p->chunk;
    return !(offset%p->chunk) && bytes==amount && index<p->chunks ?
        p->header+FES_NANO_PROGRAM_CHUNKS+index*48 : 0;
}

int fes_nano_program_build(const struct fes_nano_program *p, uint32_t words[4])
{
    if(!p || !p->bound || p->failed || !words)return -1;
    for(unsigned i=0;i<4;i++) {
        const uint8_t *b=p->header+FES_NANO_PROGRAM_BUILD+12-4*i;
        words[i]=(uint32_t)b[0]<<24|(uint32_t)b[1]<<16|(uint32_t)b[2]<<8|(uint32_t)b[3];
    }
    return 0;
}
