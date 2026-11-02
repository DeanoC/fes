/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_ddr_read.h"
#include "generated/fes_embedded_control.h"
#include <string.h>

static int fail(struct fes_nano_ddr_read *s,int error)
{
    if (!s->first_error) s->first_error=error<0 ? error : -5;
    s->failed=1;s->done=0;
    return s->first_error;
}
static int scope(struct fes_nano_ddr_read *s)
{ return s->io.owns(s->io.context)==1 ? 0 : fail(s,-1); }
static int read_word(struct fes_nano_ddr_read *s,uint32_t offset,uint32_t *value)
{
    if (scope(s)) return s->first_error;
    s->reads++;
    int error=s->io.read32(s->io.context,offset,value);
    if (error) return fail(s,error);
    return scope(s);
}
static int write_word(struct fes_nano_ddr_read *s,uint32_t offset,uint32_t value)
{
    if (scope(s)) return s->first_error;
    s->writes++;
    int error=s->io.write32(s->io.context,offset,value);
    if (error) return fail(s,error);
    return scope(s);
}
static int phase(struct fes_nano_ddr_read *s,unsigned value)
{
    if (scope(s)) return s->first_error;
    s->phase=value;
    if (s->io.trace) s->io.trace(s->io.context,value);
    return scope(s);
}
static int wait_once(struct fes_nano_ddr_read *s)
{
    if (scope(s)) return s->first_error;
    s->io.wait(s->io.context);
    return scope(s);
}
uint32_t fes_nano_ddr_read_pattern(unsigned word)
{ return 0x4e414e4fU ^ ((uint32_t)word*0x9e3779b9U); }

/* Include every identity, owner-visible request/ACK and diagnostic counter
 * that could otherwise make a previous transaction look like this one. */
static int initial(struct fes_nano_ddr_read *s)
{
    static const uint32_t offsets[]={0,4,8,12,16,20,24,28,32,36,40,44,48,
        56,60,64,68,72,76,0xb0,0xb4,0xb8,0x94,0x98,0xa0,0xa4,0xa8,0xac};
    uint32_t a[28],b[28];
    for (unsigned i=0;i<28;i++) if (read_word(s,offsets[i],&a[i])) return s->first_error;
    for (unsigned i=0;i<28;i++) if (read_word(s,offsets[i],&b[i])) return s->first_error;
    if (memcmp(a,b,sizeof(a)) || a[0]!=FES_EMBEDDED_MAGIC || a[1]!=FES_EMBEDDED_VERSION ||
        a[2]!=s->abi || memcmp(a+3,s->build,sizeof(s->build)) ||
        a[19]!=s->bus_base || a[20]!=s->window_bytes || a[21]!=1) return fail(s,-71);
    for (unsigned i=7;i<19;i++) if (a[i]) return fail(s,-71);
    for (unsigned i=22;i<28;i++) if (a[i]) return fail(s,-71);
    return 0;
}
static int state(struct fes_nano_ddr_read *s,unsigned expected,unsigned pending,unsigned acknowledged)
{
    static const uint32_t offsets[]={FES_EMBEDDED_REG_STATUS,FES_EMBEDDED_REG_PENDING,
        FES_EMBEDDED_REG_GENERATION_LO,FES_EMBEDDED_REG_GENERATION_HI,
        FES_EMBEDDED_REG_ACK_GENERATION_LO,FES_EMBEDDED_REG_ACK_GENERATION_HI,
        FES_EMBEDDED_REG_ACK_COOKIE_LO,FES_EMBEDDED_REG_ACK_COOKIE_HI,
        FES_EMBEDDED_REG_REQUEST_GENERATION_LO,FES_EMBEDDED_REG_REQUEST_GENERATION_HI,
        FES_EMBEDDED_REG_REQUEST_COOKIE_LO,FES_EMBEDDED_REG_REQUEST_COOKIE_HI};
    for (unsigned poll=0;poll<FES_NANO_DDR_READ_POLLS;poll++) {
        uint32_t v[12],previous[12];s->polls++;
        for (unsigned i=0;i<12;i++) if (read_word(s,offsets[i],&previous[i])) return s->first_error;
        for (unsigned i=0;i<12;i++) if (read_word(s,offsets[i],&v[i])) return s->first_error;
        if (memcmp(previous,v,sizeof(v))) {
            if (wait_once(s)) return s->first_error;
            continue;
        }
        if ((v[0]&~7U) || v[2]!=1 || v[3] || v[5] || v[7] || v[8]!=1 || v[9] || v[11] ||
            (v[1]&~0x10007U) || (v[1]&7U)>1 ||
            v[10]!=(s->phase>=FES_DDR_READ_DISABLE ? 7U : 0U)) return fail(s,-71);
        if (!acknowledged && (v[4] || v[6] || (v[0]&FES_EMBEDDED_STATUS_DRAINED))) return fail(s,-71);
        if (acknowledged && (v[4] || v[6] || (v[0]&FES_EMBEDDED_STATUS_DRAINED)) &&
            !(v[0]==FES_EMBEDDED_STATUS_DRAINED && !v[1] && v[4]==1 && v[6]==7)) return fail(s,-71);
        if (v[0]==expected && v[1]==pending && v[4]==acknowledged && v[6]==(acknowledged ? 7U : 0U)) return 0;
        /* Only ENABLED -> DRAINING -> DRAINED is permitted. The paired sample
         * above waits through legitimate response/counter transitions. */
        if ((expected==FES_EMBEDDED_STATUS_ENABLED && v[0]!=expected) ||
            (expected!=FES_EMBEDDED_STATUS_ENABLED && v[0]!=FES_EMBEDDED_STATUS_DRAINING)) return fail(s,-71);
        if (wait_once(s)) return s->first_error;
    }
    return fail(s,-110);
}
int fes_nano_ddr_read(struct fes_nano_ddr_read *s,struct fes_nano_ddr_read_observation *out)
{
    if (!s || !out) return -22;
    if (s->failed) return s->first_error;
    if (s->attempted) return -1;
    s->attempted=1;
    unsigned build=0;for (unsigned i=0;i<4;i++) build|=s->build[i];
    if (s->qualified!=1 || s->abi!=0x10203041U || !build ||
        s->bus_base!=FES_NANO_DDR_READ_BASE || s->window_bytes!=FES_NANO_DDR_READ_WINDOW ||
        !s->io.owns || !s->io.read32 || !s->io.write32 || !s->io.wait) return fail(s,-22);
#define READ(a,v) do { if(read_word(s,a,v))return s->first_error; } while(0)
#define WRITE(a,v) do { if(write_word(s,a,v))return s->first_error; } while(0)
#define PHASE(v) do { if(phase(s,v))return s->first_error; } while(0)
    PHASE(FES_DDR_READ_INITIAL);
    if (initial(s)) return s->first_error;
    PHASE(FES_DDR_READ_ENABLE);
    WRITE(FES_EMBEDDED_REG_REQUEST_GENERATION_LO,1);
    WRITE(FES_EMBEDDED_REG_REQUEST_GENERATION_HI,0);
    WRITE(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_ENABLE);
    if (state(s,FES_EMBEDDED_STATUS_ENABLED,0,0)) return s->first_error;
    PHASE(FES_DDR_READ_OFFER);
    WRITE(0x84,FES_NANO_DDR_READ_BASE);WRITE(0x88,1);WRITE(0x80,8);
    if (state(s,FES_EMBEDDED_STATUS_ENABLED,1,0)) return s->first_error;
    PHASE(FES_DDR_READ_ACCEPTED);
    PHASE(FES_DDR_READ_DISABLE);
    WRITE(FES_EMBEDDED_REG_REQUEST_COOKIE_LO,7);WRITE(FES_EMBEDDED_REG_REQUEST_COOKIE_HI,0);
    WRITE(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_DISABLE);
    PHASE(FES_DDR_READ_DRAIN);
    /* DISABLE forces the actual endpoint's AXI RREADY while suppressing
     * consumer delivery. Fast memory can finish before our first status
     * read; a software-visible DRAINING sample is not required. */
    if (state(s,FES_EMBEDDED_STATUS_DRAINED,0,1)) return s->first_error;
    PHASE(FES_DDR_READ_VERIFY);
    struct fes_nano_ddr_read_observation result={.generation=1,.cookie=7};
    READ(0x94,&result.returned_bytes);READ(0x98,&result.checksum);
    for (unsigned i=0;i<4;i++) READ(0xa0+4*i,&result.last_data[i]);
    uint32_t checksum=0;for (unsigned i=0;i<8;i++) checksum^=fes_nano_ddr_read_pattern(i);
    if (result.returned_bytes!=FES_NANO_DDR_READ_BYTES || result.checksum!=checksum) return fail(s,-71);
    for (unsigned i=0;i<4;i++) if (result.last_data[i]!=fes_nano_ddr_read_pattern(i+4)) return fail(s,-71);
    if (state(s,FES_EMBEDDED_STATUS_DRAINED,0,1)) return s->first_error;
    PHASE(FES_DDR_READ_COMPLETE);
    s->done=1;*out=result;
#undef READ
#undef WRITE
#undef PHASE
    return 0;
}
