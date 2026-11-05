/* SPDX-License-Identifier: BSD-3-Clause */
/* Reuse the real selected boot include and physical board register fixture. */
#define main shared_boot_fixture_main
#include "nano_usb_shared_boot_test.c"
#undef main
static unsigned fault,inject;
static unsigned physical_reads,fail_read;
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
    int error=cold_master_quiescent(p,bits);
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
    default:assert(0);
    }
    return 0;
}
int main(int argc,char **argv)
{
    assert(argc==2);fault=(unsigned)strtoul(argv[1],0,0);assert(fault<=31);
    char *setup_args[]={argv[0],"0"};assert(!shared_boot_fixture_main(2,setup_args));
    uint64_t r[4],generation=usb_platform.generation;
    fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_ACQUIRE,17,generation,0,r);assert(!r[0]);
    host_started(&model);
    fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_CONFIRM,17,generation+1,0,r);assert(!r[0]);
    assert(model.base.bridges.observe_state && usb_platform.active && usb_platform.confirmed);
    usb_master.producers.quiescent=producer_sample;
    saved_read=usb_board.physical.read;usb_board.physical.read=injected_read;
    unsigned accesses=model.accesses,requests=model.requests;
    unsigned writes=model.base.h.usb_writes,stops=model.base.h.producer_stops;
    struct fes_nano_bridge_snapshot state,untouched;
    memset(&state,0xa5,sizeof(state));untouched=state;
    if (fault==0) {
        for (unsigned n=0;n<3;n++) {
            accesses=model.accesses;requests=model.requests;physical_reads=0;
            assert(!fes_nano_bridges_observe(&model.base.bridges,&state));
            assert(state.masters_contained && state.reset==FES_NANO_BRIDGES_ALL);
            /* The failed image needed 47,434 accesses and 496 PHY requests.
             * Bound the repeated physical work, not host execution time. */
            assert(model.accesses-accesses<12000 && model.requests-requests<=112);
            printf("PASS coherent snapshot accesses=%u phy_requests=%u physical_reads=%u\n",model.accesses-accesses,model.requests-requests,physical_reads);
        }
        assert(usb_platform.generation==generation+2 && usb_platform.active && usb_platform.confirmed);
    } else {
        if (fault<=16) inject=1;
        else if (fault==17) model.base.master_boot.retained.snapshot=0;
        else if (fault==18) model.base.master_boot.retained.quiescent=0;
        else if (fault==19) model.base.master_boot.retained.quiesce=0;
        else if (fault==20) model.base.master_boot.retained.observe=replaced_observe;
        else {
            static const unsigned offsets[]={1,2,16,300,1530,1531,3060,5000,8000,10000,10599};
            fail_read=offsets[fault-21];
        }
        int status=fes_nano_bridges_observe(&model.base.bridges,&state);
        if (!status) fprintf(stderr,"fault=%u physical_reads=%u fail_read=%u board_error=%d cold_failed=%u\n",fault,physical_reads,fail_read,usb_board.first_error,provider.failed);
        assert(status<0);
        if (fault==9) assert(model.base.master_boot.first_error==-71 && usb_master.first_error==-71);
        assert(!memcmp(&state,&untouched,sizeof(state)) && model.base.bridges.failed && model.base.master_boot.failed);
        accesses=model.accesses;requests=model.requests;
        assert(fes_nano_bridges_observe(&model.base.bridges,&state)<0);
        assert(model.accesses==accesses && model.requests==requests);
        printf("PASS coherent snapshot fault=%u retained first failure\n",fault);
    }
    /* Only PHY-read transactions are permitted by board_write. No device
     * reset, producer stop, generation publication or authority is added. */
    assert(model.base.h.usb_writes==writes && model.base.h.producer_stops==stops);
    return 0;
}
