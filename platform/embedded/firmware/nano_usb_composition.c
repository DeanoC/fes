/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_usb_composition.h"

static int retire(struct fes_nano_usb_composition *s)
{
    s->failed=1;
    if (s->cold) {s->cold->failed=1;s->cold->valid=0;}
    if (s->usb) {s->usb->failed=1;if (!s->usb->first_error) s->usb->first_error=-5;}
    if (s->sd) {s->sd->failed=1;if (!s->sd->first_error) s->sd->first_error=-5;}
    /* Never clear active/prepared state: a failed lease is still reserved. */
    return 0;
}
/* Preserve the full private predicate at each use, without an extra stack
 * frame when the hot tape ownership path calls it directly. */
static inline __attribute__((always_inline)) int snapshot(void *context,uint32_t *released)
{
    struct fes_nano_usb_composition *s=context;
    struct fes_nano_usb_platform *u=s->usb;
    struct fes_nano_sd_platform *d=s->sd;
    if (s->failed || !s->attempted || !u || !d || !s->cold ||
        !s->cold->valid || s->cold->failed || !u->initialized || u->failed ||
        !d->initialized || d->failed || u->cold!=s->cold || d->cold!=s->cold ||
        !s->prepare_master || u->io.prepare_master!=s->prepare_master || u->io.context!=s->usb_context ||
        u->io.contained_scope!=s->contained_scope ||
        u->owner!=s->owner || u->epoch!=s->epoch ||
        s->cold->record.boot.owner!=s->owner || s->cold->record.boot.epoch!=s->epoch ||
        u->active>1 || u->confirmed>1 || d->active>1 || d->prepared>1 ||
        (u->confirmed && (!u->active || u->generation<2)) ||
        (d->prepared && !d->active)) return retire(s);
    *released=(u->active ? FES_NANO_COLD_USB_RESET : 0U) |
        (d->active ? FES_NANO_COLD_SD_RESET : 0U);
    return 1;
}
static int observe_usb_contained(void *context)
{
    struct fes_nano_usb_composition *s=context;
    uint32_t released;
    uint64_t generation=s->usb ? s->usb->generation : 0;
    if (snapshot(s,&released)!=1 || !(released & FES_NANO_COLD_USB_RESET) ||
        !s->usb->confirmed || fes_nano_usb_platform_idle(s->usb,s->owner,generation) ||
        s->usb->generation!=generation || !s->usb->active || !s->usb->confirmed) return retire(s);
    return 1;
}
int fes_nano_usb_composition_init(struct fes_nano_usb_composition *s,
    struct fes_nano_cold *cold,struct fes_nano_usb_platform *usb,struct fes_nano_sd_platform *sd)
{
    if (!s || s->attempted) return -1;
    s->attempted=1;
    /* Invalid pointers must not retire some other boot's platform objects. */
    if (!cold || !cold->valid || cold->failed || !usb || !sd ||
        usb->cold!=cold || sd->cold!=cold || !usb->initialized || !sd->initialized ||
        usb->failed || sd->failed || !usb->io.prepare_master || usb->active || usb->confirmed || usb->generation ||
        sd->active || sd->prepared || cold->devices_attempted) {s->failed=1;return -1;}
    s->cold=cold;s->usb=usb;s->sd=sd;
    s->owner=cold->record.boot.owner;s->epoch=cold->record.boot.epoch;
    s->usb_context=usb->io.context;s->prepare_master=usb->io.prepare_master;
    s->contained_scope=usb->io.contained_scope;
    struct fes_nano_cold_device_ops ops={s,snapshot,observe_usb_contained};
    if (fes_nano_cold_bind_devices(cold,s->owner,s->epoch,ops)) {retire(s);return -1;}
    s->initialized=1;
    return 0;
}

_Static_assert(FES_NANO_MASTER_USB0_PIO_RESET==FES_NANO_COLD_USB_RESET,"retained USB reset pair");
static int master_fail(struct fes_nano_usb_master *m,int error)
{
    if (!m->first_error) m->first_error=error<0 ? error : -1;
    m->failed=1;
    if (m->composition) retire(m->composition);
    return m->first_error;
}
static int producer_policy(struct fes_nano_usb_master *m)
{
    const struct fes_nano_usb_producer_ops *a=&m->producers,*b=&m->producer_policy;
    return a->context==b->context && a->owns==b->owns && a->quiesce==b->quiesce &&
        a->quiescent==b->quiescent && a->snapshot && a->snapshot==b->snapshot;
}
static int master_observe(void *context,uint64_t owner,uint64_t epoch,uint32_t *keep)
{
    struct fes_nano_usb_master *m=context;
    if (!m || !keep || !m->initialized || m->failed || owner!=m->owner || epoch!=m->epoch) return -1;
    if (!producer_policy(m)) return master_fail(m,-1);
    struct fes_nano_usb_composition *s=m->composition;
    if (!s || !s->usb) return master_fail(m,-1);
    uint32_t before,after;
    uint64_t generation=s->usb->generation;
    if (snapshot(s,&before)!=1 || (before & FES_NANO_COLD_SD_RESET) ||
        m->producers.owns(m->producers.context,owner,epoch)!=1) return master_fail(m,-1);
    if ((before & FES_NANO_COLD_USB_RESET) && fes_nano_usb_platform_idle(s->usb,owner,generation))
        return master_fail(m,s->usb->first_error);
    if (m->producers.owns(m->producers.context,owner,epoch)!=1 || snapshot(s,&after)!=1 ||
        before!=after || s->usb->generation!=generation) return master_fail(m,-1);
    *keep=before;
    return 0;
}
static int master_stop(void *context,uint32_t producers)
{
    struct fes_nano_usb_master *m=context;
    if (!m || !m->initialized || m->failed || producers!=FES_NANO_PRODUCERS_ALL) return -1;
    uint32_t before,after;
    uint64_t generation=m->composition->usb->generation;
    if (master_observe(m,m->owner,m->epoch,&before)) return -1;
    int error=m->producers.quiesce(m->producers.context,producers);
    if (error) return master_fail(m,error);
    if (master_observe(m,m->owner,m->epoch,&after) || before!=after ||
        m->composition->usb->generation!=generation) return master_fail(m,-1);
    return 0;
}
static int master_quiet(void *context,uint32_t *producers)
{
    struct fes_nano_usb_master *m=context;
    if (!m || !producers || !m->initialized || m->failed) return -1;
    uint32_t before,after,bits=0;
    uint64_t generation=m->composition->usb->generation;
    if (master_observe(m,m->owner,m->epoch,&before)) return -1;
    int error=m->producers.quiescent(m->producers.context,&bits);
    if (error) return master_fail(m,error);
    if ((bits & ~FES_NANO_PRODUCERS_ALL) ||
        master_observe(m,m->owner,m->epoch,&after) || before!=after ||
        m->composition->usb->generation!=generation) return master_fail(m,-1);
    *producers=bits;
    return 0;
}
/* One complete trusted observation includes CPU/cold ownership, producer bits
 * and fresh USB containment. Paired private reservation/generation checks
 * bracket it without recursively repeating the same hardware observations. */
static int master_snapshot(void *context,uint64_t owner,uint64_t epoch,
                             uint32_t *keep,uint32_t *producers,uint32_t *reset,uint32_t *per0)
{
    struct fes_nano_usb_master *m=context;
    if (!m || !keep || !producers || !reset || !per0 || !m->initialized || m->failed ||
        owner!=m->owner || epoch!=m->epoch) return -1;
    if (!producer_policy(m)) return master_fail(m,-1);
    struct fes_nano_usb_composition *s=m->composition;
    if (!s || !s->usb) return master_fail(m,-1);
    uint64_t generation=s->usb->generation;
    uint32_t before,after;
    struct fes_nano_cold_observation observation={0};
    if (snapshot(s,&before)!=1 || (before & FES_NANO_COLD_SD_RESET))
        return master_fail(m,-1);
    int error=m->producers.snapshot(m->producers.context,owner,epoch,&observation);
    if (error) return master_fail(m,error);
    if ((observation.producers & ~FES_NANO_PRODUCERS_ALL) ||
        (observation.producers & FES_NANO_PRODUCERS_HPS)!=FES_NANO_PRODUCERS_HPS ||
        snapshot(s,&after)!=1 || before!=after || s->usb->generation!=generation ||
        !producer_policy(m)) return master_fail(m,-1);
    *keep=after;*producers=observation.producers;*reset=observation.bridge;*per0=observation.per0;
    return 0;
}
int fes_nano_usb_master_init(struct fes_nano_usb_master *m,struct fes_nano_usb_composition *s,
    struct fes_nano_usb_producer_ops producers)
{
    if (!m || m->attempted) return -1;
    m->attempted=1;
    if (!s || !s->initialized || s->failed || !s->cold || !s->usb || !s->sd ||
        !s->cold->devices_bound || s->cold->devices.context!=s || s->usb->active ||
        s->usb->generation || s->sd->active || !producers.owns || !producers.quiesce ||
        !producers.quiescent || !producers.snapshot) {m->failed=1;m->first_error=-1;return -1;}
    m->composition=s;m->producers=m->producer_policy=producers;m->owner=s->owner;m->epoch=s->epoch;
    m->initialized=1;
    uint32_t keep;
    if (master_observe(m,m->owner,m->epoch,&keep) || keep) return master_fail(m,-1);
    return 0;
}
struct fes_nano_retained_master_ops fes_nano_usb_master_ops(struct fes_nano_usb_master *m)
{
    return (struct fes_nano_retained_master_ops){m,master_observe,master_stop,master_quiet,master_snapshot};
}
int fes_nano_usb_master_binding_matches(struct fes_nano_usb_master *m,const struct fes_nano_bridges *b)
{
    return fes_nano_boot_usb_pio_matches(b,m,master_observe,master_stop,master_quiet,master_snapshot);
}
static inline __attribute__((always_inline)) int shared_valid(void *context,const struct fes_nano_bridges *b,const struct fes_nano_usb_platform *p)
{
    struct fes_nano_usb_master *m=context;
    if (!m || !m->initialized || m->failed || !m->composition || !b || !p || !producer_policy(m)) return 0;
    struct fes_nano_usb_composition *s=m->composition;
    if (!s->initialized || s->failed || s->usb!=p || !s->cold || !s->sd ||
        m->owner!=s->owner || m->epoch!=s->epoch ||
        s->cold->devices.context!=s || !s->cold->devices_bound ||
        s->cold->devices.snapshot!=snapshot || s->cold->devices.usb_contained!=observe_usb_contained ||
        !fes_nano_usb_master_binding_matches(m,b)) return 0;
    uint32_t released;
    if (snapshot(s,&released)!=1 || b->device_lease!=s->sd->active ||
        s->owner!=b->state.owner || s->epoch!=b->epoch ||
        s->cold->control_expected>1 || s->cold->ddr_expected>1 ||
        (s->cold->ddr_expected && !s->cold->control_expected)) return 0;
    uint32_t reset=FES_NANO_BRIDGES_ALL;
    if (s->cold->control_expected) reset&=~FES_NANO_BRIDGES_CONTROL;
    if (s->cold->ddr_expected) reset&=~8U;
    if (b->state.reset!=reset || (s->sd->active && reset!=FES_NANO_BRIDGES_ALL)) return 0;
    if (p->active && !p->confirmed && reset!=FES_NANO_BRIDGES_ALL) return 0;
    return 1;
}
int fes_nano_usb_master_service_valid(struct fes_nano_usb_master *m,const struct fes_nano_bridges *b,
    const struct fes_nano_usb_platform *p)
{
    return shared_valid(m,b,p);
}
struct fes_nano_usb_shared_ops fes_nano_usb_master_service_ops(struct fes_nano_usb_master *m)
{
    return (struct fes_nano_usb_shared_ops){m,shared_valid,
        {fes_nano_usb_platform_observe,fes_nano_usb_platform_acquire,
         fes_nano_usb_platform_confirm,fes_nano_usb_platform_stop,fes_nano_usb_platform_idle}};
}
