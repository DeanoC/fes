/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_rom.h"
#include <string.h>

static int fail(struct fes_nano_rom *p, int error)
{
    if (!p->first_error) p->first_error=error;
    p->failed=1;
    return p->first_error;
}
static int clock_ok(struct fes_nano_rom *p)
{
    uint64_t now=p->io.now(p->io.context);
    if (now<p->last || now-p->began>=FES_EMBEDDED_ROM_MAX_TICKS)
        return fail(p,FES_NANO_ROM_LIMIT);
    p->last=now;return 0;
}
static int check(struct fes_nano_rom *p)
{
    if (clock_ok(p)) return p->first_error;
    if (p->io.owns(p->io.context,p->owner,p->generation)!=1)
        return fail(p,FES_NANO_ROM_OWNER);
    return clock_ok(p);
}
static int read_reg(struct fes_nano_rom *p,uint32_t offset,uint32_t *value)
{
    if (check(p)) return p->first_error;
    if (p->io.read32(p->io.context,offset,value)) return fail(p,FES_NANO_ROM_IO);
    (p->io.barrier)(p->io.context);
    return check(p);
}
static int write_reg(struct fes_nano_rom *p,uint32_t offset,uint32_t value)
{
    if (check(p)) return p->first_error;
    if (p->io.write32(p->io.context,offset,value)) return fail(p,FES_NANO_ROM_IO);
    (p->io.barrier)(p->io.context);
    return check(p);
}
static uint32_t crc_byte(uint32_t crc,uint8_t byte)
{
    crc^=byte;
    for (unsigned bit=0;bit<8;bit++) crc=(crc>>1)^((crc&1) ? UINT32_C(0xedb88320) : 0);
    return crc;
}
static int equal_reg(struct fes_nano_rom *p,uint32_t offset,uint32_t expected,int error)
{
    uint32_t value;
    if (read_reg(p,offset,&value)) return p->first_error;
    return value==expected ? 0 : fail(p,error);
}
static int identity(struct fes_nano_rom *p)
{
    if (equal_reg(p,FES_EMBEDDED_ROM_ID_OFFSET,FES_EMBEDDED_ROM_MAGIC,FES_NANO_ROM_IDENTITY) ||
        equal_reg(p,FES_EMBEDDED_ROM_VERSION_OFFSET,FES_EMBEDDED_ROM_VERSION,FES_NANO_ROM_IDENTITY) ||
        equal_reg(p,FES_EMBEDDED_ROM_BYTES_OFFSET,p->bytes,FES_NANO_ROM_IDENTITY)) return p->first_error;
    for (unsigned i=0;i<4;i++)
        if (equal_reg(p,FES_EMBEDDED_ROM_BUILD_OFFSET+4*i,p->build[i],FES_NANO_ROM_IDENTITY)) return p->first_error;
    return 0;
}
static int wait_status(struct fes_nano_rom *p,unsigned final)
{
    for (unsigned poll=0;poll<FES_EMBEDDED_ROM_MAX_POLLS;poll++) {
        uint32_t status;
        if (read_reg(p,FES_EMBEDDED_ROM_STATUS_OFFSET,&status)) return p->first_error;
        if ((status&~FES_EMBEDDED_ROM_STATUS_MASK) || !(status&FES_EMBEDDED_ROM_HELD) ||
            (status&FES_EMBEDDED_ROM_FAILED)) return fail(p,FES_NANO_ROM_STATE);
        if (final) {
            if (status==(FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_LOADED)) return 0;
        } else if (status==(FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_ACTIVE|FES_EMBEDDED_ROM_READY)) return 0;
        if (status&FES_EMBEDDED_ROM_LOADED) return fail(p,FES_NANO_ROM_STATE);
    }
    return fail(p,FES_NANO_ROM_LIMIT);
}
int fes_nano_rom_init(struct fes_nano_rom *p,struct fes_nano_rom_ops io,
                     uint64_t owner,uint64_t generation,const uint32_t build[4])
{
    if (!p || p->initialized) return FES_NANO_ROM_ARGUMENT;
    p->initialized=1;
    uint32_t nonzero=0;
    if (build) for (unsigned i=0;i<4;i++) nonzero|=build[i];
    if (!owner || !generation || !nonzero || !io.owns || !io.read32 || !io.write32 || !io.now || !io.barrier)
        return fail(p,FES_NANO_ROM_ARGUMENT);
    p->io=io;p->owner=owner;p->generation=generation;
    memcpy(p->build,build,sizeof(p->build));return 0;
}
static int load(struct fes_nano_rom *p,const uint8_t *bytes,size_t size,uint32_t expected_crc,
                int (*verify)(void *,const uint8_t *,size_t),void *context)
{
    if (!p || !p->initialized || p->failed) return FES_NANO_ROM_STATE;
    if (p->busy) return FES_NANO_ROM_BUSY;
    if (p->used) return FES_NANO_ROM_STATE;
    if (!bytes || (size!=4096 && size!=8192)) return FES_NANO_ROM_ARGUMENT;
    p->busy=1;p->began=p->last=p->io.now(p->io.context);
    if (check(p)) goto finished;
    uint32_t crc=UINT32_C(0xffffffff);
    for (size_t i=0;i<size;i++) {
        p->image[i]=bytes[i];crc=crc_byte(crc,p->image[i]);
        if ((i&255)==255 && check(p)) goto finished;
    }
    crc^=UINT32_C(0xffffffff);
    if (crc!=expected_crc) { p->busy=0;return FES_NANO_ROM_INTEGRITY; }
    if (verify && (verify(context,p->image,size) || check(p))) {
        fail(p,FES_NANO_ROM_INTEGRITY);goto finished;
    }
    p->used=1;p->bytes=(uint32_t)size;p->crc=crc;
    if (identity(p) || equal_reg(p,FES_EMBEDDED_ROM_STATUS_OFFSET,FES_EMBEDDED_ROM_HELD,FES_NANO_ROM_STATE) ||
        equal_reg(p,FES_EMBEDDED_ROM_RECEIVED_OFFSET,0,FES_NANO_ROM_STATE) ||
        equal_reg(p,FES_EMBEDDED_ROM_WRITTEN_OFFSET,0,FES_NANO_ROM_STATE)) goto finished;
    if (write_reg(p,FES_EMBEDDED_ROM_EXPECTED_CRC_OFFSET,crc) ||
        write_reg(p,FES_EMBEDDED_ROM_COMMAND_OFFSET,FES_EMBEDDED_ROM_START)) goto finished;
    for (uint32_t offset=0;offset<size;offset+=16) {
        if (wait_status(p,0) || equal_reg(p,FES_EMBEDDED_ROM_RECEIVED_OFFSET,offset,FES_NANO_ROM_STATE) ||
            equal_reg(p,FES_EMBEDDED_ROM_WRITTEN_OFFSET,offset,FES_NANO_ROM_STATE)) goto finished;
        for (unsigned word=0;word<4;word++) {
            uint32_t data=0;
            for (unsigned byte=0;byte<4;byte++) data|=(uint32_t)p->image[offset+4*word+byte]<<(8*byte);
            if (write_reg(p,FES_EMBEDDED_ROM_DATA_OFFSET+4*word,data)) goto finished;
        }
    }
    if (wait_status(p,1) || equal_reg(p,FES_EMBEDDED_ROM_RECEIVED_OFFSET,p->bytes,FES_NANO_ROM_STATE) ||
        equal_reg(p,FES_EMBEDDED_ROM_WRITTEN_OFFSET,p->bytes,FES_NANO_ROM_STATE) ||
        equal_reg(p,FES_EMBEDDED_ROM_OBSERVED_CRC_OFFSET,p->crc,FES_NANO_ROM_INTEGRITY) || identity(p) ||
        equal_reg(p,FES_EMBEDDED_ROM_STATUS_OFFSET,FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_LOADED,FES_NANO_ROM_STATE)) goto finished;
    p->complete=1;
finished:
    p->busy=0;return p->failed ? p->first_error : 0;
}
int fes_nano_rom_load(struct fes_nano_rom *p,const uint8_t *bytes,size_t size,uint32_t expected_crc)
{ return load(p,bytes,size,expected_crc,0,0); }
int fes_nano_rom_load_checked(struct fes_nano_rom *p,const uint8_t *bytes,size_t size,uint32_t expected_crc,
    int (*verify)(void *,const uint8_t *,size_t),void *context)
{
    if(!verify)return FES_NANO_ROM_ARGUMENT;
    return load(p,bytes,size,expected_crc,verify,context);
}
