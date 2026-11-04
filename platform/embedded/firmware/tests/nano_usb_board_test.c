/* SPDX-License-Identifier: BSD-3-Clause */
/* Reuse the independently modeled cold boot/reset/channel fixture. */
#define main usb_lifecycle_fixture_main
#include "nano_usb_platform_test.c"
#undef main
#include "nano_usb_board.h"
#include "usb_board_policy.h"
static const struct fes_nano_hps_io_register compiled_pins[FES_NANO_HPS_IO_COUNT]=FES_NANO_USB_BOARD_PINS;
static const uint8_t compiled_digest[32]=FES_NANO_USB_BOARD_DIGEST;
struct board_test {
    struct test base;
    struct fes_nano_usb_board board;
    uint32_t clocks[64],pins[0x200];
    uint8_t phy[64];
    uint32_t viewport;
    unsigned pending,requests,waits,accesses,fail_at,fault;
};
static int board_read(void *context,uintptr_t address,uint32_t *value)
{
    struct board_test *t=context;
    if (++t->accesses==t->fail_at) return -99;
    if (address>=0x10d10000UL && address<0x10d10100UL) {*value=t->clocks[(address-0x10d10000UL)/4];return 0;}
    if (address>=FES_NANO_HPS_IO_BASE+0x1000 && address<=FES_NANO_HPS_IO_BASE+0x14bc) {
        *value=t->pins[(address-FES_NANO_HPS_IO_BASE-0x1000)/4];return 0;
    }
    if (address==FES_NANO_USB_BASE+0xe00) {*value=0;return 0;}
    if (address==FES_NANO_USB_BASE+0x34) {
        assert(!(t->base.h.per0&FES_NANO_USB_RESET));
        if (t->pending && t->fault!=1) {
            if (--t->pending==0) {
                unsigned reg=(t->viewport>>16)&63;
                t->viewport=(1U<<27)|(reg<<16)|t->phy[reg];
                if (t->fault==2) t->viewport^=1U<<16;
                if (t->fault==3) t->viewport|=1U<<31;
            }
        }
        *value=t->viewport;return 0;
    }
    return read32(&t->base.h,address,value);
}
static int board_write(void *context,uintptr_t address,uint32_t value)
{
    struct board_test *t=context;
    if (++t->accesses==t->fail_at) return -99;
    if (address==FES_NANO_USB_BASE+0x34) {
        assert(!(t->base.h.per0&FES_NANO_USB_RESET));
        assert((value&~(0x3fU<<16|1U<<25))==0); /* PHY reads only. */
        assert(!t->pending);
        t->requests++;t->viewport=value|(1U<<26);t->pending=2;return 0;
    }
    return write32(&t->base.h,address,value);
}
static void board_wait(void *context) {((struct board_test *)context)->waits++;}
static void setup(struct board_test *t)
{
    memset(t,0,sizeof(*t));boot(&t->base);
    t->clocks[4/4]=0x02030300;t->clocks[0x44/4]=1U<<4;
    t->clocks[0x48/4]=t->clocks[0x9c/4]=1U<<8;
    t->clocks[0x6c/4]=t->clocks[0xc0/4]=64;
    t->clocks[0x68/4]=t->clocks[0xb4/4]=0x08000004;
    t->clocks[0x7c/4]=0; /* USB readiness must not depend on SDMMC clock gating. */
    for (unsigned i=0;i<FES_NANO_HPS_IO_COUNT;i++)
        t->pins[(compiled_pins[i].offset-0x1000)/4]=compiled_pins[i].value;
    t->phy[0]=0x24;t->phy[1]=4;t->phy[2]=7;t->phy[3]=0;
    t->phy[4]=0x40;t->phy[7]=0;t->phy[0xa]=0x66;t->phy[0x13]=6;
}
static void prepared(struct board_test *t)
{
    setup(t);
    struct fes_nano_usb_io physical={.context=t,.read=board_read,.write=board_write,.barrier=barrier,.wait=board_wait};
    assert(!fes_nano_usb_board_init(&t->board,&t->base.cold,physical,compiled_pins,FES_NANO_HPS_IO_COUNT,compiled_digest));
    struct fes_nano_usb_io ops=fes_nano_usb_board_ops(&t->board);
    assert(ops.prepare(&t->board)==1);
    assert(!ops.ready(&t->board));assert(!t->requests && !t->base.h.usb_accesses);
    assert(!fes_nano_usb_platform_init(&t->base.usb,&t->base.cold,ops,FES_NANO_USB_QUALIFIED));
    assert(!fes_nano_usb_platform_acquire(&t->base.usb,17,0));
    assert(t->base.usb.active && !t->base.usb.confirmed && t->base.usb.generation==1);
    assert(!t->requests);
}
static void host_started(struct board_test *t)
{
    /* Observations left by the real host initialization; this model does not
     * enumerate a USB device or establish electrical acceptance. */
    t->base.h.regs[12/4]=1U<<29|1U<<4;
    t->base.h.regs[20/4]=1;
    t->base.h.regs[0x440/4]=1U<<12;
}
static void completed(void)
{
    struct board_test t;prepared(&t);host_started(&t);
    assert(!fes_nano_usb_platform_confirm(&t.base.usb,17,1));
    assert(t.base.usb.confirmed && t.base.usb.generation==2 && t.board.live_checks==1 && t.requests==16);
    assert(!fes_nano_usb_platform_observe(&t.base.usb,17,2));
    assert(t.board.live_checks==2 && t.requests==32);
    assert(!fes_nano_usb_platform_stop(&t.base.usb,17,2));
    assert(!t.base.usb.confirmed && !t.base.usb.active && t.base.usb.generation==3);
    unsigned requests=t.requests;
    t.phy[0x13]=0; /* Already held stop does not require a powered/live PHY. */
    assert(!fes_nano_usb_platform_stop(&t.base.usb,17,3));
    assert(t.requests==requests && fes_nano_cold_owns(&t.base.cold,17,7)==1);
}
static void live_failures(void)
{
    for (unsigned fault=1;fault<=16;fault++) {
        struct board_test t;prepared(&t);host_started(&t);
        if (fault<=3) t.fault=fault;
        if (fault==4) t.phy[0]^=1;
        if (fault==5) t.phy[2]^=1;
        if (fault==6) t.phy[4]&=~0x40U;
        if (fault==7) t.phy[7]=1;
        if (fault==8) t.phy[0xa]|=0x80U; /* USB3320 external indicator is always high. */
        if (fault==9) t.phy[0x13]=4;
        if (fault==10) t.phy[0xa]&=~0x60U;
        if (fault==11) t.base.h.regs[20/4]=0;
        if (fault==12) t.base.h.regs[8/4]=1U<<5;
        if (fault==13) t.base.h.regs[0x440/4]|=1U<<4;
        if (fault==14) t.viewport=1U<<26;
        if (fault==15) t.phy[0xa]|=8;
        if (fault==16) t.base.h.regs[0x400/4]=1U<<23;
        assert(fes_nano_usb_platform_confirm(&t.base.usb,17,1));
        assert(t.base.usb.failed && !t.base.usb.confirmed && t.base.usb.active && t.base.cold.failed);
        assert(t.waits<=FES_NANO_COLD_POLLS+16);
        unsigned accesses=t.accesses;
        assert(fes_nano_usb_platform_confirm(&t.base.usb,17,1));
        assert(fes_nano_usb_platform_stop(&t.base.usb,17,1));
        assert(t.accesses==accesses);
    }
    struct board_test t;prepared(&t);t.phy[0x13]=0;
    assert(!fes_nano_usb_platform_stop(&t.base.usb,17,1)); /* Failed startup can abort before confirmation. */
}
static void changed_policy(void)
{
    for (unsigned i=0;i<FES_NANO_HPS_IO_COUNT;i++) {
        struct board_test t;prepared(&t);
        t.pins[(compiled_pins[i].offset-0x1000)/4]^=1;
        assert(fes_nano_usb_platform_observe(&t.base.usb,17,1));
        assert(t.board.failed && t.base.usb.failed && t.base.cold.failed);
    }
    static const unsigned clocks[]={0,4,0x30,0x88,0x40,0x44,0x48,0x6c,0x68,0x9c,0xc0,0xb4,0x18};
    for (unsigned i=0;i<13;i++) {
        struct board_test t;prepared(&t);
        t.clocks[clocks[i]/4]^=i==12?4U:i==1?0x100U:1U;
        assert(fes_nano_usb_platform_observe(&t.base.usb,17,1));
        assert(t.board.failed && t.base.cold.failed);
    }
}
static void interruptions(void)
{
    struct board_test base;prepared(&base);host_started(&base);
    unsigned before=base.accesses;
    assert(!fes_nano_usb_platform_confirm(&base.base.usb,17,1));
    unsigned count=base.accesses-before;
    /* Sample the full confirmation sequence, including early/middle/final
     * clock/pin reads and ULPI requests. Dedicated loops mutate every pin/clock. */
    for (unsigned n=1;n<=count;n+=97) {
        struct board_test t;prepared(&t);host_started(&t);t.fail_at=t.accesses+n;
        assert(fes_nano_usb_platform_confirm(&t.base.usb,17,1));
        assert(!t.base.usb.confirmed && t.base.usb.failed && t.base.cold.failed);
    }
}
static void copied_policy_and_admission(void)
{
    struct board_test t;setup(&t);
    struct fes_nano_hps_io_register rows[FES_NANO_HPS_IO_COUNT];
    uint8_t digest[32];memcpy(rows,compiled_pins,sizeof(rows));memcpy(digest,compiled_digest,32);
    struct fes_nano_usb_io physical={.context=&t,.read=board_read,.write=board_write,.barrier=barrier,.wait=board_wait};
    assert(!fes_nano_usb_board_init(&t.board,&t.base.cold,physical,rows,FES_NANO_HPS_IO_COUNT,digest));
    rows[0].value^=1;memset(digest,0,sizeof(digest));
    struct fes_nano_usb_io ops=fes_nano_usb_board_ops(&t.board);
    assert(ops.prepare(&t.board)==1 && t.board.policy_digest[0]==compiled_digest[0]);
    for (unsigned fault=0;fault<5;fault++) {
        setup(&t);memcpy(rows,compiled_pins,sizeof(rows));memcpy(digest,compiled_digest,32);
        if (fault==0) rows[0].mask=0;
        if (fault==1) rows[1].offset=rows[0].offset;
        if (fault==2) rows[0].value=16;
        if (fault==3) memset(digest,0,sizeof(digest));
        unsigned before=t.accesses;
        assert(fes_nano_usb_board_init(&t.board,&t.base.cold,physical,rows,
               fault==4?FES_NANO_HPS_IO_COUNT-1:FES_NANO_HPS_IO_COUNT,digest));
        assert(t.accesses==before && t.board.failed);
    }
}
int main(void)
{
    completed();live_failures();changed_policy();interruptions();copied_policy_and_admission();
    puts("PASS actual USB board clocks/pins, two-phase host confirmation and USB3320 ULPI/VBUS failures; model only");return 0;
}
