/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_TEST_NANO_USB_FIXTURE_H
#define FES_TEST_NANO_USB_FIXTURE_H
/* The actual platform callbacks, cold consumer and SD service run against
 * independent register/PHY reset events. No address here is physical. */
#include "nano_usb_composition.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>
struct hardware {
    uint32_t per0, bridge, clock[64], srs[256], phy[6], phy_address;
    unsigned fault, reads, writes, controller_accesses, reset_writes, host_resets;
    unsigned clock_writes, waits;
    uint32_t usb[512];
    unsigned usb_reads,usb_writes,usb_fault,usb_ready;
    uint32_t producer_bits;
    unsigned producer_stops,master_resets,bridge_writes,master_fault,post_stop_reads;
    struct fes_nano_cold *cold;
    struct fes_nano_usb_platform *usb_owner;
};
struct test {
    struct hardware h;
    struct fes_nano_cold cold;
    struct fes_nano_sd_platform platform;
    struct fes_nano_bridges bridges;
    struct fes_nano_sd service;
    struct fes_nano_usb_platform usb;
    struct fes_nano_usb_composition composition;
    struct fes_nano_boot master_boot;
    struct fes_nano_usb_master master;
};
static inline unsigned phy_index(uint32_t a)
{
    switch (a) {case 0x2000:return 0;case 0x2004:return 1;case 0x2008:return 2;
    case 0x200c:return 3;case 0x2010:return 4;case 0x2080:return 5;default:assert(0);return 0;}
}
static inline int read32(void *context, uintptr_t a, uint32_t *v)
{
    struct hardware *h=context;h->reads++;
    if (a==FES_NANO_COLD_RSTMGR+0x24) {*v=h->per0;return 0;}
    if (a>=FES_NANO_USB_BASE && a<FES_NANO_USB_BASE+0x800) {
        assert(!(h->per0&FES_NANO_USB_RESET));h->usb_reads++;
        if (h->usb_fault==7) return -77;
        *v=h->usb[(a-FES_NANO_USB_BASE)/4];return 0;
    }
    if (a>=FES_NANO_SD_CLKMGR && a<FES_NANO_SD_CLKMGR+0x100) {*v=h->clock[(a-FES_NANO_SD_CLKMGR)/4];return 0;}
    if (a==FES_NANO_SD_SYSMGR+0xfc) {*v=h->fault==1?0:1;return 0;}
    if (a==FES_NANO_SD_SYSMGR+0x1354) {*v=h->fault==2?1:0;return 0;}
    assert(a>=FES_NANO_SD_BASE && a<FES_NANO_SD_BASE+0x400);
    assert(!(h->per0&FES_NANO_COLD_SD_RESET));h->controller_accesses++;
    if (a==FES_NANO_SD_BASE+0x14) *v=h->phy[phy_index(h->phy_address)];
    else *v=h->srs[(a-FES_NANO_SD_BASE)/4];
    return 0;
}
static inline int write32(void *context, uintptr_t a, uint32_t v)
{
    struct hardware *h=context;h->writes++;
    if (a==FES_NANO_COLD_RSTMGR+0x24) {
        h->reset_writes++;
        assert((v&(FES_NANO_COLD_UNUSED&~FES_NANO_USB_RESET))==(FES_NANO_COLD_UNUSED&~FES_NANO_USB_RESET));
        assert((v&~(0xc0U|FES_NANO_USB_RESET))==(h->per0&~(0xc0U|FES_NANO_USB_RESET)));
        if ((v^h->per0)&FES_NANO_USB_RESET) {
            assert(!((v^h->per0)&0xc0U));
            assert(!(v&FES_NANO_USB_RESET) || (v&FES_NANO_USB_RESET)==FES_NANO_USB_RESET);
            if (h->usb_fault==8 && (v&FES_NANO_USB_RESET)) return 0;
            if (h->usb_fault==9 && !(v&FES_NANO_USB_RESET)) v|=FES_NANO_COLD_USB_RAM_RESET;
        }
        if (h->fault==3 && !(v&0x40U)) return 0;
        if (h->fault==4 && !(v&0x80U)) return 0;
        if (h->fault==9 && (v&0x80U)) return 0;
        h->per0=v;return 0;
    }
    if (a>=FES_NANO_USB_BASE && a<FES_NANO_USB_BASE+0x800) {
        assert(!(h->per0&FES_NANO_USB_RESET));h->usb_writes++;
        assert(!(h->usb[8/4]&0x21U));
        unsigned off=(unsigned)(a-FES_NANO_USB_BASE);
        if (off==16) v=1U<<31;
        if (off>=0x500 && off<0x700 && !(off&31U) && (v&(1U<<30))) v&=~(1U<<31);
        h->usb[off/4]=v;return 0;
    }
    assert(a>=FES_NANO_SD_BASE && a<FES_NANO_SD_BASE+0x400);
    assert(!(h->per0&0x80U));h->controller_accesses++;
    assert(a!=FES_NANO_SD_BASE+0x20c); /* firmware issues no card command */
    unsigned off=(unsigned)(a-FES_NANO_SD_BASE);
    if (off==0) {
        h->host_resets++;memset(h->srs,0,sizeof(h->srs));
        h->srs[0]=h->fault==5?1:0;return 0;
    }
    if (off==0x10) {h->phy_address=v;return 0;}
    if (off==0x14) {
        if (h->fault!=6) h->phy[phy_index(h->phy_address)]=v;
        return 0;
    }
    if (off==0x24) {
        if (h->fault==7 && !(v&1U)) return 0;
        v&=~2U;if ((v&1U) && h->fault!=8) v|=2U;
    }
    if (off==0x22c) {
        h->clock_writes++;
        if (h->fault==10) h->srs[0x224/4]=1U;
    }
    h->srs[off/4]=v;return 0;
}
static inline void barrier(void *p) {(void)p;}
static inline void wait_poll(void *p) {((struct hardware *)p)->waits++;}
static inline int cold_read(void *p, unsigned reg, uint32_t *v)
{
    struct hardware *h=p;h->reads++;
    switch(reg) {
    case FES_COLD_CAUSE:*v=4;break;
    case FES_COLD_PER0:*v=h->per0;break;
    case FES_COLD_BRG:*v=h->bridge;break;
    case FES_COLD_CPUS:*v=15;break;
    case FES_COLD_RELEASE:*v=0;break;
    case FES_COLD_BOOT_KIND:*v=4;break;
    case FES_COLD_CPU0_POWER:*v=0x90000021;break;
    case FES_COLD_CPU1_POWER:case FES_COLD_CPU2_POWER:case FES_COLD_CPU3_POWER:*v=0;break;
    case FES_COLD_PCH_REQUEST:case FES_COLD_PCH_STATUS:*v=0x23;break;
    case FES_COLD_BOOT_CONFIG:*v=0x0070021e;break;
    default:assert(0);return -1;
    }
    return 0;
}
static inline int cold_write(void *p,unsigned reg,uint32_t v) {(void)p;(void)reg;(void)v;assert(0);return -1;}
static inline int owns(void *p,uint64_t owner,uint64_t epoch)
{ struct test *t=p;return fes_nano_cold_owns(&t->cold,owner,epoch); }
static inline int bridge_read(void *p,uint32_t *v) {*v=((struct test *)p)->h.bridge;return 0;}
static inline int masters(void *p) {return (((struct test *)p)->h.per0&FES_NANO_MASTER_RESET)==FES_NANO_MASTER_RESET;}
static inline int no_stop(void *p) {(void)p;assert(0);return -1;}
static inline int no_bridge(void *p,uint32_t m,unsigned f) {(void)p;(void)m;(void)f;assert(0);return -1;}
static inline void boot(struct test *t)
{
    memset(t,0,sizeof(*t));t->h.per0=FES_NANO_MASTER_RESET;t->h.bridge=15;
    t->h.cold=&t->cold;t->h.usb_owner=&t->usb;
    t->h.usb[0x40/4]=0x4f54330aU;t->h.usb[0x48/4]=1U<<19|2U<<3|3U<<14;
    t->h.usb[0x4c/4]=1024U<<16;t->h.usb[16/4]=1U<<31;
    t->h.clock[4/4]=0x02030300;t->h.clock[0x44/4]=1<<4;
    t->h.clock[0x48/4]=t->h.clock[0x9c/4]=1<<8;
    t->h.clock[0x6c/4]=t->h.clock[0xc0/4]=64;
    t->h.clock[0x68/4]=t->h.clock[0xb4/4]=0x08000004;
    t->h.clock[0x7c/4]=32;
    struct fes_nano_cold_record r={0};uint8_t chain[32]={0x42};
    r.magic=FES_NANO_COLD_MAGIC;r.version=FES_NANO_COLD_VERSION;r.size=sizeof(r);r.flags=FES_NANO_COLD_FLAGS;
    r.boot=(struct fes_nano_boot_record){.magic=FES_NANO_BOOT_MAGIC,.version=1,.size=sizeof(r.boot),
      .owner=17,.epoch=7,.history=1,.fabric=1,.producers=FES_NANO_PRODUCERS_ALL};
    memcpy(r.boot.chain,chain,32);
    r.first=(struct fes_nano_cold_sample){.cause=4,.per0=FES_NANO_COLD_UNUSED,.bridge=15,.cpus=15,.boot_kind=4,
      .power={0x90000021,0,0,0},.pch_request=0x23,.pch_status=0x23,.boot_config=0x0070021e};
    r.last=r.first;r.last.per0=FES_NANO_MASTER_RESET;r.sd_clock=3;r.checksum=fes_nano_cold_checksum(&r);
    struct fes_nano_cold_io io={&t->h,cold_read,cold_write,barrier,wait_poll};
    assert(!fes_nano_cold_consume(&t->cold,&r,io,chain,17));
    struct fes_nano_bridge_ops raw={t,owns,bridge_read,masters,no_stop,no_bridge,no_bridge};
    assert(!fes_nano_bridges_init(&t->bridges,raw,17,7,1,1));
}
static inline int initialize(struct test *t)
{
    struct fes_nano_sd_io io={&t->h,read32,write32,barrier,wait_poll};
    return fes_nano_sd_platform_init(&t->platform,&t->cold,io,FES_NANO_SD_QUALIFIED);
}
static inline void ready(struct test *t)
{
    boot(t);assert(!initialize(t));assert(!t->h.controller_accesses && !t->h.writes);
    assert(!fes_nano_sd_init(&t->service,&t->bridges,fes_nano_sd_platform_ops(&t->platform),FES_NANO_SD_QUALIFIED));
}
static inline unsigned call(struct test *t,unsigned op)
{
    uint64_t r[4];fes_nano_sd_sip(&t->service,op,op?17:0,op?t->bridges.state.generation:0,0,r);
    assert(r[1]>>32==FES_EMBEDDED_SD_TAG);
    if (!r[0]) {assert(r[2]==17 && r[3]==t->bridges.state.generation);}
    else assert(r[1]==(uint64_t)FES_EMBEDDED_SD_TAG<<32 && !r[2] && !r[3]);
    return (unsigned)r[0];
}
static inline void native_prepare(struct test *t)
{
    /* Model the generic driver's SRS reset/internal-clock setup. The platform
     * has to restore Nano PHY timing even when that setup erased HRS state. */
    memset(t->h.srs,0,sizeof(t->h.srs));memset(t->h.phy,0,sizeof(t->h.phy));
    t->h.srs[0x22c/4]=FES_NANO_SD_IDENT_CLOCK;t->h.srs[0x234/4]=0xffff8033U;
}

static inline int usb_prepare(void *context)
{
    struct hardware *h=context;
    if (h->usb_fault==3) h->per0&=~FES_NANO_COLD_SD_RESET;
    if (h->usb_fault==4) h->usb_owner->generation++;
    return h->usb_fault!=1 && !fes_nano_cold_usb_scope(h->cold,17,7,h->usb_owner->active);
}
static inline int usb_ready(void *context)
{
    struct hardware *h=context;h->usb_ready++;
    if (h->usb_fault==2) return 0; /* injected live PHY readiness failure */
    return usb_prepare(h);
}
static inline void composed(struct test *t)
{
    ready(t);assert(!call(t,FES_EMBEDDED_SD_QUERY));
    struct fes_nano_usb_io io={&t->h,read32,write32,barrier,wait_poll,usb_ready,usb_prepare,0,usb_prepare,0};
    assert(!fes_nano_usb_platform_init(&t->usb,&t->cold,io,FES_NANO_USB_QUALIFIED));
    assert(!fes_nano_usb_composition_init(&t->composition,&t->cold,&t->usb,&t->platform));
    assert(t->cold.devices_bound && !t->h.writes && !t->h.usb_reads);
}
static inline void confirmed(struct test *t)
{
    composed(t);
    assert(!fes_nano_usb_platform_acquire(&t->usb,17,0));
    assert(!fes_nano_usb_platform_confirm(&t->usb,17,1));
    assert(t->usb.generation==2 && t->usb.confirmed);
}
static inline void no_retry(struct test *t)
{
    unsigned reads=t->h.reads,writes=t->h.writes;
    assert(fes_nano_cold_owns(&t->cold,17,7)!=1);
    assert(fes_nano_cold_sd_scope(&t->cold,17,7,t->platform.active)<0);
    assert(fes_nano_cold_usb_scope(&t->cold,17,7,t->usb.active)<0);
    assert(fes_nano_usb_platform_observe(&t->usb,17,t->usb.generation)<0);
    assert(t->h.reads==reads && t->h.writes==writes);
}

#endif
