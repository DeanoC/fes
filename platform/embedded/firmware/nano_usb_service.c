/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_usb_service.h"

static int fail(struct fes_nano_usb_service *s)
{
    s->failed=1;
    if (s->bridges) s->bridges->failed=1;
    if (s->platform) {
        s->platform->failed=1;
        if (s->platform->cold) {s->platform->cold->failed=1;s->platform->cold->valid=0;}
    }
    return -1;
}
static int healthy(struct fes_nano_usb_service *s)
{
    struct fes_nano_usb_platform *p=s->platform;
    struct fes_nano_bridges *b=s->bridges;
    if (s->failed || !p || !b || p->failed || !p->initialized || !b->initialized || b->failed ||
        p->owner!=s->owner || p->epoch!=s->epoch || b->state.owner!=s->owner || b->epoch!=s->epoch ||
        b->state.reset!=FES_NANO_BRIDGES_ALL || b->state.fabric!=FES_NANO_FABRIC_ABSENT ||
        !b->state.masters_contained || b->device_lease!=p->active || !b->state.generation) return fail(s);
    if (!b->state.never_enabled) {
        uint64_t generation;
        if (fes_nano_bridges_held_return(b,&generation)!=1) return fail(s);
    }
    return 0;
}
int fes_nano_usb_service_init(struct fes_nano_usb_service *s,struct fes_nano_bridges *b,
                             struct fes_nano_usb_platform *p,struct fes_nano_usb_service_ops ops)
{
    if (!s || s->attempted) return -1;
    s->attempted=1;s->platform=p;s->bridges=b;
    if (!p || !p->initialized || p->active || p->confirmed || p->generation || !b ||
        !b->initialized || !b->state.never_enabled || b->device_lease || !ops.observe ||
        !ops.acquire || !ops.confirm || !ops.stop) return fail(s);
    s->owner=p->owner;s->epoch=p->epoch;s->ops=ops;
    struct fes_nano_bridge_snapshot observed;
    if (healthy(s) || ops.observe(p,s->owner,0) || fes_nano_bridges_observe(b,&observed)) return fail(s);
    s->initialized=1;return 0;
}
int fes_nano_usb_service_active(const struct fes_nano_usb_service *s)
{
    /* Keep the gate closed after an uncertain release or failed stop. */
    return s && s->attempted && s->platform && s->bridges &&
        (s->platform->active || (s->failed && s->bridges->device_lease));
}
void fes_nano_usb_service_sip(struct fes_nano_usb_service *s,uint64_t operation,
                              uint64_t owner,uint64_t generation,unsigned configuring,uint64_t r[4])
{
    r[0]=FES_EMBEDDED_USB_REJECTED;r[1]=(uint64_t)FES_EMBEDDED_USB_TAG<<32;r[2]=r[3]=0;
    if (operation>FES_EMBEDDED_USB_STOP || (operation==FES_EMBEDDED_USB_QUERY && (owner||generation))) return;
    if (!s || !s->attempted) {r[0]=FES_EMBEDDED_USB_NOT_READY;return;}
    if (!s->initialized || s->failed) {r[0]=FES_EMBEDDED_USB_ERROR;return;}
    struct fes_nano_usb_platform *p=s->platform;
    struct fes_nano_bridges *b=s->bridges;
    if (operation!=FES_EMBEDDED_USB_QUERY && (owner!=s->owner || generation!=p->generation)) return;
    if (configuring || (b->device_lease && !p->active)) {r[0]=FES_EMBEDDED_USB_BUSY;return;}
    if (healthy(s)) {r[0]=FES_EMBEDDED_USB_ERROR;return;}
    uint64_t expected_generation=p->generation;
    unsigned expected_active=p->active,expected_confirmed=p->confirmed;
    unsigned advances=operation==FES_EMBEDDED_USB_ACQUIRE || operation==FES_EMBEDDED_USB_CONFIRM ||
        (operation==FES_EMBEDDED_USB_STOP && p->active);
    if (advances) {
        if (expected_generation==UINT64_MAX) {fail(s);r[0]=FES_EMBEDDED_USB_ERROR;return;}
        expected_generation++;
    }
    if (operation==FES_EMBEDDED_USB_ACQUIRE) {expected_active=1;expected_confirmed=0;}
    if (operation==FES_EMBEDDED_USB_CONFIRM) expected_confirmed=1;
    if (operation==FES_EMBEDDED_USB_STOP) expected_active=expected_confirmed=0;
    int error=0;
    if (operation==FES_EMBEDDED_USB_ACQUIRE) {
        if (p->active) return;
        /* Reserve before the platform's first potentially mutating callback. */
        b->device_lease=1;
        error=s->ops.acquire(p,s->owner,p->generation);
    } else if (operation==FES_EMBEDDED_USB_CONFIRM) {
        if (!p->active || p->confirmed) return;
        error=s->ops.confirm(p,s->owner,p->generation);
    } else if (operation==FES_EMBEDDED_USB_STOP) {
        error=s->ops.stop(p,s->owner,p->generation);
        if (!error && !p->active) {
            struct fes_nano_bridge_snapshot observed;
            b->device_lease=0;
            if (fes_nano_bridges_observe(b,&observed)) {b->device_lease=1;error=-1;}
        }
    } else error=s->ops.observe(p,s->owner,p->generation);
    if (error || p->generation!=expected_generation || p->active!=expected_active ||
        p->confirmed!=expected_confirmed || healthy(s)) {
        /* A callback may have reset the controller but returned inconsistent
         * state. Keep the shared reservation until a new trusted boot. */
        if (operation!=FES_EMBEDDED_USB_QUERY &&
            (operation==FES_EMBEDDED_USB_ACQUIRE || expected_active || advances)) b->device_lease=1;
        fail(s);r[0]=FES_EMBEDDED_USB_ERROR;return;
    }
    uint32_t flags=FES_EMBEDDED_USB_SUPPORTED|FES_EMBEDDED_USB_DATA_HELD|FES_EMBEDDED_USB_CLOSED_ONLY;
    flags|=p->active?FES_EMBEDDED_USB_ACTIVE:FES_EMBEDDED_USB_CONTAINED;
    if (p->confirmed) flags|=FES_EMBEDDED_USB_CONFIRMED;
    r[0]=FES_EMBEDDED_USB_OK;r[1]|=flags;r[2]=s->owner;r[3]=p->generation;
}
