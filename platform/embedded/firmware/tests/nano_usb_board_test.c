/* SPDX-License-Identifier: BSD-3-Clause */
/* Reuse the independently modeled cold boot/reset/channel fixture. */
#define main usb_lifecycle_fixture_main
#include "nano_usb_platform_test.c"
#undef main
#include "nano_usb_board_fixture.h"
static void initialized(struct board_test *t)
{
    setup(t);
    struct fes_nano_usb_io physical={.context=t,.read=board_read,.write=board_write,.barrier=barrier,.wait=board_wait};
    assert(!fes_nano_usb_board_init(&t->board,&t->base.cold,physical,compiled_pins,FES_NANO_HPS_IO_COUNT,compiled_digest));
    struct fes_nano_usb_io ops=fes_nano_usb_board_ops(&t->board);
    assert(ops.prepare(&t->board)==1);
    assert(!ops.ready(&t->board));assert(!t->requests && !t->base.h.usb_accesses);
    assert(!fes_nano_usb_platform_init(&t->base.usb,&t->base.cold,ops,FES_NANO_USB_QUALIFIED));
}
static void prepared(struct board_test *t)
{
    initialized(t);
    assert(!fes_nano_usb_platform_acquire(&t->base.usb,17,0));
    assert(t->base.usb.active && !t->base.usb.confirmed && t->base.usb.generation==1);
    assert(!t->requests);
}
static unsigned drift_request;
static int drift_read(void *context,uintptr_t address,uint32_t *value)
{
    struct board_test *t=context;
    int error=board_read(context,address,value);
    if (!error && address==FES_NANO_USB_BASE+0x34 &&
        t->requests>=drift_request && !t->pending) {
        if (t->fault==17) {t->clocks[0]^=1;t->fault=0;}
        if (t->fault==18) {t->pins[0]^=1;t->fault=0;}
    }
    return error;
}
static int drift_write(void *context,uintptr_t address,uint32_t value)
{
    struct board_test *t=context;
    int error=board_write(context,address,value);
    if (!error && address==FES_NANO_USB_BASE+12) {
        if (t->fault==19) {t->clocks[0]^=1;t->fault=0;}
        if (t->fault==20) {t->pins[0]^=1;t->fault=0;}
    }
    return error;
}
static void operation_boundaries(void)
{
    for (unsigned fault=17;fault<=22;fault++) {
        struct board_test t;
        if (fault>=21) {
            prepared(&t);host_started(&t);
            assert(!fes_nano_usb_platform_confirm(&t.base.usb,17,1));
            t.fault=fault-4;drift_request=t.requests+1;t.board.physical.read=drift_read;
            assert(fes_nano_usb_platform_observe(&t.base.usb,17,2));
        } else if (fault<=18) {
            prepared(&t);host_started(&t);t.fault=fault;
            drift_request=t.requests+1;t.board.physical.read=drift_read;
            assert(fes_nano_usb_platform_confirm(&t.base.usb,17,1));
        } else {
            initialized(&t);t.fault=fault;t.board.physical.write=drift_write;
            assert(fes_nano_usb_platform_acquire(&t.base.usb,17,0));
        }
        assert(!t.fault && t.board.failed && t.base.usb.failed &&
               t.base.cold.failed && t.base.usb.active && t.base.usb.confirmed==(fault>=21));
        unsigned accesses=t.accesses;
        assert(fes_nano_usb_platform_observe(&t.base.usb,17,t.base.usb.generation));
        assert(fes_nano_usb_platform_stop(&t.base.usb,17,t.base.usb.generation));
        assert(t.accesses==accesses); /* No new observation or recovery after failure. */
    }
}
static int reject_ready_scope(void *p,struct fes_nano_cold *c,uint64_t owner,uint64_t epoch)
{(void)p;(void)c;(void)owner;(void)epoch;return 0;}
static struct fes_nano_usb_platform *changed_platform;
static int (*saved_ready_scope)(void *,struct fes_nano_cold *,uint64_t,uint64_t);
static unsigned changed_field;
static int changed_ready_scope(void *p,struct fes_nano_cold *c,uint64_t owner,uint64_t epoch)
{
    int result=saved_ready_scope(p,c,owner,epoch);
    if (result==1) {
        if (changed_field==0) changed_platform->generation++;
        if (changed_field==1) changed_platform->owner++;
        if (changed_field==2) changed_platform->epoch++;
        if (changed_field==3) changed_platform->confirmed=0;
        if (changed_field==4) changed_platform->active=0;
    }
    return result;
}
static void prepared_observation_admission(void)
{
    struct board_test t;prepared(&t);host_started(&t);
    assert(!fes_nano_usb_platform_confirm(&t.base.usb,17,1));
    struct fes_nano_usb_io ops=fes_nano_usb_board_ops(&t.board);
    unsigned accesses=t.accesses;
    struct fes_nano_cold foreign={0},untouched=foreign;
    assert(!ops.ready_scope(&t.board,&foreign,17,7));
    assert(!memcmp(&foreign,&untouched,sizeof(foreign)));
    assert(!ops.ready_scope(&t.board,&t.base.cold,18,7));
    assert(!ops.ready_scope(&t.board,&t.base.cold,17,8));
    assert(fes_nano_usb_platform_observe(&t.base.usb,18,2));
    assert(fes_nano_usb_platform_observe(&t.base.usb,17,1));
    assert(t.accesses==accesses && !t.board.failed && !t.base.cold.failed);
    t.base.usb.io.ready_scope=reject_ready_scope;
    assert(fes_nano_usb_platform_observe(&t.base.usb,17,2));
    assert(t.accesses==accesses && t.base.usb.failed); /* No weaker path after a failed proof. */
    for (unsigned fault=0;fault<5;fault++) {
        prepared(&t);host_started(&t);assert(!fes_nano_usb_platform_confirm(&t.base.usb,17,1));
        changed_platform=&t.base.usb;saved_ready_scope=t.base.usb.io.ready_scope;changed_field=fault;
        t.base.usb.io.ready_scope=changed_ready_scope;
        assert(fes_nano_usb_platform_observe(&t.base.usb,17,2));
        assert(t.base.usb.failed && t.base.cold.failed);
        accesses=t.accesses;
        assert(fes_nano_usb_platform_observe(&t.base.usb,17,t.base.usb.generation));
        assert(t.accesses==accesses);
    }
}
static unsigned containment_drift;
static int containment_read(void *context,uintptr_t address,uint32_t *value)
{
    struct board_test *t=context;
    int error=board_read(context,address,value);
    if (!error && address==FES_NANO_USB_BASE+16 && containment_drift) {
        unsigned fault=containment_drift;containment_drift=0;
        if (fault==1) t->clocks[0]^=1;
        if (fault==2) t->pins[0]^=1;
        if (fault==3) t->base.h.regs[8/4]|=1U<<5;
        if (fault==4) t->base.h.regs[8/4]|=1U;
        if (fault==5) t->base.h.regs[24/4]=1;
        if (fault==6) t->base.h.regs[12/4]=0;
        if (fault==7) t->base.h.regs[16/4]=0;
        if (fault==8) t->base.usb.generation++;
        if (fault==9) t->base.usb.owner++;
        if (fault==10) t->base.usb.epoch++;
        if (fault==11) t->base.usb.active=0;
        if (fault==12) t->base.usb.confirmed=0;
        if (fault==13) t->base.h.per0|=FES_NANO_COLD_USB_RAM_RESET;
        if (fault==14) t->base.h.per0&=~1U;
        if (fault==15) t->base.h.bridge&=~2U;
    }
    return error;
}
static void master_containment(void)
{
    struct board_test t;prepared(&t);host_started(&t);
    assert(!fes_nano_usb_platform_confirm(&t.base.usb,17,1));
    unsigned requests=t.requests,writes=t.base.h.writes,live=t.board.live_checks;
    t.phy[0]^=1;t.viewport=1U<<26; /* Readiness fails; no PHY access is needed for no-DMA containment. */
    assert(!fes_nano_usb_platform_idle(&t.base.usb,17,2));
    assert(t.requests==requests && t.base.h.writes==writes && t.board.live_checks==live);
    assert(t.base.usb.generation==2 && t.base.usb.active && t.base.usb.confirmed);
    assert(fes_nano_usb_platform_observe(&t.base.usb,17,2));
    assert(t.base.usb.failed && t.base.cold.failed);
    for (unsigned fault=1;fault<=15;fault++) {
        prepared(&t);host_started(&t);assert(!fes_nano_usb_platform_confirm(&t.base.usb,17,1));
        requests=t.requests;writes=t.base.h.writes;
        containment_drift=fault;t.board.physical.read=containment_read;
        assert(fes_nano_usb_platform_idle(&t.base.usb,17,2));
        assert(!containment_drift && t.base.usb.failed && t.base.cold.failed);
        assert(t.requests==requests && t.base.h.writes==writes);
        unsigned accesses=t.accesses;
        assert(fes_nano_usb_platform_idle(&t.base.usb,17,t.base.usb.generation));
        assert(fes_nano_usb_platform_observe(&t.base.usb,17,t.base.usb.generation));
        assert(fes_nano_usb_platform_stop(&t.base.usb,17,t.base.usb.generation));
        assert(t.accesses==accesses);
    }
    prepared(&t);host_started(&t);assert(!fes_nano_usb_platform_confirm(&t.base.usb,17,1));
    unsigned accesses=t.accesses;
    assert(fes_nano_usb_platform_idle(&t.base.usb,18,2));
    assert(fes_nano_usb_platform_idle(&t.base.usb,17,1));
    assert(t.accesses==accesses && !t.base.usb.failed && t.base.cold.valid);
    /* Fault every captured clock and sealed pin in the actual containment path. */
    for (unsigned i=0;i<FES_NANO_HPS_IO_COUNT+13;i++) {
        static const unsigned clocks[]={0,4,0x30,0x88,0x40,0x44,0x48,0x6c,0x68,0x9c,0xc0,0xb4,0x18};
        prepared(&t);host_started(&t);assert(!fes_nano_usb_platform_confirm(&t.base.usb,17,1));
        if (i<FES_NANO_HPS_IO_COUNT) t.pins[(compiled_pins[i].offset-0x1000)/4]^=1;
        else {unsigned n=i-FES_NANO_HPS_IO_COUNT;t.clocks[clocks[n]/4]^=n==12?4U:n==1?0x100U:1U;}
        requests=t.requests;writes=t.base.h.writes;
        assert(fes_nano_usb_platform_idle(&t.base.usb,17,2));
        assert(t.board.failed && t.base.usb.failed && t.base.cold.failed);
        assert(t.requests==requests && t.base.h.writes==writes);
    }
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
#ifndef FES_NANO_USB_BOARD_ENTRY
#define FES_NANO_USB_BOARD_ENTRY main
#endif
int FES_NANO_USB_BOARD_ENTRY(void)
{
    master_containment();completed();live_failures();changed_policy();interruptions();copied_policy_and_admission();operation_boundaries();prepared_observation_admission();
    puts("PASS actual USB board clocks/pins, two-phase host confirmation and USB3320 ULPI/VBUS failures; model only");return 0;
}
