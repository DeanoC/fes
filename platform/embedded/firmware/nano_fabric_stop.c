/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_fabric_stop.h"
#include "nano_program.h"
#include "generated/fes_embedded_control.h"
#include <string.h>

enum { MAGIC, VERSION, ABI, BUILD0, BUILD1, BUILD2, BUILD3, GENERATION_LO,
    GENERATION_HI, REQUEST_LO, REQUEST_HI, COOKIE_LO, COOKIE_HI, STATUS,
    PENDING, ACK_LO, ACK_HI, ACK_COOKIE_LO, ACK_COOKIE_HI, BASE, BYTES, HELD };
static int fail(struct fes_nano_fabric_stop *s, int error)
{
    if (!s->first_error) s->first_error=error<0 ? error : -5;
    s->failed=1;s->done=0;
    return s->first_error;
}
static int scope(struct fes_nano_fabric_stop *s)
{ return s->io.owns(s->io.context)==1 ? 0 : fail(s,-1); }
static uint64_t pair(const struct fes_nano_fabric_snapshot *v,unsigned low)
{ return v->words[low] | ((uint64_t)v->words[low+1]<<32); }
static int read_word(struct fes_nano_fabric_stop *s,uint32_t offset,uint32_t *value)
{
    if (scope(s)) return s->first_error;
    s->reads++;
    int error=s->io.read32(s->io.context,offset,value);
    if (error) return fail(s,error);
    return scope(s);
}
static int snapshot(struct fes_nano_fabric_stop *s,struct fes_nano_fabric_snapshot *v)
{
    const uint32_t offsets[]={FES_EMBEDDED_REG_MAGIC,FES_EMBEDDED_REG_VERSION,FES_EMBEDDED_REG_ABI,
        FES_EMBEDDED_REG_BUILD0,FES_EMBEDDED_REG_BUILD1,FES_EMBEDDED_REG_BUILD2,FES_EMBEDDED_REG_BUILD3,
        FES_EMBEDDED_REG_GENERATION_LO,FES_EMBEDDED_REG_GENERATION_HI,
        FES_EMBEDDED_REG_REQUEST_GENERATION_LO,FES_EMBEDDED_REG_REQUEST_GENERATION_HI,
        FES_EMBEDDED_REG_REQUEST_COOKIE_LO,FES_EMBEDDED_REG_REQUEST_COOKIE_HI,
        FES_EMBEDDED_REG_STATUS,FES_EMBEDDED_REG_PENDING,
        FES_EMBEDDED_REG_ACK_GENERATION_LO,FES_EMBEDDED_REG_ACK_GENERATION_HI,
        FES_EMBEDDED_REG_ACK_COOKIE_LO,FES_EMBEDDED_REG_ACK_COOKIE_HI,0xb0,0xb4,
        s->abi==0x10203043U ? 0xccU : s->abi==0x10203042U ? 0xc8U : 0xb8U};
    for (unsigned i=0;i<22;i++) {
        if (read_word(s,offsets[i],&v->words[i])) return s->first_error;
    }
    if (v->words[MAGIC]!=FES_EMBEDDED_MAGIC || v->words[VERSION]!=FES_EMBEDDED_VERSION ||
        v->words[ABI]!=s->abi || memcmp(v->words+BUILD0,s->build,sizeof(s->build)) ||
        v->words[BASE]!=s->bus_base || v->words[BYTES]!=s->bytes || v->words[HELD]!=1 ||
        (v->words[PENDING]&~0x10007U) || (v->words[PENDING]&7U)>4)
        return fail(s,-71);
    return 0;
}
static int stable(struct fes_nano_fabric_stop *s,struct fes_nano_fabric_snapshot *v)
{
    struct fes_nano_fabric_snapshot a;
    if (snapshot(s,&a) || snapshot(s,v)) return s->first_error;
    return memcmp(&a,v,sizeof(a)) ? 1 : 0;
}
static int write_word(struct fes_nano_fabric_stop *s,uint32_t offset,uint32_t value)
{
    if (scope(s)) return s->first_error;
    int error=s->io.write32(s->io.context,offset,value);
    if (error) return fail(s,error);
    return scope(s);
}
static int drained(const struct fes_nano_fabric_snapshot *v)
{
    return v->words[STATUS]==FES_EMBEDDED_STATUS_DRAINED && !v->words[PENDING] &&
        pair(v,GENERATION_LO) && pair(v,REQUEST_LO)==pair(v,GENERATION_LO) &&
        pair(v,ACK_LO)==pair(v,GENERATION_LO) && pair(v,COOKIE_LO) &&
        pair(v,ACK_COOKIE_LO)==pair(v,COOKIE_LO);
}
static int reader_stop(struct fes_nano_fabric_stop *s)
{
    uint64_t generation=0;
    for (unsigned poll=0;poll<FES_NANO_FABRIC_STOP_POLLS;poll++) {
        struct fes_nano_fabric_snapshot v;
        s->polls++;
        int error=stable(s,&v);
        if (error<0) return error;
        if (error) { s->io.wait(s->io.context);continue; }
        struct fes_nano_fabric_progress observed={
            pair(&v,GENERATION_LO),pair(&v,REQUEST_LO),pair(&v,COOKIE_LO),
            pair(&v,ACK_LO),pair(&v,ACK_COOKIE_LO),v.words[STATUS],v.words[PENDING]};
        if (!s->observations) s->first_observed=observed;
        s->last_observed=observed;s->observations++;s->observed_poll=s->polls;
        if (!s->disable_issued) {
            /* A never-enabled or already-drained reader needs no command.
             * Every mutable word must agree; reset/status alone is insufficient. */
            unsigned zero=0;
            for (unsigned i=GENERATION_LO;i<=ACK_COOKIE_HI;i++) zero|=v.words[i];
            if (!zero || drained(&v)) { s->last=v;return scope(s); }
            generation=pair(&v,GENERATION_LO);
            if (v.words[STATUS]!=FES_EMBEDDED_STATUS_ENABLED || !generation ||
                pair(&v,REQUEST_LO)!=generation || pair(&v,ACK_LO) || pair(&v,ACK_COOKIE_LO)) return fail(s,-71);
            /* Frame updates may have completed earlier reader generations.
             * Their cookies remain monotonic across ENABLE. Select a fresh
             * stop token from this validated snapshot before issuing anything. */
            uint64_t previous=pair(&v,COOKIE_LO);
            if (s->cookie<=previous) {
                if (previous==UINT64_MAX) return fail(s,-75);
                s->cookie=previous+1;
            }
            /* Record intent before a possibly issued write. No retry can turn
             * an uncertain command into a drain receipt. */
            s->disable_issued=1;
            if (write_word(s,FES_EMBEDDED_REG_REQUEST_COOKIE_LO,(uint32_t)s->cookie) ||
                write_word(s,FES_EMBEDDED_REG_REQUEST_COOKIE_HI,(uint32_t)(s->cookie>>32)) ||
                write_word(s,FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_DISABLE)) return s->first_error;
        } else {
            if (pair(&v,GENERATION_LO)!=generation || pair(&v,REQUEST_LO)!=generation ||
                pair(&v,COOKIE_LO)!=s->cookie) return fail(s,-1);
            if (drained(&v)) { s->last=v;return scope(s); }
            if (v.words[STATUS]!=FES_EMBEDDED_STATUS_DRAINING ||
                pair(&v,ACK_LO) || pair(&v,ACK_COOKIE_LO)) return fail(s,-71);
        }
        s->io.wait(s->io.context);
    }
    return fail(s,-110);
}
/* Private native48 view of the fixed-grant scanout. The platform must bind
 * the guarded scanout aperture and the independent probe from one activation.
 * SNAPSHOT increments one serial and latches the reader's actual grant and
 * outstanding count. A bounded guard error is terminal. */
enum { SC_ID, SC_VERSION, SC_BUILD0, SC_BUILD1, SC_BUILD2, SC_BUILD3, SC_SERIAL,
    SC_LIVE, SC_STATUS, SC_GEN_LO, SC_GEN_HI, SC_OUTSTANDING, SC_BASE, SC_BASE_HI,
    SC_BYTES, SC_WORDS };
#define SC_RETIRED UINT32_C(0x80000000)
#define SC_DRAINED UINT32_C(0x08)
#define SC_DISPLAY_READY UINT32_C(0x40)
#define SC_CONFIGURED UINT32_C(0x01)
#define SC_BUSY UINT32_C(0x06)
#define SC_COMMAND_OFFSET UINT32_C(0x0c)
#define SC_STATUS_OFFSET UINT32_C(0x08)
#define SC_SERIAL_OFFSET UINT32_C(0x2c)
#define SC_COMMAND_STOP UINT32_C(2)
#define SC_COMMAND_SNAPSHOT UINT32_C(8)
#define SC_STOPPED_MASK (SC_RETIRED|SC_DRAINED|SC_DISPLAY_READY|SC_CONFIGURED)
static int scanout_identity(struct fes_nano_fabric_stop *s,struct fes_nano_fabric_snapshot *v)
{
    const uint32_t offsets[]={0x00,0x04,0x64,0x68,0x6c,0x70};
    for(unsigned i=0;i<sizeof(offsets)/sizeof(offsets[0]);i++)
        if(read_word(s,offsets[i],&v->words[i]))return s->first_error;
    if(v->words[SC_ID]!=0x44325343U || v->words[SC_VERSION]!=0x00010003U ||
       memcmp(v->words+SC_BUILD0,s->build,sizeof(s->build)))return fail(s,-71);
    return 0;
}
static int scanout_snapshot(struct fes_nano_fabric_stop *s,struct fes_nano_fabric_snapshot *v)
{
    uint32_t serial;
    const uint32_t offsets[]={SC_SERIAL_OFFSET,SC_STATUS_OFFSET,0x30,0x34,0x38,0x54,0x58,0x5c,0x60};
    _Static_assert(sizeof(offsets)/sizeof(offsets[0])==SC_WORDS-SC_SERIAL,"scanout receipt offsets");
    if(scanout_identity(s,v))return s->first_error;
    for(unsigned i=SC_SERIAL;i<SC_WORDS;i++)
        if(read_word(s,offsets[i-SC_SERIAL],&v->words[i]))return s->first_error;
    if(read_word(s,SC_SERIAL_OFFSET,&serial))return s->first_error;
    uint32_t status=v->words[SC_STATUS];
    if(!serial || serial!=v->words[SC_SERIAL] || (status&~SC_STOPPED_MASK) ||
       (status&(SC_RETIRED|SC_DRAINED))!=(SC_RETIRED|SC_DRAINED) ||
       ((status^v->words[SC_LIVE])&~SC_DISPLAY_READY) || v->words[SC_OUTSTANDING] ||
       v->words[SC_BASE_HI])return fail(s,-71);
    if(status&SC_CONFIGURED) {
        if(pair(v,SC_GEN_LO)!=s->cookie || v->words[SC_BASE]!=s->bus_base ||
           v->words[SC_BYTES]!=s->bytes)return fail(s,-71);
    } else if(pair(v,SC_GEN_LO) || v->words[SC_BASE] || v->words[SC_BYTES])return fail(s,-71);
    v->words[SC_STATUS] &= ~SC_DISPLAY_READY;
    v->words[SC_LIVE] &= ~SC_DISPLAY_READY;
    return scope(s);
}
static int scanout_stop(struct fes_nano_fabric_stop *s)
{
    struct fes_nano_fabric_snapshot v={0};
    uint32_t status,serial;
    if(scanout_identity(s,&v) || read_word(s,SC_STATUS_OFFSET,&status))return s->first_error;
    if((status&~(SC_STOPPED_MASK|SC_BUSY)))return fail(s,-71);
    if(!(status&SC_RETIRED)) {
        s->disable_issued=1;
        if(write_word(s,SC_COMMAND_OFFSET,SC_COMMAND_STOP))return s->first_error;
    }
    for(unsigned poll=0;poll<FES_NANO_FABRIC_STOP_POLLS;poll++) {
        s->polls++;
        if(read_word(s,SC_STATUS_OFFSET,&status))return s->first_error;
        if((status&~(SC_STOPPED_MASK|SC_BUSY)) || !(status&SC_RETIRED))return fail(s,-71);
        if((status&(SC_DRAINED|SC_BUSY))==SC_DRAINED) {
            if(read_word(s,SC_SERIAL_OFFSET,&serial))return s->first_error;
            if(serial==UINT32_MAX)return fail(s,-75);
            if(write_word(s,SC_COMMAND_OFFSET,SC_COMMAND_SNAPSHOT) || scanout_snapshot(s,&v))return s->first_error;
            if(v.words[SC_SERIAL]!=serial+1)return fail(s,-71);
            s->last=v;
            return scope(s);
        }
        s->io.wait(s->io.context);
    }
    return fail(s,-110);
}

int fes_nano_fabric_stopped(struct fes_nano_fabric_stop *s)
{
    if (!s || s->failed) return s && s->first_error ? s->first_error : -1;
    if (!s->done) return fail(s,-1);
    struct fes_nano_fabric_snapshot v={0};
    int observed=s->abi==FES_NANO_PROGRAM_SPECTRUM48_ABI ? scanout_snapshot(s,&v) : stable(s,&v);
    if (observed || memcmp(&v,&s->last,sizeof(v))) return fail(s,s->first_error ? s->first_error : -1);
    /* In this branch the probe helper only reads its completed receipt. */
    if (!s->probe.done || fes_nano_probe_stop(&s->probe)) return fail(s,s->probe.first_error ? s->probe.first_error : -1);
    return scope(s);
}
int fes_nano_fabric_stop(struct fes_nano_fabric_stop *s)
{
    if (!s || s->failed) return s && s->first_error ? s->first_error : -1;
    if (s->done) return fes_nano_fabric_stopped(s);
    if (s->attempted) return fail(s,-1);
    s->attempted=1;
    unsigned build=0;
    for (unsigned i=0;i<4;i++) build|=s->build[i];
    unsigned spectrum=s->abi==FES_NANO_PROGRAM_SPECTRUM48_ABI;
    unsigned reader=(spectrum && s->bytes==(8U<<20)) ||
                    (s->abi==0x10203041U && s->bytes==8192U) ||
                    (s->abi==0x10203042U && s->bytes==(4U<<20)) ||
                    (s->abi==0x10203043U && s->bytes==(8U<<20));
    if (s->qualified!=1 || !reader || !build || !s->cookie ||
        s->bus_base!=0x95000000U ||
        !s->io.owns || !s->io.read32 || !s->io.write32 || !s->io.wait ||
        !s->probe.io.owns || !s->probe.io.read32 || !s->probe.io.write32 || !s->probe.io.wait)
        return fail(s,-22);
    if (spectrum ? scanout_stop(s) : reader_stop(s)) return s->first_error;
    if (fes_nano_probe_stop(&s->probe)) return fail(s,s->probe.first_error);
    s->done=1;
    return fes_nano_fabric_stopped(s);
}
