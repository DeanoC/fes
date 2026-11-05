/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_usb_fixture.h"

static int producer_stop(void *p,uint32_t bits)
{
    struct test *t=p;assert(bits==FES_NANO_PRODUCERS_ALL);t->h.producer_stops++;
    if (t->h.master_fault==2) return -71;
    t->h.producer_bits=t->h.master_fault==3 ? FES_NANO_PRODUCERS_HPS : bits;
    if (t->h.master_fault==7) t->usb.generation++;
    return 0;
}
static int producer_quiet(void *p,uint32_t *bits)
{
    struct test *t=p;*bits=t->h.producer_bits;
    if (t->h.producer_stops) {
        if (t->h.master_fault==6) t->h.usb[8/4]|=1U<<5;
        if (t->h.master_fault==8) *bits|=0x80000000U;
    }
    if (t->master_boot.producers_stopped && ++t->h.post_stop_reads>=2 && t->h.master_fault==5)
        *bits&=~FES_NANO_PRODUCERS_FPGA;
    return 0;
}
static int master_per0(void *p,uint32_t *value)
{struct test *t=p;t->h.reads++;*value=t->h.per0;return 0;}
static int master_reset(void *p,uint32_t mask)
{
    struct test *t=p;assert(!(mask&FES_NANO_USB_RESET));
    assert(t->h.producer_bits==FES_NANO_PRODUCERS_ALL);
    t->h.master_resets++;t->h.per0|=mask;return 0;
}
static int master_enable(void *p,uint32_t mask,unsigned fabric)
{
    struct test *t=p;assert(fabric==FES_NANO_FABRIC_ABSENT);
    if (mask==2) assert(!fes_nano_cold_control_target(&t->cold,17,7,1));
    else {assert(mask==8);assert(!fes_nano_cold_ddr_target(&t->cold,17,7));}
    t->h.producer_bits=FES_NANO_PRODUCERS_HPS;t->h.bridge&=~mask;t->h.bridge_writes++;return 0;
}
static int master_disable(void *p,uint32_t mask,unsigned fabric)
{
    struct test *t=p;assert(fabric==FES_NANO_FABRIC_ABSENT);
    assert(t->h.producer_bits==FES_NANO_PRODUCERS_ALL && t->usb.active && t->usb.confirmed);
    if (mask==2) assert(!fes_nano_cold_control_target(&t->cold,17,7,0));
    else {assert(mask==10);assert(!fes_nano_cold_ddr_close_target(&t->cold,17,7));}
    t->h.bridge|=mask;t->h.bridge_writes++;return 0;
}
static void master_prepare(struct test *t,unsigned bind)
{
    boot(t);memset(&t->bridges,0,sizeof(t->bridges));
    t->h.producer_bits=FES_NANO_PRODUCERS_HPS;
    struct fes_nano_boot_policy policy={.qualifications=FES_NANO_BOOT_QUALIFIED,
        .history=1,.fabric=1,.producers=FES_NANO_PRODUCERS_ALL};
    memcpy(policy.chain,t->cold.record.boot.chain,32);
    struct fes_nano_master_ops ops={t,owns,producer_stop,producer_quiet,master_per0,master_reset,barrier,barrier};
    struct fes_nano_bridge_ops raw={.context=t,.read_reset=bridge_read,.enable=master_enable,.disable=master_disable};
    assert(!fes_nano_boot_accept(&t->master_boot,&t->bridges,&t->cold.record.boot,&policy,ops,raw));
    assert(!initialize(t));
    assert(!fes_nano_sd_init(&t->service,&t->bridges,fes_nano_sd_platform_ops(&t->platform),FES_NANO_SD_QUALIFIED));
    struct fes_nano_usb_io usb={&t->h,read32,write32,barrier,wait_poll,usb_ready,usb_prepare,0};
    assert(!fes_nano_usb_platform_init(&t->usb,&t->cold,usb,FES_NANO_USB_QUALIFIED));
    assert(!fes_nano_usb_composition_init(&t->composition,&t->cold,&t->usb,&t->platform));
    assert(!fes_nano_usb_master_init(&t->master,&t->composition,ops));
    if (bind) {
        struct fes_nano_retained_master_ops retained=fes_nano_usb_master_ops(&t->master);
        if (bind==2) retained.snapshot=0; /* trusted provider declines the combined path */
        assert(!fes_nano_boot_bind_usb_pio(&t->master_boot,&t->bridges,17,7,retained));
    }
}
static void master_setup(struct test *t,unsigned bind)
{
    master_prepare(t,bind);
    assert(!fes_nano_usb_platform_acquire(&t->usb,17,0));assert(!fes_nano_usb_platform_confirm(&t->usb,17,1));
}
static void master_cycles(void)
{
    struct test t;master_setup(&t,1);
    struct fes_nano_bridge_snapshot state;
    for (unsigned n=0;n<3;n++) {
        unsigned status=call(&t,FES_EMBEDDED_SD_RESUME);
        if(status)fprintf(stderr,"cycle=%u status=%u service=%d boot=%d gen=%llu data=%u drain=%u\n",n,status,t.service.first_error,t.master_boot.first_error,(unsigned long long)t.bridges.state.generation,t.bridges.closure.data_remained_contained,t.bridges.closure.hardware_drained);
        assert(!status);native_prepare(&t);
        assert(!call(&t,FES_EMBEDDED_SD_PREPARE));t.h.srs[0x234/4]=0;
        assert(!call(&t,FES_EMBEDDED_SD_STOP));
        assert(!fes_nano_bridges_open(&t.bridges,17,t.bridges.state.generation,2,&state));
        if (n==2) assert(!fes_nano_bridges_open(&t.bridges,17,state.generation,8,&state));
        t.h.usb[0x500/4]=1U<<31;unsigned writes=t.h.usb_writes;
        struct fes_nano_bridge_closure closure;
        assert(!fes_nano_bridges_close(&t.bridges,17,state.generation,0,n!=2,&closure));
        assert(closure.producers_quiescent && !closure.fabric_acknowledged);
        assert(closure.hardware_drained==(n==2 ? 8U : 0U));
        assert(closure.data_remained_contained==(n!=2));
        assert(t.h.usb[0x500/4]==1U<<31 && t.h.usb_writes==writes);
        assert(!t.h.master_resets && t.usb.generation==2 && t.usb.confirmed);
    }
    assert(t.h.producer_stops==3);
    assert(!fes_nano_usb_platform_stop(&t.usb,17,2));
    unsigned accesses=t.h.usb_reads+t.h.usb_writes;
    assert(!fes_nano_bridges_observe(&t.bridges,&state));
    assert(t.h.usb_reads+t.h.usb_writes==accesses && state.masters_contained);
}
static void master_faults(void)
{
    for (unsigned fault=0;fault<=8;fault++) {
        if (fault==4) continue;
        struct test t;master_setup(&t,1);
        struct fes_nano_bridge_snapshot state;
        assert(!fes_nano_bridges_open(&t.bridges,17,t.bridges.state.generation,2,&state));
        if (fault!=5) assert(!fes_nano_bridges_open(&t.bridges,17,state.generation,8,&state));
        t.h.master_fault=fault;
        if (fault==0) t.h.usb[8/4]|=1U<<5;
        if (fault==1) t.h.usb[16/4]=0;
        unsigned bridge_writes=t.h.bridge_writes;
        struct fes_nano_bridge_closure closure,untouched;
        memset(&closure,0xa5,sizeof(closure));untouched=closure;
        assert(fes_nano_bridges_close(&t.bridges,17,state.generation,0,fault==5,&closure)<0);
        assert(!memcmp(&closure,&untouched,sizeof(closure)) && t.bridges.failed && t.master_boot.failed);
        assert(t.h.bridge_writes==bridge_writes && !t.h.master_resets && t.usb.active);
        if (fault==1 || fault==3) assert(t.master_boot.first_error==-60);
        if (fault==2) assert(t.master_boot.first_error==-71);
        unsigned reads=t.h.reads,writes=t.h.writes,stops=t.h.producer_stops;
        assert(fes_nano_bridges_observe(&t.bridges,&state)<0);
        assert(fes_nano_bridges_close(&t.bridges,17,t.bridges.state.generation,0,0,&closure)<0);
        assert(t.h.reads==reads && t.h.writes==writes && t.h.producer_stops==stops);
    }
    struct test t;master_setup(&t,0);
    struct fes_nano_bridge_snapshot state;
    assert(fes_nano_bridges_observe(&t.bridges,&state)<0 && !t.h.master_resets);
}
static int invalid_retention(void *p,uint64_t owner,uint64_t epoch,uint32_t *keep)
{(void)p;(void)owner;(void)epoch;*keep=FES_NANO_COLD_SD_RESET;return 0;}
static void master_ownership(void)
{
    struct test t;master_setup(&t,1);
    struct fes_nano_retained_master_ops ops=fes_nano_usb_master_ops(&t.master);
    unsigned reads=t.h.reads,writes=t.h.writes;
    uint32_t keep=0x1234;
    uint32_t producers=0x5678;
    assert(ops.observe(ops.context,18,7,&keep)<0 && keep==0x1234);
    assert(ops.observe(ops.context,17,8,&keep)<0 && keep==0x1234);
    assert(ops.snapshot(ops.context,18,7,&keep,&producers)<0 && keep==0x1234 && producers==0x5678);
    assert(ops.snapshot(ops.context,17,8,&keep,&producers)<0 && keep==0x1234 && producers==0x5678);
    assert(fes_nano_usb_platform_idle(&t.usb,17,1)<0);
    assert(fes_nano_boot_bind_usb_pio(&t.master_boot,&t.bridges,17,7,ops)<0);
    assert(t.h.reads==reads && t.h.writes==writes && !t.master_boot.failed);
    struct fes_nano_usb_master duplicate={0};
    assert(fes_nano_usb_master_init(&duplicate,&t.composition,t.master.producers)<0);
    assert(!t.master.failed && t.cold.valid);
    t.master_boot.retained.observe=invalid_retention;
    struct fes_nano_bridge_snapshot state;
    assert(fes_nano_bridges_observe(&t.bridges,&state)<0 && !t.h.master_resets);
}
int main(void)
{
    master_cycles();master_faults();master_ownership();
    struct test legacy;master_setup(&legacy,2);
    struct fes_nano_bridge_snapshot state;
    assert(!legacy.bridges.observe_state && !fes_nano_bridges_observe(&legacy.bridges,&state));
    struct fes_nano_retained_master_ops retained=fes_nano_usb_master_ops(&legacy.master);
    assert(!fes_nano_boot_usb_pio_matches(&legacy.bridges,retained));
    retained.snapshot=0;assert(fes_nano_boot_usb_pio_matches(&legacy.bridges,retained));
    puts("PASS retained USB PIO master/owned SD/control/DDR close cycles and failures; injected hardware only");return 0;
}
