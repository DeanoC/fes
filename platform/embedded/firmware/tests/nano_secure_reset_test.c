/* SPDX-License-Identifier: BSD-3-Clause */
/* Real platform/controller/provider bodies; lock, cache and mailbox are models. */
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "nano_bridge_platform.h"

#define PLATFORM_MODEL 5
#define PLAT_SOCFPGA_AGILEX5 5
#define DCCISW 1
typedef unsigned spinlock_t;
static spinlock_t fes_nano_owned_lock;
static struct fes_nano_boot fes_nano_owned_boot;
struct fes_nano_bridges fes_nano_owned_bridges;
static unsigned char fes_nano_reset_attempted;
static unsigned mode, locks, unlocks, scopes, requests, cache_step;
/* A source reservation is deliberately outside reset authority. */
static unsigned char source[128], saved[128];
static int mailbox_status;
static void spin_lock(spinlock_t *p) {assert(!*p);*p=1;locks++;}
static void spin_unlock(spinlock_t *p) {assert(*p);*p=0;unlocks++;}
#if CACHE_FLUSH
static void dcsw_op_all(unsigned op)
{assert(op==DCCISW && !cache_step && fes_nano_owned_lock);cache_step=1;}
static void invalidate_cache_low_el(void) {assert(cache_step==1);cache_step=2;}
static void flush_l3_dcache(void) {assert(cache_step==2);cache_step=3;}
#endif
static void mailbox_reset_cold(void)
{
    assert(fes_nano_owned_lock && !locks && !unlocks);
    assert(fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed);
    assert(fes_nano_reset_attempted==2 && scopes==1 && requests==0);
    assert(cache_step==(CACHE_FLUSH ? 3U : 0U));
    requests++;
    /* The pinned void vendor API can return after transport failure. Neither
     * return value modeled here is accepted as proof that reset happened. */
    assert(mailbox_status==0 || mailbox_status==-5);
}
#include "reset_body.inc"

static int recovery_scope(void *context,uint64_t owner,uint64_t epoch,uint64_t generation)
{
    assert(context==source && owner==17 && epoch==7 && generation==9);
    assert(fes_nano_owned_lock && fes_nano_reset_attempted==1);
    assert(fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed);
    assert(!memcmp(source,saved,sizeof(source)) && !locks && !unlocks);
    scopes++;
    switch(mode) {
    case 1:return 0;
    case 2:return -5;
    case 3:return 2;
    case 4:fes_nano_owned_boot.owner++;break;
    case 5:fes_nano_owned_boot.epoch++;break;
    case 6:fes_nano_owned_bridges.state.owner++;break;
    case 7:fes_nano_owned_bridges.epoch++;break;
    case 8:fes_nano_owned_bridges.state.generation++;break;
    case 9:fes_nano_owned_bridges.initialized=0;break;
    case 10:fes_nano_owned_boot.attempted=0;break;
    case 11:fes_nano_owned_boot.failed=0;break;
    case 12:fes_nano_owned_bridges.failed=0;break;
    case 23: {
        struct fes_nano_reset_ops ops={source,recovery_scope};
        assert(fes_nano_bridge_platform_cold_reset_held(owner,epoch,generation,ops)==-1);
        break;
    }
    }
    return 1;
}

int main(void)
{
    for(mode=0;mode<25;mode++) {
        memset(&fes_nano_owned_boot,0,sizeof(fes_nano_owned_boot));
        memset(&fes_nano_owned_bridges,0,sizeof(fes_nano_owned_bridges));
        memset(source,0xa5,sizeof(source));memcpy(saved,source,sizeof(source));
        fes_nano_owned_boot.attempted=1;fes_nano_owned_boot.owner=17;fes_nano_owned_boot.epoch=7;
        fes_nano_owned_bridges.initialized=1;fes_nano_owned_bridges.state.owner=17;
        fes_nano_owned_bridges.epoch=7;fes_nano_owned_bridges.state.generation=9;
        fes_nano_owned_lock=1;locks=unlocks=scopes=requests=cache_step=0;
        fes_nano_reset_attempted=0;mailbox_status=mode==24 ? -5 : 0;
        struct fes_nano_reset_ops ops={source,recovery_scope};
        uint64_t owner=17,epoch=7,generation=9;
        if(mode==13)ops.scope=0;
        if(mode==14)owner++;
        if(mode==15)epoch++;
        if(mode==16)generation++;
        if(mode==17)owner=0;
        if(mode==18)fes_nano_owned_boot.attempted=0;
        if(mode==19)fes_nano_owned_bridges.initialized=0;
        if(mode==20)epoch=0;
        if(mode==21){fes_nano_owned_boot.failed=1;fes_nano_owned_boot.first_error=-42;}
        if(mode==22)fes_nano_reset_attempted=2;
        int result=fes_nano_bridge_platform_cold_reset_held(owner,epoch,generation,ops);
        unsigned posted=mode==0 || mode==21 || mode==24;
        assert(result==(posted ? -5 : -1) && requests==posted);
        assert(fes_nano_owned_lock && !locks && !unlocks);
        assert(fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed);
        assert(fes_nano_reset_attempted && !memcmp(source,saved,sizeof(source)));
        if(mode==21)assert(fes_nano_owned_boot.first_error==-42);
        assert(fes_nano_owned_boot.first_error==(mode==21 ? -42 : -1));
        unsigned old_scopes=scopes,old_requests=requests;
        assert(fes_nano_bridge_platform_cold_reset_held(17,7,9,ops)==-1);
        assert(scopes==old_scopes && requests==old_requests && !locks && !unlocks);
    }
    /* Ordinary PSCI invalidation retains its lock-free caller contract. */
    fes_nano_owned_lock=0;locks=unlocks=0;
    fes_nano_owned_boot.failed=fes_nano_owned_bridges.failed=0;
    fes_nano_bridge_platform_invalidate();
    assert(locks==1 && unlocks==1 && !fes_nano_owned_lock);
    assert(fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed);
    locks=unlocks=0;fes_nano_owned_lock=1;
    fes_nano_bridge_platform_invalidate_held();
    assert(!locks && !unlocks && fes_nano_owned_lock);
    puts("PASS 27 held-lock reset cases; injected cache and transport only");
    return 0;
}
