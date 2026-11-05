/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_usb_fixture.h"

static void cycle(void)
{
    struct test t;confirmed(&t);
    struct fes_nano_sd_ops sd=fes_nano_sd_platform_ops(&t.platform);
    for (unsigned n=0;n<3;n++) {
        assert(fes_nano_cold_owns(&t.cold,17,7)==1);
        unsigned usb_writes=t.h.usb_writes;
        assert(!sd.resume(sd.context));
        native_prepare(&t);assert(!sd.prepare(sd.context));
        assert(t.usb.confirmed && t.platform.prepared);
        /* USB observes both cached leases, without recursively auditing itself. */
        assert(!fes_nano_usb_platform_observe(&t.usb,17,2));
        t.h.srs[0x234/4]=0;assert(!sd.stop(sd.context));
        assert(t.usb.generation==2 && t.h.usb_writes==usb_writes && !t.bridges.device_lease);
        assert((t.h.per0&FES_NANO_MASTER_RESET)==(FES_NANO_MASTER_RESET&~FES_NANO_USB_RESET));
        /* Exercise existing exact bridge target handoffs, not fabricated close
         * receipts. The physical bridge driver/master policy is not selected. */
        assert(!fes_nano_cold_control_target(&t.cold,17,7,1));t.h.bridge&=~2U;
        assert(fes_nano_cold_owns(&t.cold,17,7)==1);
        assert(!fes_nano_usb_platform_observe(&t.usb,17,2));
        assert(!fes_nano_cold_ddr_target(&t.cold,17,7));t.h.bridge&=~8U;
        assert(fes_nano_cold_owns(&t.cold,17,7)==1);
        assert(!fes_nano_usb_platform_observe(&t.usb,17,2));
        assert(!fes_nano_cold_ddr_close_target(&t.cold,17,7));t.h.bridge|=10U;
        assert(fes_nano_cold_owns(&t.cold,17,7)==1);
    }
    assert(!fes_nano_usb_platform_stop(&t.usb,17,2));
    assert(!t.usb.active && t.usb.generation==3 && t.h.per0==FES_NANO_MASTER_RESET);
    unsigned accesses=t.h.usb_reads+t.h.usb_writes;
    assert(!fes_nano_usb_platform_stop(&t.usb,17,3));
    assert(t.h.usb_reads+t.h.usb_writes==accesses);
    uint32_t quiet=0;assert(!fes_nano_cold_quiescent(&t.cold,&quiet));
    assert(quiet==FES_NANO_PRODUCERS_ALL);
}
static void failures(void)
{
    for (unsigned kind=0;kind<12;kind++) {
        struct test t;confirmed(&t);
        switch(kind) {
        case 0:t.h.usb[8/4]|=1U<<5;break; /* DMA */
        case 1:t.h.usb[8/4]|=1U;break; /* global IRQ */
        case 2:t.h.usb[24/4]=1;break;
        case 3:t.h.usb[12/4]=0;break;
        case 4:t.h.per0|=FES_NANO_COLD_USB_RAM_RESET;break;
        case 5:t.h.per0&=~1U;break; /* unrelated EMAC */
        case 6:
            /* PHY readiness is independent from no-DMA bus containment. A
             * later USB readiness query must reject and poison that owner. */
            t.h.usb_fault=2;
            assert(fes_nano_cold_owns(&t.cold,17,7)==1);
            assert(fes_nano_usb_platform_observe(&t.usb,17,2)<0);
            assert(t.usb.active && t.cold.failed && !t.cold.valid);
            no_retry(&t);continue;
        case 7:t.h.usb_fault=3;break; /* reset changes inside preparation callback */
        case 8:t.h.usb_fault=4;break; /* same active bit, different generation */
        case 9:t.h.usb_fault=7;break;
        case 10:t.h.bridge=13;break; /* unarmed bridge */
        case 11:t.platform.active=1;break; /* cached state disagrees with reset */
        }
        unsigned writes=t.h.writes,usb_reads=t.h.usb_reads;
        int status=fes_nano_cold_owns(&t.cold,17,7);
        if(status>=0) fprintf(stderr,"composition kind=%u status=%d generation=%llu\n",kind,status,(unsigned long long)t.usb.generation);
        assert(status<0 && !t.cold.valid && t.cold.failed);
        assert(t.usb.active && t.h.writes==writes);
        if (kind==4 || kind==5 || kind==10 || kind==11) assert(t.h.usb_reads==usb_reads);
        no_retry(&t);
    }
    struct test t;confirmed(&t);
    unsigned writes=t.h.writes;
    t.h.usb[8/4]|=1U<<5;
    struct fes_nano_sd_ops sd=fes_nano_sd_platform_ops(&t.platform);
    assert(sd.resume(sd.context)<0);
    assert(t.h.writes==writes && t.usb.active && t.composition.failed);
    no_retry(&t);
    confirmed(&t);t.h.usb_fault=8;assert(fes_nano_usb_platform_stop(&t.usb,17,2)<0);
    assert(t.usb.active && t.usb.confirmed && t.cold.failed);no_retry(&t);
    composed(&t);assert(!fes_nano_usb_platform_acquire(&t.usb,17,0));
    assert(fes_nano_cold_owns(&t.cold,17,7)<0 && t.composition.failed);no_retry(&t);
    composed(&t);t.h.usb_fault=9;
    assert(fes_nano_usb_platform_acquire(&t.usb,17,0)<0 && t.usb.active && !t.h.usb_reads);
    no_retry(&t);
    confirmed(&t);
    /* Preserve the old all-producer receipt instead of silently widening it. */
    uint32_t quiet=0x1234;
    assert(fes_nano_cold_quiescent(&t.cold,&quiet)<0 && quiet==0x1234);no_retry(&t);
    confirmed(&t);assert(!fes_nano_cold_control_target(&t.cold,17,7,1));t.h.bridge&=~2U;
    assert(fes_nano_cold_sd_scope(&t.cold,17,7,0)<0);no_retry(&t);
}
static void ownership(void)
{
    struct test t;composed(&t);
    unsigned reads=t.h.reads,writes=t.h.writes;
    assert(fes_nano_cold_owns(&t.cold,18,7)==0);
    assert(fes_nano_cold_usb_scope(&t.cold,17,8,0)<0);
    assert(fes_nano_cold_sd_scope(&t.cold,18,7,0)<0);
    assert(fes_nano_usb_platform_acquire(&t.usb,18,0)<0);
    assert(t.h.reads==reads && t.h.writes==writes && t.cold.valid);
    struct fes_nano_usb_composition second={0};
    assert(fes_nano_usb_composition_init(&second,&t.cold,&t.usb,&t.platform)<0);
    assert(t.cold.valid && t.cold.devices.context==&t.composition);
    struct test other;ready(&other);
    struct fes_nano_usb_composition foreign={0};
    assert(fes_nano_usb_composition_init(&foreign,&t.cold,&t.usb,&other.platform)<0);
    assert(t.cold.valid && other.cold.valid);
    confirmed(&t);
    /* A stop-target readback alone cannot release the still-active owner. */
    t.h.per0|=FES_NANO_USB_RESET;
    assert(!fes_nano_cold_usb_stopped(&t.cold,17,7) && t.usb.active && t.usb.confirmed);
    assert(fes_nano_cold_owns(&t.cold,17,7)<0);no_retry(&t);
    /* Reset bits alone cannot grant USB. Default scope remains exclusive. */
    ready(&other);other.h.per0&=~FES_NANO_USB_RESET;
    assert(fes_nano_cold_sd_scope(&other.cold,17,7,0)<0);
}
int main(void)
{
    cycle();failures();ownership();
    puts("PASS composed USB PIO/cold/SD/bridge-target ownership and retained failures; injected registers only");
    return 0;
}
