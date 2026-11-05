/* SPDX-License-Identifier: BSD-3-Clause */
/* Reuse the real selected boot include and physical board register fixture. */
#define main shared_boot_fixture_main
#include "nano_usb_shared_boot_test.c"
#undef main
static unsigned fault,inject,fake_containment_called,fake_prepare_called,fake_snapshot_called;
static int fake_snapshot(void *p,uint64_t owner,uint64_t epoch,struct fes_nano_cold_observation *out)
{(void)p;(void)owner;(void)epoch;(void)out;fake_snapshot_called++;return 0;}
static int fake_prepare(void *p) {(void)p;fake_prepare_called++;return 1;}
static int fake_containment(struct fes_nano_usb_platform *p,uint64_t owner,uint64_t generation)
{(void)p;(void)owner;(void)generation;fake_containment_called++;return 0;}
static unsigned physical_reads,fail_read;
static unsigned late_cold_fault;
static int (*saved_cold_read)(void *,unsigned,uint32_t *);
static int injected_cold_read(void *p,unsigned reg,uint32_t *value)
{
    int error=saved_cold_read(p,reg,value);
    if (error) return error;
    if (late_cold_fault==49 && reg==FES_COLD_CPU1_POWER) *value=1;
    if (late_cold_fault==50 && reg==FES_COLD_CPUS) *value&=~2U;
    if (late_cold_fault==51 && reg==FES_COLD_CAUSE) *value^=1U;
    if (late_cold_fault==52 && reg==FES_COLD_RELEASE) *value=2U;
    if (late_cold_fault==53 && reg==FES_COLD_BOOT_KIND) *value=1;
    if (late_cold_fault==54 && reg==FES_COLD_BOOT_CONFIG) *value^=0x400U;
    if (late_cold_fault==55 && reg==FES_COLD_PCH_REQUEST) *value^=2U;
    if (late_cold_fault==56 && reg==FES_COLD_PCH_STATUS) *value^=2U;
    return 0;
}
static int (*saved_read)(void *,uintptr_t,uint32_t *);
static int injected_read(void *p,uintptr_t address,uint32_t *value)
{
    if (++physical_reads==fail_read) return -99;
    return saved_read(p,address,value);
}
static int replaced_observe(void *p,uint64_t owner,uint64_t epoch,uint32_t *keep)
{(void)p;(void)owner;(void)epoch;*keep=FES_NANO_COLD_SD_RESET;return 0;}
static int producer_sample(void *p,uint32_t *bits)
{
    int error=cold_producer_sample(p,bits);
    if (error || !inject) return error;
    inject=0;
    switch (fault) {
    case 1: model.base.h.usb[8/4]|=1U<<5;break; /* DMA */
    case 2: model.clocks[0]=1;break;
    case 3: model.pins[0]^=1;break;
    case 4: model.phy[0]^=1;break;
    case 5: usb_platform.generation++;break;
    case 6: usb_platform.active=0;break;
    case 7: sd_platform.active=1;break;
    case 8: *bits|=0x80000000U;break;
    case 9: return -71;
    case 10: model.base.h.bridge&=~2U;break;
    case 11: model.base.h.per0|=FES_NANO_USB_RESET;break;
    case 12: model.base.master_boot.retained.observe=replaced_observe;break;
    case 13: model.base.master_boot.retained.context=0;break;
    case 14: provider.record.boot.owner++;break;
    case 15: model.base.h.usb[16/4]=0;break; /* interconnect not idle */
    case 16: model.base.h.usb[24/4]=1;break; /* interrupt mask */
    case 49:case 50:case 51:case 52:case 53:case 54:case 55:case 56:
        late_cold_fault=fault;break;
    case 57: provider.record.core=2;break;
    case 58: provider.control_expected=1;break;
    case 59: provider.ddr_expected=1;break;
    case 60: provider.valid=0;break;
    case 61: provider.failed=1;break;
    case 62: provider.devices.snapshot=0;break;
    case 63: usb_platform.active=0;break;
    case 64: provider.record.boot.epoch++;break;
    case 65: *bits=0;break;
    case 66: usb_platform.confirmed=0;break;
    default:assert(0);
    }
    return 0;
}
int main(int argc,char **argv)
{
    assert(argc==2);fault=(unsigned)strtoul(argv[1],0,0);assert(fault<=66);
    char *setup_args[]={argv[0],"0"};assert(!shared_boot_fixture_main(2,setup_args));
    uint64_t r[4],generation=usb_platform.generation;
    fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_ACQUIRE,17,generation,0,r);assert(!r[0]);
    host_started(&model);
    fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_CONFIRM,17,generation+1,0,r);assert(!r[0]);
    assert(model.base.bridges.observe_state && usb_platform.active && usb_platform.confirmed);
    producer_hook=producer_sample;
    saved_cold_read=provider.io.read;provider.io.read=injected_cold_read;
    saved_read=usb_board.physical.read;usb_board.physical.read=injected_read;
    unsigned accesses=model.accesses,requests=model.requests;
    unsigned writes=model.base.h.usb_writes,stops=model.base.h.producer_stops;
    if (fault>=32 && fault<=48) {
        if (fault==32) model.base.h.usb[8/4]|=1U<<5;
        if (fault==33) model.base.h.usb[24/4]=1;
        if (fault==34) model.base.h.usb[16/4]=0;
        if (fault==35) model.base.h.per0|=FES_NANO_COLD_USB_RAM_RESET;
        if (fault==36) model.clocks[0]^=1;
        if (fault==37) model.pins[0]^=1;
        if (fault==38) model.base.h.usb[8/4]|=1U;
        if (fault==39) fes_nano_owned_usb.ops.contained=0;
        if (fault==40) fes_nano_owned_usb.ops.contained=fake_containment;
        if (fault==41) usb_platform.io.prepare_master=0;
        if (fault==42) usb_platform.io.prepare_master=fake_prepare;
        if (fault==43) usb_master.producers.snapshot=0;
        if (fault==44) usb_master.producers.snapshot=fake_snapshot;
        if (fault==45) usb_master.producers.context=0;
        if (fault==46) usb_master.producers.owns=0;
        if (fault==47) usb_master.producers.quiesce=0;
        if (fault==48) usb_master.producers.quiescent=0;
        if (fault==37) {
            assert(!fes_nano_usb_service_gate(&fes_nano_owned_usb));
            assert(provider.valid && model.requests==requests);
            fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_QUERY,0,0,0,r);
            assert(r[0]==FES_EMBEDDED_USB_ERROR);
        } else assert(fes_nano_usb_service_gate(&fes_nano_owned_usb)<0);
        assert(fes_nano_owned_usb.failed && model.base.bridges.failed && provider.failed);
        assert(usb_platform.active && usb_platform.confirmed && usb_platform.generation==generation+2);
        if (fault>=39) assert(model.accesses==accesses && !fake_containment_called && !fake_prepare_called && !fake_snapshot_called);
        accesses=model.accesses;
        assert(fes_nano_usb_service_gate(&fes_nano_owned_usb)<0);
        assert(model.accesses==accesses && model.requests==requests);
        assert(model.base.h.usb_writes==writes && model.base.h.producer_stops==stops);
        puts("PASS coherent snapshot shared firmware gate retains containment failure");return 0;
    }
    struct fes_nano_bridge_snapshot state,untouched;
    memset(&state,0xa5,sizeof(state));untouched=state;
    if (fault==0) {
        for (unsigned n=0;n<3;n++) {
            accesses=model.accesses;requests=model.requests;physical_reads=0;
            assert(!fes_nano_bridges_observe(&model.base.bridges,&state));
            assert(state.masters_contained && state.reset==FES_NANO_BRIDGES_ALL);
            /* The failed image needed 47,434 accesses and 496 PHY requests.
             * Bound the repeated physical work, not host execution time. */
            assert(model.accesses-accesses<100 && model.requests==requests);
            printf("PASS coherent snapshot accesses=%u phy_requests=%u physical_reads=%u\n",model.accesses-accesses,model.requests-requests,physical_reads);
        }
        assert(usb_platform.generation==generation+2 && usb_platform.active && usb_platform.confirmed);
    } else {
        if (fault<=16 || fault>=49) inject=1;
        else if (fault==17) model.base.master_boot.retained.snapshot=0;
        else if (fault==18) model.base.master_boot.retained.quiescent=0;
        else if (fault==19) model.base.master_boot.retained.quiesce=0;
        else if (fault==20) model.base.master_boot.retained.observe=replaced_observe;
        else {
            /* Find the actual read span for this compiled provider, then
             * sample early/middle/final reads without stale absolute offsets. */
            physical_reads=0;
            assert(!fes_nano_bridges_observe(&model.base.bridges,&state));
            unsigned count=physical_reads;assert(count>16);
            static const unsigned eighths[]={0,0,0,1,2,3,4,5,6,7,8};
            unsigned index=fault-21;
            fail_read=index<3 ? index+1 : index==10 ? count : count*eighths[index]/8;
            physical_reads=0;state=untouched;
        }
        int status=fes_nano_bridges_observe(&model.base.bridges,&state);
        if (fault==3 || fault==4) {
            /* The master remains contained without a PHY transaction. That
             * grants no USB readiness: an explicit query still checks both
             * PHY samples and retires the complete shared source on drift. */
            assert(!status && state.masters_contained && model.requests==requests);
            assert(!fes_nano_usb_service_gate(&fes_nano_owned_usb));
            assert(model.requests==requests && usb_platform.generation==generation+2);
            unsigned observed=model.accesses;
            fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_QUERY,0,0,0,r);
            assert(r[0]==FES_EMBEDDED_USB_ERROR && model.accesses>observed);
            assert(usb_platform.active && usb_platform.confirmed && provider.failed);
            accesses=model.accesses;requests=model.requests;
            assert(fes_nano_bridges_observe(&model.base.bridges,&state)<0);
            assert(model.accesses==accesses && model.requests==requests);
            puts("PASS coherent snapshot master containment is not PHY readiness");return 0;
        }
        if (!status) fprintf(stderr,"fault=%u physical_reads=%u fail_read=%u board_error=%d cold_failed=%u\n",fault,physical_reads,fail_read,usb_board.first_error,provider.failed);
        assert(status<0);
        if (fault==9) assert(model.base.master_boot.first_error==-71 && usb_master.first_error==-71);
        assert(!memcmp(&state,&untouched,sizeof(state)) && model.base.bridges.failed && model.base.master_boot.failed);
        accesses=model.accesses;requests=model.requests;
        assert(fes_nano_bridges_observe(&model.base.bridges,&state)<0);
        assert(model.accesses==accesses && model.requests==requests);
        printf("PASS coherent snapshot fault=%u retained first failure\n",fault);
    }
    /* Master observations issue no USB/PHY writes or generation publication.
     * Explicit device queries above retain their full independent PHY checks. */
    assert(model.base.h.usb_writes==writes && model.base.h.producer_stops==stops);
    return 0;
}
