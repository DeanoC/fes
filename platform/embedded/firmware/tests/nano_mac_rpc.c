/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual owned dispatcher with modeled MAC grant/drain and cold USB/SD. */
#define FES_NANO_SHARED_RPC_ENTRY unused_shared_rpc_main
#include "nano_usb_shared_rpc.c"
#include "nano_mac_fixture.h"
int main(int argc,char **argv)
{
    struct mac_hardware h={0};
    master_prepare(&model,1);
    struct fes_nano_usb_service_ops usb_ops={fes_nano_usb_platform_observe,fes_nano_usb_platform_acquire,
        fes_nano_usb_platform_confirm,fes_nano_usb_platform_stop,fes_nano_usb_platform_idle};
    assert(!fes_nano_usb_service_init_shared(&fes_nano_owned_usb,&model.bridges,&model.usb,usb_ops,
        fes_nano_usb_master_service_ops(&model.master)));
#define s fes_nano_owned_mac
    if(argc!=2 || strcmp(argv[1],"unready"))mac_setup(&s,&h);
    unsigned long long q[7];unsigned fault;
    while(scanf("%llx %llx %llx %llx %llx %llx %llx %x",&q[0],&q[1],&q[2],&q[3],&q[4],&q[5],&q[6],&fault)==8) {
        uint64_t r[4]={FES_EMBEDDED_MAC_REJECTED,(uint64_t)FES_EMBEDDED_MAC_TAG<<32,0,0};
        h.fault=fault;h.after=0;
        if(q[0]==FES_EMBEDDED_MAC_CALL && q[3]==FES_EMBEDDED_MAC_COOKIE && !q[5] && !q[6])
            fes_nano_owned_call((uint32_t)q[0],q[1],q[2],q[3],q[4],r);
        printf("%llx %llx %llx %llx %x %x %x %x %x %x\n",
            (unsigned long long)r[0],(unsigned long long)r[1],(unsigned long long)r[2],(unsigned long long)r[3],
            s.reservation,h.acquires,h.confirms,h.stops,s.failed,h.scopes);
        fflush(stdout);
    }
    return ferror(stdin) ? 1 : 0;
}
