/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_cold.h"
#include <stddef.h>
#include <string.h>

static int fail(struct fes_nano_cold *s) { s->failed = 1; s->valid = 0; return -1; }
static int sample(struct fes_nano_cold *s, struct fes_nano_cold_sample *v)
{
    if (s->io.read(s->io.context,FES_COLD_CAUSE,&v->cause) ||
        s->io.read(s->io.context,FES_COLD_PER0,&v->per0) ||
        s->io.read(s->io.context,FES_COLD_BRG,&v->bridge) ||
        s->io.read(s->io.context,FES_COLD_CPUS,&v->cpus) ||
        s->io.read(s->io.context,FES_COLD_RELEASE,&v->release) ||
        s->io.read(s->io.context,FES_COLD_BOOT_KIND,&v->boot_kind)) return -1;
    for (unsigned i=0;i<4;i++)
        if (s->io.read(s->io.context,FES_COLD_CPU0_POWER+i,&v->power[i])) return -1;
    return s->io.read(s->io.context,FES_COLD_PCH_REQUEST,&v->pch_request) ||
        s->io.read(s->io.context,FES_COLD_PCH_STATUS,&v->pch_status) ||
        s->io.read(s->io.context,FES_COLD_BOOT_CONFIG,&v->boot_config);
}
static int parked(const struct fes_nano_cold_sample *v, unsigned core)
{
    if (core!=0 && core!=2) return 0;
    const uint32_t spares=15U^(1U<<core), channel=1U<<(core+1);
    /* SDM fresh entry parks non-boot CPUs. CPUINRESET is not a live bitmap:
     * the executing EL3 primary's bit can remain set. Require independent
     * boot selection, sole CPU power request/ack and untouched spare power
     * controllers as well as their unreleased reset history. Zero PWRSTAT is
     * a reset-default observation, not an invented universal power-off code.
     * Actual MPIDR/EL checks are in the trusted SPL/BL31 adapters. */
    if (((v->boot_config & FES_NANO_COLD_BOOT_CORE)>>10)!=core ||
        (v->cpus & spares)!=spares || (v->release & 15U) ||
        (v->pch_request & FES_NANO_COLD_CPU_CHANNELS)!=channel ||
        (v->pch_status & FES_NANO_COLD_CPU_CHANNELS)!=channel) return 0;
    for (unsigned i=0;i<4;i++) if (i!=core && v->power[i]) return 0;
    return 1;
}
static int initial(const struct fes_nano_cold_sample *v, unsigned core)
{
    /* Require the SDM's positive boot-kind observation, parked secondary CPUs and
     * any previously open bridge or unused bus master. The reviewed SPL keeps
     * these held from this first sample until BL31. The exact pinned DDR driver
     * defines POR=0, warm=1, cold=2, nCONFIG=3, JTAG=4, RSU=5. Only the four
     * reviewed fresh-entry paths qualify; absence of warm bits alone cannot. */
    return parked(v,core) && !(v->cause & ~FES_NANO_COLD_CAUSE_ALLOWED) &&
        (v->boot_kind==0 || v->boot_kind==2 || v->boot_kind==3 || v->boot_kind==4) &&
        (v->per0 & FES_NANO_COLD_UNUSED) == FES_NANO_COLD_UNUSED &&
        (v->bridge & 15U) == 15U;
}
static int stable(const struct fes_nano_cold_sample *a, const struct fes_nano_cold_sample *b, unsigned core)
{
    return parked(a,core) && parked(b,core) && a->cause == b->cause && a->boot_kind==b->boot_kind &&
        a->release == b->release && a->boot_config==b->boot_config &&
        (a->bridge & 15U) == (b->bridge & 15U);
}
static int io_valid(struct fes_nano_cold_io io)
{ return io.read && io.write && io.barrier && io.wait; }
uint32_t fes_nano_cold_checksum(const struct fes_nano_cold_record *r)
{
    const uint8_t *p = (const uint8_t *)r;
    uint32_t crc = 0xffffffffU;
    for (unsigned i = 0; i < sizeof(*r); i++) {
        uint8_t byte = i >= offsetof(struct fes_nano_cold_record,checksum) &&
            i < offsetof(struct fes_nano_cold_record,checksum)+4 ? 0 : p[i];
        crc ^= byte;
        for (unsigned bit = 0; bit < 8; bit++) crc = (crc >> 1) ^ ((0U-(crc & 1U)) & 0xedb88320U);
    }
    return ~crc;
}
int fes_nano_cold_begin(struct fes_nano_cold *s, struct fes_nano_cold_io io,
                         const uint8_t chain[32], uint64_t owner, uint64_t epoch, unsigned core)
{
    if (!s || s->attempted) return -1;
    s->attempted = 1;
    if (!io_valid(io) || !chain || !owner || !epoch || core > 3) return fail(s);
    s->io = io;
    memset(&s->record,0,sizeof(s->record));
    memset(&s->before,0,sizeof(s->before));
    s->finish_stage=FES_COLD_FINISH_NONE;
    s->sd_clock_before=s->reset_readback=0;
    struct fes_nano_cold_record *r = &s->record;
    r->magic=FES_NANO_COLD_MAGIC; r->version=FES_NANO_COLD_VERSION; r->size=sizeof(*r); r->core=core;
    r->boot=(struct fes_nano_boot_record){.magic=FES_NANO_BOOT_MAGIC,.version=1,.size=sizeof(r->boot),
        .owner=owner,.epoch=epoch,.history=FES_NANO_BOOT_NEVER_ENABLED,
        .fabric=FES_NANO_FABRIC_ABSENT,.producers=FES_NANO_PRODUCERS_ALL};
    memcpy(r->boot.chain,chain,32);
    if (sample(s,&r->first) || !initial(&r->first,core)) return fail(s);
    return 0;
}
static int idle(struct fes_nano_cold *s, uint32_t *state)
{
    for (unsigned i=0;i<FES_NANO_COLD_POLLS;i++) {
        if (s->io.read(s->io.context,FES_COLD_SD_STATE,state)) return fail(s);
        if (!(*state & FES_NANO_COLD_SD_BUSY)) return 0;
        s->io.wait(s->io.context);
    }
    return fail(s);
}
static int hold(struct fes_nano_cold *s, uint32_t mask)
{
    if (s->io.write(s->io.context,FES_COLD_PER0,mask)) return fail(s);
    (s->io.barrier)(s->io.context);
    if (s->io.read(s->io.context,FES_COLD_PER0,&s->reset_readback) ||
        (s->reset_readback & mask) != mask) return fail(s);
    return 0;
}
int fes_nano_cold_finish(struct fes_nano_cold *s, unsigned entry_verified,
                          struct fes_nano_cold_record *out)
{
    if (!s || !out || !s->attempted || s->failed || s->valid) return -1;
    struct fes_nano_cold_record *r=&s->record;
    s->finish_stage=FES_COLD_FINISH_ENTRY;
    if (entry_verified != 1) return fail(s);
    s->finish_stage=FES_COLD_FINISH_SAMPLE;
    if (sample(s,&s->before)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_INITIAL;
    if (!initial(&s->before,r->core)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_STABLE;
    if (!stable(&r->first,&s->before,r->core)) return fail(s);
    /* SD must have been used by the synchronous PIO SPL. Missing clock/reset
     * access or an unexpected DMA mode rejects before peripheral resets. */
    s->finish_stage=FES_COLD_FINISH_SD_RESET;
    if (s->before.per0 & FES_NANO_COLD_SD_RESET) return fail(s);
    s->finish_stage=FES_COLD_FINISH_MODE_READ;
    if (s->io.read(s->io.context,FES_COLD_SD_MODE,&r->sd_mode)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_DMA;
    if (r->sd_mode & 1U) return fail(s);
    s->finish_stage=FES_COLD_FINISH_INT;
    if (s->io.write(s->io.context,FES_COLD_SD_INT,0)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_SIGNAL;
    if (s->io.write(s->io.context,FES_COLD_SD_SIGNAL,0)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_IDLE_BEFORE;
    if (idle(s,&r->sd_before)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_CLOCK_READ;
    if (s->io.read(s->io.context,FES_COLD_SD_CLOCK,&s->sd_clock_before)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_CLOCK_STOP;
    if (s->io.write(s->io.context,FES_COLD_SD_CLOCK,s->sd_clock_before & ~4U)) return fail(s);
    (s->io.barrier)(s->io.context);
    s->finish_stage=FES_COLD_FINISH_IDLE_AFTER;
    if (idle(s,&r->sd_after)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_CLOCK_READBACK;
    if (s->io.read(s->io.context,FES_COLD_SD_CLOCK,&r->sd_clock)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_CLOCK_CHECK;
    if (r->sd_clock & 4U) return fail(s);
    /* The independent idle observation precedes functional and OCP reset. */
    s->finish_stage=FES_COLD_FINISH_FUNCTION_RESET;
    if (hold(s,FES_NANO_MASTER_FUNCTION)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_ALL_RESET;
    if (hold(s,FES_NANO_MASTER_RESET)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_SAMPLE_LAST;
    if (sample(s,&r->last)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_STABLE_LAST;
    if (!stable(&r->first,&r->last,r->core)) return fail(s);
    s->finish_stage=FES_COLD_FINISH_RESET_CHECK;
    if ((r->last.per0 & FES_NANO_MASTER_RESET) != FES_NANO_MASTER_RESET) return fail(s);
    r->flags=FES_NANO_COLD_FLAGS; r->checksum=fes_nano_cold_checksum(r);
    s->finish_stage=FES_COLD_FINISH_COMPLETE;
    s->valid=1; *out=*r;
    return 0;
}
int fes_nano_cold_owns(void *p, uint64_t owner, uint64_t epoch)
{
    struct fes_nano_cold *s=p;
    struct fes_nano_cold_sample now;
    if (!s || !s->valid || s->failed || owner!=s->record.boot.owner || epoch!=s->record.boot.epoch) return 0;
    if (sample(s,&now) || !stable(&s->record.last,&now,s->record.core) ||
        (now.per0 & FES_NANO_MASTER_RESET) != FES_NANO_MASTER_RESET) return fail(s);
    return 1;
}
int fes_nano_cold_consume(struct fes_nano_cold *s, const struct fes_nano_cold_record *r,
                           struct fes_nano_cold_io io, const uint8_t chain[32], uint64_t owner)
{
    if (!s || s->attempted) return -1;
    s->attempted=1;
    if (!r || !io_valid(io) || !chain || r->magic!=FES_NANO_COLD_MAGIC || r->version!=FES_NANO_COLD_VERSION ||
        r->size!=sizeof(*r) || r->checksum!=fes_nano_cold_checksum(r) || r->flags!=FES_NANO_COLD_FLAGS ||
        r->boot.owner!=owner || !r->boot.epoch || memcmp(r->boot.chain,chain,32) ||
        r->boot.magic!=FES_NANO_BOOT_MAGIC || r->boot.version!=1 || r->boot.size!=sizeof(r->boot) ||
        r->boot.reserved || r->boot.reserved_end || r->boot.history!=FES_NANO_BOOT_NEVER_ENABLED ||
        r->boot.fabric!=FES_NANO_FABRIC_ABSENT || r->boot.producers!=FES_NANO_PRODUCERS_ALL ||
        !initial(&r->first,r->core) || !stable(&r->first,&r->last,r->core) ||
        (r->last.per0 & FES_NANO_MASTER_RESET)!=FES_NANO_MASTER_RESET ||
        (r->sd_before & FES_NANO_COLD_SD_BUSY) || (r->sd_after & FES_NANO_COLD_SD_BUSY) ||
        (r->sd_mode & 1U) || (r->sd_clock & 4U)) return fail(s);
    s->io=io; s->record=*r; s->valid=1;
    return fes_nano_cold_owns(s,owner,r->boot.epoch)==1 ? 0 : fail(s);
}
int fes_nano_cold_quiesce(void *p, uint32_t producers)
{
    struct fes_nano_cold *s=p;
    if (!s || producers!=FES_NANO_PRODUCERS_ALL) return -1;
    /* This serial profile never opens a bridge or restarts a device. Active
     * traffic needs a different qualified provider; never manufacture its ACK. */
    return fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)==1 ? 0 : fail(s);
}
int fes_nano_cold_quiescent(void *p, uint32_t *producers)
{
    if (!producers || fes_nano_cold_quiesce(p,FES_NANO_PRODUCERS_ALL)) return -1;
    *producers=FES_NANO_PRODUCERS_ALL;
    return 0;
}
