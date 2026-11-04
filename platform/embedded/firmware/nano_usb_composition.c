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
