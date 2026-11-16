/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_mac.h"

static int retire(struct fes_nano_mac *s,int error)
{
    s->failed=1;
    if (!s->first_error) s->first_error=error ? error : -1;
    /* A possibly released device remains reserved until a fresh trusted boot.
     * Never reset, retry, reclaim buffers or invent containment on failure. */
    return -1;
}
static int healthy(struct fes_nano_mac *s)
{
    if (!s || !s->initialized || s->failed) return -1;
    if (!s->attempted || !s->ops.scope || !s->ops.acquire || !s->ops.confirm || !s->ops.stop ||
        s->active>1 || s->confirmed>1 || s->reservation!=s->active ||
        (s->confirmed && !s->active)) return retire(s,-1);
    return 0;
}
static int scope(struct fes_nano_mac *s,unsigned active,unsigned confirmed)
{
    int error=s->ops.scope(s->ops.context,&s->binding,active,confirmed);
    return error ? retire(s,error) : 0;
}
int fes_nano_mac_init(struct fes_nano_mac *s,struct fes_nano_mac_binding binding,
    struct fes_nano_mac_ops ops,unsigned qualifications)
{
    if (!s || s->attempted) return -1;
    s->attempted=1;
    s->binding=binding;s->ops=ops;
    if (qualifications!=FES_NANO_MAC_QUALIFIED || !ops.scope || !ops.acquire ||
        !ops.confirm || !ops.stop || !binding.owner || !binding.epoch || binding.mac>2 ||
        binding.phy>31 || !binding.phy_id || binding.phy_id==UINT32_MAX ||
        !binding.dma_start || binding.dma_start%0x200000U || binding.dma_size!=0x200000U ||
        binding.dma_start>UINT64_MAX-binding.dma_size || s->active || s->confirmed ||
        s->reservation || s->generation) return retire(s,-22);
    if (scope(s,0,0)) return -1;
    s->initialized=1;
    return 0;
}
int fes_nano_mac_gate(struct fes_nano_mac *s)
{
    if (!s || !s->attempted) return 0;
    if (healthy(s)) return -1;
    if (s->active || s->reservation) return 1;
    return scope(s,0,0);
}
int fes_nano_mac_observe(struct fes_nano_mac *s)
{
    if (!s || !s->attempted) return 0;
    if (healthy(s)) return -1;
    if (s->active && !s->confirmed) return 1;
    return scope(s,s->active,s->confirmed);
}
static void reply(struct fes_nano_mac *s,uint64_t r[4])
{
    unsigned flags=FES_EMBEDDED_MAC_SUPPORTED|FES_EMBEDDED_MAC_PINNED_WINDOW|
        FES_EMBEDDED_MAC_PRIVATE_GENERATION;
    flags|=s->active ? FES_EMBEDDED_MAC_ACTIVE : FES_EMBEDDED_MAC_CONTAINED;
    if (s->confirmed) flags|=FES_EMBEDDED_MAC_CONFIRMED;
    r[0]=FES_EMBEDDED_MAC_OK;r[1]|=flags;r[2]=s->binding.owner;r[3]=s->generation;
}
void fes_nano_mac_sip(struct fes_nano_mac *s,uint64_t op,uint64_t owner,
    uint64_t generation,unsigned configuring,uint64_t r[4])
{
    r[0]=FES_EMBEDDED_MAC_REJECTED;r[1]=(uint64_t)FES_EMBEDDED_MAC_TAG<<32;r[2]=r[3]=0;
    if (op>FES_EMBEDDED_MAC_STOP || (op==FES_EMBEDDED_MAC_QUERY && (owner || generation))) return;
    if (!s || !s->attempted) {r[0]=FES_EMBEDDED_MAC_NOT_READY;return;}
    if (healthy(s)) {r[0]=FES_EMBEDDED_MAC_ERROR;return;}
    if (op!=FES_EMBEDDED_MAC_QUERY && (owner!=s->binding.owner || generation!=s->generation)) return;
    if (configuring) {r[0]=FES_EMBEDDED_MAC_BUSY;return;}
    if ((op==FES_EMBEDDED_MAC_ACQUIRE && s->active) ||
        (op==FES_EMBEDDED_MAC_CONFIRM && (!s->active || s->confirmed))) return;
    r[0]=FES_EMBEDDED_MAC_ERROR;
    unsigned increment=op==FES_EMBEDDED_MAC_ACQUIRE || op==FES_EMBEDDED_MAC_CONFIRM ||
        (op==FES_EMBEDDED_MAC_STOP && s->active);
    /* Keep enough namespace for confirmation and a subsequent checked stop. */
    unsigned needed=op==FES_EMBEDDED_MAC_ACQUIRE ? 3U : op==FES_EMBEDDED_MAC_CONFIRM ? 2U : increment;
    if (needed && s->generation>UINT64_MAX-needed) {retire(s,-75);return;}
    if (scope(s,s->active,s->confirmed)) return;
    unsigned active=s->active,confirmed=s->confirmed;
    int error=0;
    if (op==FES_EMBEDDED_MAC_ACQUIRE) {
        s->active=s->reservation=1; /* before the first potentially releasing callback */
        active=1;confirmed=0;
        error=s->ops.acquire(s->ops.context,&s->binding);
    } else if (op==FES_EMBEDDED_MAC_CONFIRM) {
        confirmed=1;
        error=s->ops.confirm(s->ops.context,&s->binding);
    } else if (op==FES_EMBEDDED_MAC_STOP && s->active) {
        active=confirmed=0;
        error=s->ops.stop(s->ops.context,&s->binding);
    }
    if (error) {retire(s,error);return;}
    if (scope(s,active,confirmed)) return;
    s->active=s->reservation=active;s->confirmed=confirmed;
    if (increment) s->generation++;
    reply(s,r);
}
