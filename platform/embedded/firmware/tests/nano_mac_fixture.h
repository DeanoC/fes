/* SPDX-License-Identifier: BSD-3-Clause */
/* Injected authority and drain, never a physical supplier. */
#include <assert.h>
#include <stdio.h>
#include <string.h>
#include "nano_mac.h"
struct mac_hardware {
    unsigned active,confirmed,pending,fault,after;
    unsigned scopes,acquires,confirms,stops;
    uint64_t bridge_generation,usb_generation,sd_generation;
};
static inline struct fes_nano_mac_binding mac_binding(void)
{return (struct fes_nano_mac_binding){17,7,0x92000000U,0x200000U,0,4,0x001cc916U};}
static inline int mac_scope(void *context,const struct fes_nano_mac_binding *b,unsigned active,unsigned confirmed)
{
    struct mac_hardware *h=context;
    h->scopes++;
    struct fes_nano_mac_binding expected=mac_binding();
    if (h->fault&1U || ((h->fault&16U)&&h->after) || h->fault&64U ||
        b->owner!=expected.owner || b->epoch!=expected.epoch || b->dma_start!=expected.dma_start ||
        b->dma_size!=expected.dma_size || b->mac!=expected.mac || b->phy!=expected.phy ||
        b->phy_id!=expected.phy_id || active!=h->active || confirmed!=h->confirmed ||
        (!active && h->pending)) return -5;
    return 0;
}
static inline int mac_acquire(void *context,const struct fes_nano_mac_binding *b)
{
    struct mac_hardware *h=context;(void)b;h->acquires++;h->active=1;h->after=1;
    return h->fault&2U ? -6 : 0;
}
static inline int mac_confirm(void *context,const struct fes_nano_mac_binding *b)
{
    struct mac_hardware *h=context;(void)b;h->confirms++;h->confirmed=1;h->after=1;
    return h->fault&4U ? -7 : 0;
}
static inline int mac_stop(void *context,const struct fes_nano_mac_binding *b)
{
    struct mac_hardware *h=context;(void)b;h->stops++;h->after=1;
    if (h->fault&32U) {h->pending=1;return -60;}
    h->active=h->confirmed=0;
    return h->fault&8U ? -8 : 0;
}
static inline struct fes_nano_mac_ops mac_ops(struct mac_hardware *h)
{return (struct fes_nano_mac_ops){h,mac_scope,mac_acquire,mac_confirm,mac_stop};}
static inline void mac_setup(struct fes_nano_mac *s,struct mac_hardware *h)
{
    memset(s,0,sizeof(*s));memset(h,0,sizeof(*h));
    h->bridge_generation=43;h->usb_generation=5;h->sd_generation=42;
    assert(!fes_nano_mac_init(s,mac_binding(),mac_ops(h),FES_NANO_MAC_QUALIFIED));
}
static inline void mac_call(struct fes_nano_mac *s,uint64_t op,uint64_t r[4])
{fes_nano_mac_sip(s,op,op ? 17 : 0,op ? s->generation : 0,0,r);}
static inline void mac_empty_reply(const uint64_t r[4],unsigned status)
{assert(r[0]==status && r[1]==(uint64_t)FES_EMBEDDED_MAC_TAG<<32 && !r[2] && !r[3]);}
