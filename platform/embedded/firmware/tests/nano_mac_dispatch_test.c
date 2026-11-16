/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual dispatcher, shared USB/SD/cold policy and portable MAC engine. */
#include <stdlib.h>
#define FES_NANO_FIRMWARE_MMU 1
static unsigned cpu_valid=1;
int fes_nano_mmu_platform_scope(void) {return cpu_valid;}
#define FES_NANO_SHARED_RPC_ENTRY unused_shared_rpc_main
#include "nano_usb_shared_rpc.c"
#include "nano_mac_fixture.h"

#define SMC_GET_GP(handle,n) (((const uint64_t *)(handle))[n])
#define CTX_GPREG_X5 5
#define CTX_GPREG_X6 6
#define SMC_RET4(handle,a,b,c,d) do { (void)(handle);result_out[0]=(a);result_out[1]=(b);result_out[2]=(c);result_out[3]=(d);return; } while(0)
#define SMC_RET1(handle,a) do { (void)(handle);result_out[0]=(a);return; } while(0)
static void entry(uint32_t smc_fid,uint64_t x1,uint64_t x2,uint64_t x3,uint64_t x4,
                  const uint64_t *handle,uint64_t result_out[4])
{
#include "mac_entry_guard.inc"
    result_out[0]=99;
}
static struct mac_hardware network_model;
static void mac_request(unsigned op,uint64_t r[4])
{
    uint64_t registers[7]={0};
    entry(FES_EMBEDDED_MAC_CALL,op,op ? 17 : 0,FES_EMBEDDED_MAC_COOKIE,
        op ? fes_nano_owned_mac.generation : 0,registers,r);
}
static unsigned peripheral_accesses(void)
{return model.h.reads+model.h.writes+model.h.usb_reads+model.h.usb_writes;}
int main(int argc,char **argv)
{
    assert(argc==2);unsigned mode=(unsigned)strtoul(argv[1],0,0);assert(mode<=9);
    master_prepare(&model,1);
    struct fes_nano_usb_service_ops usb_ops={fes_nano_usb_platform_observe,fes_nano_usb_platform_acquire,
        fes_nano_usb_platform_confirm,fes_nano_usb_platform_stop,fes_nano_usb_platform_idle};
    assert(!fes_nano_usb_service_init_shared(&fes_nano_owned_usb,&model.bridges,&model.usb,usb_ops,
        fes_nano_usb_master_service_ops(&model.master)));
    uint64_t r[4];
    fes_nano_owned_call(FES_EMBEDDED_USB_CALL,1,17,FES_EMBEDDED_USB_COOKIE,0,r);assert(!r[0]);
    native_prepare(&model);
    fes_nano_owned_call(FES_EMBEDDED_USB_CALL,2,17,FES_EMBEDDED_USB_COOKIE,1,r);assert(!r[0]);
    if(mode==7) {
        unsigned reads=peripheral_accesses();mac_request(0,r);mac_empty_reply(r,5);
        assert(peripheral_accesses()==reads);puts("PASS unbound MAC dispatcher");return 0;
    }
    mac_setup(&fes_nano_owned_mac,&network_model);
    if(mode==5) {
        unsigned scopes=network_model.scopes,reads=peripheral_accesses();uint64_t registers[7]={0};
        for(unsigned bad=0;bad<5;bad++) {
            registers[5]=bad==0;registers[6]=bad==1;
            entry(FES_EMBEDDED_MAC_CALL,bad==4 ? 4 : 1,bad==3 ? 18 : 17,
                FES_EMBEDDED_MAC_COOKIE+(bad==2),0,registers,r);mac_empty_reply(r,2);
        }
        assert(network_model.scopes==scopes && !network_model.acquires && peripheral_accesses()==reads);
        puts("PASS MAC malformed entry rejected before platform");return 0;
    }
    mac_request(1,r);assert(!r[0]);
    unsigned reads=peripheral_accesses();
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_SNAPSHOT,0,FES_EMBEDDED_BRIDGE_COOKIE,0,r);
    assert(r[0]==1 && peripheral_accesses()==reads); /* unconfirmed cannot observe */
    mac_request(2,r);assert(!r[0]);
    uint64_t bridge_generation=model.bridges.state.generation,usb_generation=model.usb.generation;
    unsigned scopes=network_model.scopes;
    if(mode==1 || mode==8 || mode==9) {
        network_model.fault=mode==1 ? 32 : mode==8 ? 16 : 8;
        network_model.after=0;mac_request(3,r);mac_empty_reply(r,4);
        assert(fes_nano_owned_mac.reservation && fes_nano_owned_bridges.failed);
    } else if(mode==2) {
        model.bridges.state.owner++;mac_request(0,r);mac_empty_reply(r,4);
        assert(network_model.scopes==scopes && fes_nano_owned_mac.reservation);
    } else if(mode==3) {
        cpu_valid=0;mac_request(0,r);mac_empty_reply(r,5);
        assert(network_model.scopes==scopes && fes_nano_owned_mac.reservation);
    } else if(mode==4) {
        network_model.fault=1;
        fes_nano_owned_call(FES_EMBEDDED_USB_CALL,0,0,FES_EMBEDDED_USB_COOKIE,0,r);
        assert(r[0]==4 && r[1]==(uint64_t)FES_EMBEDDED_USB_TAG<<32 && !r[2] && !r[3]);
        assert(peripheral_accesses()==reads && fes_nano_owned_mac.reservation && model.bridges.failed);
    } else if(mode==6) {
        request_type=RECONFIGURATION;mac_request(3,r);mac_empty_reply(r,1);
        assert(network_model.scopes==scopes && fes_nano_owned_mac.generation==2 && !network_model.stops);
        puts("PASS busy MAC request retains live generation");return 0;
    } else {
        for(unsigned cycle=0;cycle<3;cycle++) {
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_SNAPSHOT,0,FES_EMBEDDED_BRIDGE_COOKIE,0,r);
            assert(!r[0] && r[2]==17 && r[3]==bridge_generation);
            fes_nano_owned_call(FES_EMBEDDED_USB_CALL,0,0,FES_EMBEDDED_USB_COOKIE,0,r);
            assert(!r[0] && r[3]==usb_generation);
            reads=peripheral_accesses();scopes=network_model.scopes;
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,1,17,FES_EMBEDDED_SD_COOKIE,bridge_generation,r);
            assert(r[0]==1 && r[1]==(uint64_t)FES_EMBEDDED_SD_TAG<<32 && !r[2] && !r[3]);
            fes_nano_owned_call(FES_EMBEDDED_USB_CALL,3,17,FES_EMBEDDED_USB_COOKIE,usb_generation,r);assert(r[0]==1);
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,r);assert(r[0]==1 && !configurations);
            assert(peripheral_accesses()==reads && network_model.scopes==scopes);
            mac_request(3,r);assert(!r[0] && r[3]==cycle*3+3);
            assert(model.bridges.state.generation==bridge_generation && model.usb.generation==usb_generation);
            assert(!fes_nano_mac_gate(&fes_nano_owned_mac));
            if(cycle<2){mac_request(1,r);assert(!r[0]);mac_request(2,r);assert(!r[0]);}
        }
        puts("PASS MAC/USB/bridge coexistence and contained mutation gate");return 0;
    }
    scopes=network_model.scopes;reads=peripheral_accesses();cpu_valid=1;
    mac_request(0,r);mac_empty_reply(r,4);
    fes_nano_owned_call(FES_EMBEDDED_USB_CALL,0,0,FES_EMBEDDED_USB_COOKIE,0,r);assert(r[0]);
    assert(network_model.scopes==scopes && peripheral_accesses()==reads && fes_nano_owned_mac.reservation);
    puts("PASS MAC failure retires shared dispatcher without cleanup or callbacks");return 0;
}
