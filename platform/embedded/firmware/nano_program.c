/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_program.h"
#include <string.h>

_Static_assert(FES_NANO_PROGRAM_ROLE_OFFSET==FES_NANO_PROGRAM_CHUNKS+48U*FES_NANO_PROGRAM_MAX_CHUNKS &&
               FES_NANO_PROGRAM_ROLE_OFFSET+8U==FES_NANO_PROGRAM_HEADER_BYTES,
               "role fields must follow every bounded digest slot");

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

int fes_nano_program_enable_rom(struct fes_nano_program *p,
    int (*read_rom)(void *, uint8_t result[FES_NANO_PROGRAM_ROM_HEADER_BYTES]))
{
    if(!p || !p->initialized || p->failed || p->owner || p->bound || p->read_rom || !read_rom)return -1;
    p->read_rom=read_rom;return 0;
}

int fes_nano_program_enable_roles(struct fes_nano_program *p)
{
    if(!p || !p->initialized || p->failed || p->owner || p->bound || !p->read_rom || p->mixed_roles)return -1;
    p->mixed_roles=1;return 0;
}

static int capture_extension(struct fes_nano_program *p)
{
    uint8_t second[FES_NANO_PROGRAM_ROM_HEADER_BYTES];
    if(!p->read_rom || p->io.guard(p->io.context,p->owner,p->generation) ||
       p->read_rom(p->io.context,p->rom_header) ||
       p->io.guard(p->io.context,p->owner,p->generation) ||
       p->read_rom(p->io.context,second) ||
       p->io.guard(p->io.context,p->owner,p->generation) ||
       memcmp(p->rom_header,second,sizeof(second)))return fail(p);
    return 0;
}

static int capture_rom(struct fes_nano_program *p,uint32_t bytes)
{
    if(capture_extension(p))return -1;
    const uint8_t *h=p->rom_header;
    uint32_t offset=le32(h+12),size=le32(h+16);
    if(le32(h)!=FES_NANO_PROGRAM_ROM_MAGIC || le32(h+4)!=1 || le32(h+8)!=sizeof(p->rom_header) ||
       offset!=((bytes+63U)&~63U) || (size!=4096 && size!=8192) ||
       offset>p->maximum || size>p->maximum-offset ||
       !nonzero(h+FES_NANO_PROGRAM_ROM_SHA256,32) ||
       !nonzero(h+FES_NANO_PROGRAM_ROM_SHA384,48) || nonzero(h+104,24))return fail(p);
    p->rom_offset=offset;p->rom_bytes=size;p->rom_crc=le32(h+20);
    return 0;
}

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
    p->rom_offset=p->rom_bytes=p->rom_crc=0;
    p->role=p->control_abi=0;
    memset(p->rom_header,0,sizeof(p->rom_header));
    uint8_t second[FES_NANO_PROGRAM_HEADER_BYTES];
    if(p->io.guard(p->io.context,owner,generation) || p->io.read(p->io.context,p->header) ||
       p->io.guard(p->io.context,owner,generation) || p->io.read(p->io.context,second) ||
       p->io.guard(p->io.context,owner,generation) || memcmp(p->header,second,sizeof(second)))
        return fail(p);
    const uint8_t *h=p->header;
    uint32_t bytes=le32(h+12),chunk=le32(h+16),chunks=le32(h+20);
    uint32_t version=le32(h+4);
    if(le32(h)!=FES_NANO_PROGRAM_MAGIC ||
       version!=(p->mixed_roles ? FES_NANO_PROGRAM_ROLE_VERSION :
                  p->read_rom ? FES_NANO_PROGRAM_ROM_VERSION : FES_NANO_PROGRAM_VERSION) ||
       le32(h+8)!=FES_NANO_PROGRAM_HEADER_BYTES || !bytes || (bytes&3U) || bytes>p->maximum ||
       chunk!=p->chunk || !chunks || chunks>FES_NANO_PROGRAM_MAX_CHUNKS ||
       chunks!=(bytes-1)/chunk+1 || le64(h+24)!=owner || le64(h+32)!=generation ||
       memcmp(h+FES_NANO_PROGRAM_CONTEXT,p->context,32) ||
       !nonzero(h+FES_NANO_PROGRAM_PACKAGE,32) || !nonzero(h+FES_NANO_PROGRAM_BUILD,16) ||
       !nonzero(h+FES_NANO_PROGRAM_SHA256,32) || !nonzero(h+FES_NANO_PROGRAM_SHA384,48))
        return fail(p);
    for(unsigned i=0;i<chunks;i++)
        if(!nonzero(h+FES_NANO_PROGRAM_CHUNKS+i*48,48))return fail(p);
    const uint32_t padding_end=p->mixed_roles ? FES_NANO_PROGRAM_ROLE_OFFSET : FES_NANO_PROGRAM_HEADER_BYTES;
    if(nonzero(h+FES_NANO_PROGRAM_CHUNKS+chunks*48,
               padding_end-FES_NANO_PROGRAM_CHUNKS-chunks*48))return fail(p);
    uint32_t role=0,abi=0;
    if(p->mixed_roles) {
        role=le32(h+FES_NANO_PROGRAM_ROLE_OFFSET);abi=le32(h+FES_NANO_PROGRAM_ROLE_OFFSET+4);
        if(role==FES_NANO_PROGRAM_ROLE_MENU && abi==FES_NANO_PROGRAM_MENU_ABI) {
            if(capture_extension(p) || nonzero(p->rom_header,sizeof(p->rom_header)))return fail(p);
        } else if(role==FES_NANO_PROGRAM_ROLE_COMPUTER && abi==FES_NANO_PROGRAM_COMPUTER_ABI) {
            if(capture_rom(p,bytes))return -1;
        } else return fail(p);
    }
    if(version==FES_NANO_PROGRAM_ROM_VERSION && capture_rom(p,bytes))return -1;
    if(p->io.guard(p->io.context,owner,generation))return fail(p);
    p->bytes=bytes;p->chunks=chunks;p->role=role;p->control_abi=abi;p->bound=1;
    return 0;
}

int fes_nano_program_role(const struct fes_nano_program *p,uint32_t *role,uint32_t *abi)
{
    if(!p || !p->initialized || !p->mixed_roles || !p->bound || p->failed || !role || !abi)return -1;
    *role=p->role;*abi=p->control_abi;return 0;
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
