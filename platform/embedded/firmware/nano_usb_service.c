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
        !b->state.generation || b->state.never_enabled>1 || b->state.masters_contained>1 ||
        (b->state.fabric!=FES_NANO_FABRIC_ABSENT && b->state.fabric!=FES_NANO_FABRIC_PRESENT)) return fail(s);
    if (s->shared.valid) {
        if (s->reservation!=p->active ||
            s->ops.observe!=s->shared.platform.observe || s->ops.acquire!=s->shared.platform.acquire ||
            s->ops.confirm!=s->shared.platform.confirm || s->ops.stop!=s->shared.platform.stop ||
            s->shared.valid(s->shared.context,b,p)!=1) return fail(s);
        return 0;
    }
    if (b->state.reset!=FES_NANO_BRIDGES_ALL || b->state.fabric!=FES_NANO_FABRIC_ABSENT ||
        !b->state.masters_contained || b->device_lease!=p->active) return fail(s);
    if (!b->state.never_enabled) {
        uint64_t generation;
        if (fes_nano_bridges_held_return(b,&generation)!=1) return fail(s);
    }
    return 0;
}
static int initialize(struct fes_nano_usb_service *s,struct fes_nano_bridges *b,
    struct fes_nano_usb_platform *p,struct fes_nano_usb_service_ops ops,
    const struct fes_nano_usb_shared_ops *shared)
{
    if (!s || s->attempted) return -1;
    s->attempted=1;s->platform=p;s->bridges=b;
    if (shared) {
        s->shared=*shared;
        if (!shared->context || !shared->valid) return fail(s);
    }
    if (!p || !p->initialized || p->active || p->confirmed || p->generation || !b ||
        !b->initialized || !b->state.never_enabled || b->device_lease || !ops.observe ||
        !ops.acquire || !ops.confirm || !ops.stop) return fail(s);
    s->owner=p->owner;s->epoch=p->epoch;s->ops=ops;
    struct fes_nano_bridge_snapshot observed;
    if (healthy(s) || ops.observe(p,s->owner,0) || fes_nano_bridges_observe(b,&observed)) return fail(s);
    s->initialized=1;return 0;
}
int fes_nano_usb_service_init(struct fes_nano_usb_service *s,struct fes_nano_bridges *b,
    struct fes_nano_usb_platform *p,struct fes_nano_usb_service_ops ops)
{return initialize(s,b,p,ops,0);}
int fes_nano_usb_service_init_shared(struct fes_nano_usb_service *s,struct fes_nano_bridges *b,
    struct fes_nano_usb_platform *p,struct fes_nano_usb_service_ops ops,struct fes_nano_usb_shared_ops shared)
{return initialize(s,b,p,ops,&shared);}
int fes_nano_usb_service_active(const struct fes_nano_usb_service *s)
{
    /* Keep the gate closed after an uncertain release or failed stop. */
    return s && s->attempted && s->platform && s->bridges &&
        (s->platform->active || s->reservation || (s->failed && s->bridges->device_lease));
}
int fes_nano_usb_service_gate(struct fes_nano_usb_service *s)
{
    if (!s || !s->attempted) return 0;
    if (!s->shared.valid) return fes_nano_usb_service_active(s) ? 1 : 0;
    if (!s->initialized || s->failed || healthy(s)) return -1;
    if (s->ops.observe(s->platform,s->owner,s->platform->generation) || healthy(s)) return fail(s);
    return s->platform->active && !s->platform->confirmed ? 1 : 0;
}
static int returned(struct fes_nano_bridges *b)
{
    uint64_t generation;
    return b->state.never_enabled || fes_nano_bridges_held_return(b,&generation)==1 ||
        fes_nano_bridges_window_return(b,&generation)==1;
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
    if ((configuring && (!s->shared.valid || operation!=FES_EMBEDDED_USB_QUERY)) ||
        (!s->shared.valid && b->device_lease && !p->active)) {r[0]=FES_EMBEDDED_USB_BUSY;return;}
    if (healthy(s)) {r[0]=FES_EMBEDDED_USB_ERROR;return;}
    /* Shared mutation still requires an owned, closed fabric. The SD lease is
     * independent and may remain active; live platform scope checks both. */
    if (s->shared.valid && operation!=FES_EMBEDDED_USB_QUERY &&
        (b->state.reset!=FES_NANO_BRIDGES_ALL || !returned(b))) {r[0]=FES_EMBEDDED_USB_BUSY;return;}
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
        if (s->shared.valid) s->reservation=1;
        else b->device_lease=1;
        error=s->ops.acquire(p,s->owner,p->generation);
    } else if (operation==FES_EMBEDDED_USB_CONFIRM) {
        if (!p->active || p->confirmed) return;
        error=s->ops.confirm(p,s->owner,p->generation);
    } else if (operation==FES_EMBEDDED_USB_STOP) {
        error=s->ops.stop(p,s->owner,p->generation);
        if (!error && !p->active) {
            if (s->shared.valid) s->reservation=0;
            else {
                struct fes_nano_bridge_snapshot observed;
                b->device_lease=0;
                if (fes_nano_bridges_observe(b,&observed)) {b->device_lease=1;error=-1;}
            }
        }
    } else error=s->ops.observe(p,s->owner,p->generation);
    if (error || p->generation!=expected_generation || p->active!=expected_active ||
        p->confirmed!=expected_confirmed || healthy(s)) {
        /* A callback may have reset the controller but returned inconsistent
         * state. Keep the shared reservation until a new trusted boot. */
        if (operation!=FES_EMBEDDED_USB_QUERY &&
            (operation==FES_EMBEDDED_USB_ACQUIRE || expected_active || advances)) {
            if (s->shared.valid) s->reservation=1;
            else b->device_lease=1;
        }
        fail(s);r[0]=FES_EMBEDDED_USB_ERROR;return;
    }
    uint32_t flags=FES_EMBEDDED_USB_SUPPORTED;
    if (s->shared.valid) {
        flags|=FES_EMBEDDED_USB_SHARED_PIO;
        if ((b->state.reset & FES_NANO_BRIDGES_DATA)==FES_NANO_BRIDGES_DATA) flags|=FES_EMBEDDED_USB_DATA_HELD;
    } else flags|=FES_EMBEDDED_USB_DATA_HELD|FES_EMBEDDED_USB_CLOSED_ONLY;
    flags|=p->active?FES_EMBEDDED_USB_ACTIVE:FES_EMBEDDED_USB_CONTAINED;
    if (p->confirmed) flags|=FES_EMBEDDED_USB_CONFIRMED;
    r[0]=FES_EMBEDDED_USB_OK;r[1]|=flags;r[2]=s->owner;r[3]=p->generation;
}
