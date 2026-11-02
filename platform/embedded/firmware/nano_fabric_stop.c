/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_fabric_stop.h"
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
static int snapshot(struct fes_nano_fabric_stop *s,struct fes_nano_fabric_snapshot *v)
{
    static const uint32_t offsets[]={FES_EMBEDDED_REG_MAGIC,FES_EMBEDDED_REG_VERSION,FES_EMBEDDED_REG_ABI,
        FES_EMBEDDED_REG_BUILD0,FES_EMBEDDED_REG_BUILD1,FES_EMBEDDED_REG_BUILD2,FES_EMBEDDED_REG_BUILD3,
        FES_EMBEDDED_REG_GENERATION_LO,FES_EMBEDDED_REG_GENERATION_HI,
        FES_EMBEDDED_REG_REQUEST_GENERATION_LO,FES_EMBEDDED_REG_REQUEST_GENERATION_HI,
        FES_EMBEDDED_REG_REQUEST_COOKIE_LO,FES_EMBEDDED_REG_REQUEST_COOKIE_HI,
        FES_EMBEDDED_REG_STATUS,FES_EMBEDDED_REG_PENDING,
        FES_EMBEDDED_REG_ACK_GENERATION_LO,FES_EMBEDDED_REG_ACK_GENERATION_HI,
        FES_EMBEDDED_REG_ACK_COOKIE_LO,FES_EMBEDDED_REG_ACK_COOKIE_HI,0xb0,0xb4,0xb8};
    for (unsigned i=0;i<22;i++) {
        if (scope(s)) return s->first_error;
        s->reads++;
        int error=s->io.read32(s->io.context,offsets[i],&v->words[i]);
        if (error) return fail(s,error);
        if (scope(s)) return s->first_error;
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
        if (!s->disable_issued) {
            /* A never-enabled or already-drained reader needs no command.
             * Every mutable word must agree; reset/status alone is insufficient. */
            unsigned zero=0;
            for (unsigned i=GENERATION_LO;i<=ACK_COOKIE_HI;i++) zero|=v.words[i];
            if (!zero || drained(&v)) { s->last=v;return scope(s); }
            generation=pair(&v,GENERATION_LO);
            if (v.words[STATUS]!=FES_EMBEDDED_STATUS_ENABLED || !generation ||
                pair(&v,REQUEST_LO)!=generation || pair(&v,ACK_LO) || pair(&v,ACK_COOKIE_LO) ||
                s->cookie<=pair(&v,COOKIE_LO)) return fail(s,-71);
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
int fes_nano_fabric_stopped(struct fes_nano_fabric_stop *s)
{
    if (!s || s->failed) return s && s->first_error ? s->first_error : -1;
    if (!s->done) return fail(s,-1);
    struct fes_nano_fabric_snapshot v;
    if (stable(s,&v) || memcmp(&v,&s->last,sizeof(v))) return fail(s,s->first_error ? s->first_error : -1);
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
    if (s->qualified!=1 || s->abi!=0x10203041U || !build || !s->cookie ||
        s->bus_base!=0x95000000U || s->bytes!=8192U ||
        !s->io.owns || !s->io.read32 || !s->io.write32 || !s->io.wait ||
        !s->probe.io.owns || !s->probe.io.read32 || !s->probe.io.write32 || !s->probe.io.wait)
        return fail(s,-22);
    if (reader_stop(s)) return s->first_error;
    if (fes_nano_probe_stop(&s->probe)) return fail(s,s->probe.first_error);
    s->done=1;
    return fes_nano_fabric_stopped(s);
}
