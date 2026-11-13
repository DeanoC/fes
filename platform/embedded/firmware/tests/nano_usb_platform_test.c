/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_usb_platform.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>
_Static_assert(FES_NANO_USB_RESET==0x808U,"USB0 module bit3 and packet-RAM/ECC bit11");
struct hardware {
    uint32_t per0,bridge,regs[512];
    uint32_t stuck_release,stuck_stop;
    unsigned reads,writes,usb_accesses,waits,ready_calls;
    unsigned fault, fail_at, accesses;
};
struct test {struct hardware h;struct fes_nano_cold cold;struct fes_nano_usb_platform usb;};
static int read32(void *context,uintptr_t a,uint32_t *v)
{
    struct hardware *h=context;h->reads++;
    if (++h->accesses==h->fail_at) return -99;
    if (a==FES_NANO_COLD_RSTMGR+0x24) {*v=h->per0;return 0;}
    assert(a>=FES_NANO_USB_BASE && a<FES_NANO_USB_BASE+0x800);
    assert(!(h->per0&FES_NANO_USB_RESET));h->usb_accesses++;
    *v=h->regs[(a-FES_NANO_USB_BASE)/4];return 0;
}
static int write32(void *context,uintptr_t a,uint32_t v)
{
    struct hardware *h=context;h->writes++;
    if (++h->accesses==h->fail_at) return -99;
    if (a==FES_NANO_COLD_RSTMGR+0x24) {
        assert((v&~FES_NANO_USB_RESET)==(h->per0&~FES_NANO_USB_RESET));
        if (h->fault==4 && (v&FES_NANO_USB_RESET)) return 0;
        if (h->fault==5 && !(v&FES_NANO_USB_RESET)) return 0;
        if (!(v&0x808U)) v|=h->stuck_release;
        if ((v&0x808U)==0x808U) v&=~h->stuck_stop;
        h->per0=v;return 0;
    }
    assert(a>=FES_NANO_USB_BASE && a<FES_NANO_USB_BASE+0x800);
    assert(!(h->per0&FES_NANO_USB_RESET));h->usb_accesses++;
    assert(!(h->regs[8/4]&0x21U)); /* Never rewrite a foreign DMA/IRQ owner. */
    unsigned off=(unsigned)(a-FES_NANO_USB_BASE);
    if (off>=0x500 && off<0x700 && !(off&31U) && (v&(1U<<30)) && h->fault!=1) v&=~(1U<<31);
    if (off==16 && h->fault!=2) v=1U<<31;
    if (off==0x440 && h->fault==3) v|=1U<<12;
    h->regs[off/4]=v;return 0;
}
static void barrier(void *p) {(void)p;}
static void wait_poll(void *p) {((struct hardware *)p)->waits++;}
static int ready(void *p) {struct hardware *h=p;h->ready_calls++;return h->fault==6?0:1;}
static int cold_read(void *p,unsigned reg,uint32_t *v)
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
static int cold_write(void *p,unsigned r,uint32_t v) {(void)p;(void)r;(void)v;assert(0);return -1;}
static void boot(struct test *t)
{
    memset(t,0,sizeof(*t));t->h.per0=FES_NANO_MASTER_RESET|0x400000U;t->h.bridge=15;
    t->h.regs[0x40/4]=0x4f54330aU;t->h.regs[0x48/4]=1U<<19|2U<<3|3U<<14;
    t->h.regs[0x4c/4]=1024U<<16;t->h.regs[16/4]=1U<<31;
    struct fes_nano_cold_record r={0};uint8_t chain[32]={0x42};
    r.magic=FES_NANO_COLD_MAGIC;r.version=FES_NANO_COLD_VERSION;r.size=sizeof(r);r.flags=FES_NANO_COLD_FLAGS;
    r.boot=(struct fes_nano_boot_record){.magic=FES_NANO_BOOT_MAGIC,.version=1,.size=sizeof(r.boot),
      .owner=17,.epoch=7,.history=1,.fabric=1,.producers=FES_NANO_PRODUCERS_ALL};
    memcpy(r.boot.chain,chain,32);
    r.first=(struct fes_nano_cold_sample){.cause=4,.per0=FES_NANO_COLD_UNUSED,.bridge=15,.cpus=15,.boot_kind=4,
      .power={0x90000021,0,0,0},.pch_request=0x23,.pch_status=0x23,.boot_config=0x0070021e};
    r.last=r.first;r.last.per0=t->h.per0;r.sd_clock=3;r.checksum=fes_nano_cold_checksum(&r);
    struct fes_nano_cold_io io={&t->h,cold_read,cold_write,barrier,wait_poll};
    assert(!fes_nano_cold_consume(&t->cold,&r,io,chain,17));
}
static struct fes_nano_usb_io io(struct test *t)
{ return (struct fes_nano_usb_io){&t->h,read32,write32,barrier,wait_poll,ready,ready,0,ready,0}; }
static void init(struct test *t)
{
    boot(t);assert(!fes_nano_usb_platform_init(&t->usb,&t->cold,io(t),FES_NANO_USB_QUALIFIED));
    assert(!t->h.writes && !t->h.usb_accesses);
}
static void active(struct test *t)
{ init(t);assert(!fes_nano_usb_platform_acquire(&t->usb,17,0));assert(t->usb.active && t->usb.generation==1); }
static void no_retry(struct test *t)
{
    unsigned reads=t->h.reads,writes=t->h.writes,ready_calls=t->h.ready_calls;
    assert(fes_nano_usb_platform_observe(&t->usb,17,t->usb.generation));
    assert(fes_nano_usb_platform_acquire(&t->usb,17,t->usb.generation));
    assert(fes_nano_usb_platform_confirm(&t->usb,17,t->usb.generation));
    assert(fes_nano_usb_platform_stop(&t->usb,17,t->usb.generation));
    assert(t->h.reads==reads && t->h.writes==writes && t->h.ready_calls==ready_calls);
}
static void cycles(void)
{
    struct test t;init(&t);
    assert(!fes_nano_usb_platform_stop(&t.usb,17,0));assert(!t.h.writes && !t.h.usb_accesses);
    for (unsigned i=0;i<3;i++) {
        uint64_t generation=t.usb.generation;
        assert(!fes_nano_usb_platform_acquire(&t.usb,17,generation));
        assert(!fes_nano_usb_platform_observe(&t.usb,17,generation+1));
        assert((t.h.per0&FES_NANO_MASTER_RESET)==(FES_NANO_MASTER_RESET&~0x808U));
        unsigned reads=t.h.reads,writes=t.h.writes,ready_calls=t.h.ready_calls;
        assert(fes_nano_usb_platform_stop(&t.usb,18,generation+1));
        assert(fes_nano_usb_platform_stop(&t.usb,17,generation));
        assert(fes_nano_usb_platform_acquire(&t.usb,17,generation+1));
        assert(t.h.reads==reads && t.h.writes==writes && t.h.ready_calls==ready_calls);
        t.h.regs[0x500/4]=1U<<31;t.h.regs[0x560/4]=1U<<31;t.h.regs[0x440/4]=1U<<12;
        assert(!fes_nano_usb_platform_stop(&t.usb,17,generation+1));
        assert(!t.usb.active && t.usb.generation==generation+2 && !t.h.waits);
        assert((t.h.per0&FES_NANO_MASTER_RESET)==FES_NANO_MASTER_RESET);
        assert(fes_nano_cold_owns(&t.cold,17,7)==1);
        writes=t.h.writes;unsigned accesses=t.h.usb_accesses;
        assert(!fes_nano_usb_platform_stop(&t.usb,17,generation+2));
        assert(t.h.writes==writes && t.h.usb_accesses==accesses);
    }
}
static void failures(void)
{
    for (unsigned fault=1;fault<=12;fault++) {
        struct test t;active(&t);t.h.fault=fault;
        if (fault==1) t.h.regs[0x500/4]=1U<<31;
        if (fault==7) t.h.regs[8/4]=1U<<5;
        if (fault==8) t.h.regs[8/4]=1;
        if (fault==9) t.h.regs[12/4]=0;
        if (fault==10) t.h.regs[16/4]=0;
        if (fault==11) t.h.per0&=~FES_NANO_COLD_SD_RESET;
        if (fault==12) t.h.bridge=13;
        if (fault==5) { /* Release failure is exercised on a fresh closed scope. */
            init(&t);t.h.fault=fault;
            assert(fes_nano_usb_platform_acquire(&t.usb,17,0));
        } else assert(fes_nano_usb_platform_stop(&t.usb,17,1));
        assert(t.usb.failed && t.cold.failed && t.usb.active);
        assert(t.h.waits<=FES_NANO_COLD_POLLS);no_retry(&t);
    }
    struct test t;active(&t);
    assert(fes_nano_cold_owns(&t.cold,17,7)!=1); /* No USB-to-FPGA authority. */
    assert(t.cold.failed);
    assert(fes_nano_usb_platform_observe(&t.usb,17,1));no_retry(&t);
}
static void admission(void)
{
    for (unsigned bit=0;bit<4;bit++) {
        struct test t;boot(&t);unsigned reads=t.h.reads;
        assert(fes_nano_usb_platform_init(&t.usb,&t.cold,io(&t),FES_NANO_USB_QUALIFIED^(1U<<bit)));
        assert(t.h.reads==reads && !t.h.writes);no_retry(&t);
    }
    for (unsigned fault=0;fault<5;fault++) {
        struct test t;init(&t);
        if (fault==0) t.h.regs[0x40/4]=0;
        if (fault==1) t.h.regs[0x48/4]&=~(1U<<19);
        if (fault==2) t.h.regs[0x4c/4]=128U<<16;
        if (fault==3) t.h.regs[8/4]=1U<<5;
        if (fault==4) t.h.regs[0x520/4]=1U<<31;
        assert(fes_nano_usb_platform_acquire(&t.usb,17,0));
        assert(t.usb.failed && t.usb.active);no_retry(&t);
    }
}
static void interrupted(void)
{
    for (unsigned stopping=0;stopping<2;stopping++) {
        struct test control;if (stopping) active(&control);else init(&control);
        unsigned start=control.h.accesses;
        assert(!(stopping ? fes_nano_usb_platform_stop(&control.usb,17,1) :
                 fes_nano_usb_platform_acquire(&control.usb,17,0)));
        unsigned count=control.h.accesses-start;
        for (unsigned n=1;n<=count;n++) {
            struct test t;if (stopping) active(&t);else init(&t);
            t.h.fail_at=t.h.accesses+n;
            assert(stopping ? fes_nano_usb_platform_stop(&t.usb,17,1) : fes_nano_usb_platform_acquire(&t.usb,17,0));
            assert(t.usb.failed && t.cold.failed);no_retry(&t);
        }
    }
}
static void paired_resets(void)
{
    /* Independent documented bit positions: held packet RAM matches the
     * physical zero-FIFO failure state. Neither half is a valid lease. */
    const uint32_t halves[]={1U<<3,1U<<11};
    for (unsigned i=0;i<2;i++) {
        struct test t;init(&t);t.h.stuck_release=halves[i];
        unsigned accesses=t.h.usb_accesses;
        assert(fes_nano_usb_platform_acquire(&t.usb,17,0));
        assert(t.usb.failed && t.cold.failed && t.usb.active && !t.usb.generation);
        assert((t.h.per0&0x808U)==halves[i] && t.h.usb_accesses==accesses);
        assert((t.h.per0&~0x808U)==((FES_NANO_MASTER_RESET|0x400000U)&~0x808U));
        no_retry(&t);

        active(&t);t.h.stuck_stop=halves[i];
        assert(fes_nano_usb_platform_stop(&t.usb,17,1));
        assert(t.usb.failed && t.cold.failed && t.usb.active && t.usb.generation==1);
        assert((t.h.per0&0x808U)==(0x808U&~halves[i]));
        no_retry(&t);

        active(&t);t.h.per0|=halves[i];accesses=t.h.usb_accesses;
        assert(fes_nano_usb_platform_observe(&t.usb,17,1));
        assert(t.usb.failed && t.cold.failed && t.h.usb_accesses==accesses);
        no_retry(&t);
    }
}
static void confirmation(void)
{
    struct test t;active(&t);
    assert(!t.usb.confirmed);
    assert(!fes_nano_usb_platform_confirm(&t.usb,17,1));
    assert(t.usb.confirmed && t.usb.generation==2);
    unsigned reads=t.h.reads,writes=t.h.writes,calls=t.h.ready_calls;
    assert(fes_nano_usb_platform_confirm(&t.usb,17,2));
    assert(fes_nano_usb_platform_stop(&t.usb,17,1));
    assert(t.h.reads==reads && t.h.writes==writes && t.h.ready_calls==calls);
    assert(!fes_nano_usb_platform_observe(&t.usb,17,2));
    assert(!fes_nano_usb_platform_stop(&t.usb,17,2));
    assert(!t.usb.active && !t.usb.confirmed && t.usb.generation==3);
}
int main(void) {cycles();failures();admission();interrupted();paired_resets();confirmation();puts("PASS USB-only trusted acquire/observe/drain/reset, stale ownership and interrupted MMIO; no hardware acceptance");return 0;}
