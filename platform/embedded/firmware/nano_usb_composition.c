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
static int snapshot(void *context,uint32_t *released)
{
    struct fes_nano_usb_composition *s=context;
    struct fes_nano_usb_platform *u=s->usb;
    struct fes_nano_sd_platform *d=s->sd;
    if (s->failed || !s->attempted || !u || !d || !s->cold ||
        !s->cold->valid || s->cold->failed || !u->initialized || u->failed ||
        !d->initialized || d->failed || u->cold!=s->cold || d->cold!=s->cold ||
        u->owner!=s->owner || u->epoch!=s->epoch ||
        s->cold->record.boot.owner!=s->owner || s->cold->record.boot.epoch!=s->epoch ||
        u->active>1 || u->confirmed>1 || d->active>1 || d->prepared>1 ||
        (u->confirmed && (!u->active || u->generation<2)) ||
        (d->prepared && !d->active)) return retire(s);
    *released=(u->active ? FES_NANO_COLD_USB_RESET : 0U) |
        (d->active ? FES_NANO_COLD_SD_RESET : 0U);
    return 1;
}
static int observe_usb(void *context)
{
    struct fes_nano_usb_composition *s=context;
    uint32_t released;
    uint64_t generation=s->usb ? s->usb->generation : 0;
    if (snapshot(s,&released)!=1 || !(released & FES_NANO_COLD_USB_RESET) ||
        !s->usb->confirmed || fes_nano_usb_platform_observe(s->usb,s->owner,generation) ||
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
        usb->failed || sd->failed || usb->active || usb->confirmed || usb->generation ||
        sd->active || sd->prepared || cold->devices_attempted) {s->failed=1;return -1;}
    s->cold=cold;s->usb=usb;s->sd=sd;
    s->owner=cold->record.boot.owner;s->epoch=cold->record.boot.epoch;
    struct fes_nano_cold_device_ops ops={s,snapshot,observe_usb};
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
static int master_observe(void *context,uint64_t owner,uint64_t epoch,uint32_t *keep)
{
    struct fes_nano_usb_master *m=context;
    if (!m || !keep || !m->initialized || m->failed || owner!=m->owner || epoch!=m->epoch) return -1;
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
    uint32_t before,after,bits;
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
int fes_nano_usb_master_init(struct fes_nano_usb_master *m,struct fes_nano_usb_composition *s,
    struct fes_nano_master_ops producers)
{
    if (!m || m->attempted) return -1;
    m->attempted=1;
    if (!s || !s->initialized || s->failed || !s->cold || !s->usb || !s->sd ||
        !s->cold->devices_bound || s->cold->devices.context!=s || s->usb->active ||
        s->usb->generation || s->sd->active || !producers.owns || !producers.quiesce ||
        !producers.quiescent) {m->failed=1;m->first_error=-1;return -1;}
    m->composition=s;m->producers=producers;m->owner=s->owner;m->epoch=s->epoch;
    m->initialized=1;
    uint32_t keep;
    if (master_observe(m,m->owner,m->epoch,&keep) || keep) return master_fail(m,-1);
    return 0;
}
struct fes_nano_retained_master_ops fes_nano_usb_master_ops(struct fes_nano_usb_master *m)
{
    return (struct fes_nano_retained_master_ops){m,master_observe,master_stop,master_quiet};
}
