/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual shared dispatcher, including malformed requests and live failures. */
#include <stdlib.h>
#define FES_NANO_SHARED_RPC_ENTRY shared_rpc_fixture_main
#include "nano_usb_shared_rpc.c"
static int forged_observer(void *p,uint64_t owner,uint64_t epoch,uint32_t *reset,unsigned *masters)
{(void)p;(void)owner;(void)epoch;(void)reset;(void)masters;assert(0);return -1;}
int main(int argc,char **argv)
{
    assert(argc==2);unsigned mode=(unsigned)strtoul(argv[1],0,0);assert(mode<=20);
    master_prepare(&model,mode==19 ? 2 : 1);
    struct fes_nano_usb_service_ops ops={fes_nano_usb_platform_observe,fes_nano_usb_platform_acquire,
        fes_nano_usb_platform_confirm,fes_nano_usb_platform_stop,fes_nano_usb_platform_idle};
    int initialized=fes_nano_usb_service_init_shared(&fes_nano_owned_usb,&model.bridges,&model.usb,ops,
        fes_nano_usb_master_service_ops(&model.master));
    if (mode==19) {
        assert(initialized<0 && fes_nano_owned_usb.failed && !model.usb.active);
        puts("PASS shared snapshot dispatcher rejects a different private observer binding");return 0;
    }
    assert(!initialized);
    uint64_t reply[4];
    fes_nano_owned_call(FES_EMBEDDED_USB_CALL,FES_EMBEDDED_USB_ACQUIRE,17,FES_EMBEDDED_USB_COOKIE,0,reply);assert(!reply[0]);
    if (mode!=8) {
        fes_nano_owned_call(FES_EMBEDDED_USB_CALL,FES_EMBEDDED_USB_CONFIRM,17,FES_EMBEDDED_USB_COOKIE,1,reply);assert(!reply[0]);
    }
    unsigned reference_reads=0,reference_usb=0;
    if (mode==0 || mode==17 || mode==19) {
        unsigned reads=model.h.reads,usb=model.h.usb_reads;
        struct fes_nano_bridge_snapshot snapshot;
        assert(!fes_nano_bridges_observe(&model.bridges,&snapshot));
        reference_reads=model.h.reads-reads;reference_usb=model.h.usb_reads-usb;
    }
    if (mode==1)model.h.usb[8/4]|=1U<<5;
    if (mode==2)model.h.usb[8/4]|=1U;
    if (mode==3)model.h.usb[16/4]=0;
    if (mode==4)model.h.usb_fault=3;
    if (mode==5)model.cold.record.boot.epoch++;
    if (mode==6)model.master.producers.snapshot=0;
    if (mode==7)fes_nano_owned_usb.ops.contained=0;
    if (mode==9)assert(!call(&model,FES_EMBEDDED_SD_RESUME));
    if (mode==17)request_type=1;
    if (mode==20)model.bridges.observe_state=forged_observer;
    unsigned reads=model.h.reads,usb=model.h.usb_reads,writes=model.h.usb_writes,bridge_writes=model.h.bridge_writes;
    unsigned ready=model.h.usb_ready;
    uint64_t operation=mode==13 ? 0x7fU : FES_EMBEDDED_BRIDGE_SNAPSHOT;
    uint64_t cookie=FES_EMBEDDED_BRIDGE_COOKIE+(mode==10);
    uint64_t owner=mode==11 ? 17 : 0,generation=mode==12 ? 1 : 0;
    if (mode==14)fes_nano_owned_call(FES_EMBEDDED_USB_CALL,FES_EMBEDDED_USB_QUERY,0,FES_EMBEDDED_USB_COOKIE,0,reply);
    else if (mode==15)fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_ISDONE,0,0,0,0,reply);
    else if (mode==16)fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTEXT_PART0,17,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
    else fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,operation,owner,cookie,generation,reply);
    if (mode==0 || mode==17 || mode==19) {
        assert(!reply[0] && reply[2]==17 && reply[3]==1);
        assert((uint32_t)reply[1]==0xdf && reply[1]>>32==FES_EMBEDDED_BRIDGE_TAG);
        /* One complete actual observation, with no preceding hardware gate. */
        assert(model.h.reads-reads==reference_reads && model.h.usb_reads-usb==reference_usb);
    } else if (mode==8 || mode==9) {
        assert(reply[0]==1 && !reply[2] && !reply[3] && !model.cold.failed);
        assert(model.h.reads==reads && model.h.usb_reads==usb);
    } else if (mode==10 || mode==11 || mode==12 || mode==13 || mode==16) {
        assert(reply[0] && !reply[2] && !reply[3] && !model.cold.failed && model.h.usb_reads>usb);
    } else if (mode==14) {
        assert(!reply[0] && model.h.usb_ready>ready);
    } else if (mode==15) {
        assert(reply[0] && !configurations && model.h.usb_reads>usb);
    } else if (mode==18) {
        struct fes_nano_bridge_snapshot out,untouched;
        memset(&out,0xa5,sizeof(out));untouched=out;model.h.usb[8/4]|=1U<<5;
        assert(fes_nano_usb_service_snapshot(&fes_nano_owned_usb,&out)<0);
        assert(!memcmp(&out,&untouched,sizeof(out)));
    } else {
        assert(reply[0] && !reply[2] && !reply[3] && model.cold.failed && fes_nano_owned_usb.failed);
        reads=model.h.reads;usb=model.h.usb_reads;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_SNAPSHOT,0,FES_EMBEDDED_BRIDGE_COOKIE,0,reply);
        assert(reply[0] && model.h.reads==reads && model.h.usb_reads==usb);
    }
    assert(model.h.usb_writes==writes && model.h.bridge_writes==bridge_writes);
    puts("PASS shared snapshot dispatcher issues one complete observation and preserves failure gates");return 0;
}
