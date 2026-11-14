/* SPDX-License-Identifier: BSD-3-Clause */
#define FES_NANO_USB_BOARD_ENTRY boot_board_fixture_cases
#include "nano_usb_board_test.c"
#include "nano_usb_service.h"
#include <stdlib.h>
static struct board_test model;
struct fes_nano_usb_service fes_nano_owned_usb;
struct fes_nano_bridges fes_nano_owned_bridges;
static uint32_t boot_read(uintptr_t a) {uint32_t v;assert(!board_read(&model,a,&v));return v;}
static void boot_write(uintptr_t a,uint32_t v) {assert(!board_write(&model,a,v));}
#define provider (model.base.cold)
#define hps_io_registers compiled_pins
#define hps_io_digest compiled_digest
#define FES_COLD_READ32(a) boot_read(a)
#define FES_COLD_WRITE32(a,v) boot_write(a,v)
#define FES_COLD_BARRIER() barrier(&model)
#define FES_COLD_WAIT() board_wait(&model)
#define NOTICE(...) printf(__VA_ARGS__)
#include "nano_usb_boot.inc"
static int boot_owns(void *p,uint64_t owner,uint64_t epoch)
{return fes_nano_cold_owns(p,owner,epoch);}
static int boot_reset(void *p,uint32_t *v) {(void)p;*v=model.base.h.bridge;return 0;}
static int boot_masters(void *p) {(void)p;return (model.base.h.per0&FES_NANO_MASTER_RESET)==FES_NANO_MASTER_RESET;}
static int boot_stop(void *p) {(void)p;assert(0);return -1;}
static int boot_change(void *p,uint32_t m,unsigned f) {(void)p;(void)m;(void)f;assert(0);return -1;}
static void call(uint64_t op,uint64_t generation,uint64_t *r)
{fes_nano_usb_service_sip(&fes_nano_owned_usb,op,op?17:0,generation,0,r);}
int main(int argc,char **argv)
{
    assert(argc==2);unsigned mode=(unsigned)strtoul(argv[1],0,0);assert(mode<=5);
    setup(&model);
    struct fes_nano_bridge_ops ops={&provider,boot_owns,boot_reset,boot_masters,boot_stop,boot_change,boot_change};
    assert(!fes_nano_bridges_init(&fes_nano_owned_bridges,ops,17,7,1,1));
    if (mode==1) model.clocks[0]=1;
    int result=fes_nano_platform_usb_setup();uint64_t r[4];
    if (mode==1) {
        assert(result<0 && provider.failed && fes_nano_owned_usb.failed);
        call(0,0,r);assert(r[0]==FES_EMBEDDED_USB_ERROR);assert(!model.base.h.usb_accesses);return 0;
    }
    assert(result==1 && usb_platform.initialized && !usb_platform.active && !model.base.h.usb_accesses);
    unsigned accesses=model.accesses;uint32_t ignored;
    assert(usb_read(NULL,FES_NANO_USB_BASE,&ignored) && usb_write(NULL,FES_NANO_USB_BASE,0));
    assert(model.accesses==accesses);
    call(0,0,r);assert(!r[0] && r[2]==17 && !r[3]);
    if (mode==5) {
        assert(fes_nano_platform_usb_setup()<0 && provider.failed);return 0;
    }
    for (unsigned cycle=0;cycle<(mode==0?3U:1U);cycle++) {
        uint64_t generation=usb_platform.generation;
        call(FES_EMBEDDED_USB_ACQUIRE,generation,r);assert(!r[0] && usb_platform.active && !usb_platform.confirmed);
        accesses=model.accesses;
        fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_CONFIRM,18,generation+1,0,r);
        assert(r[0]==FES_EMBEDDED_USB_REJECTED && accesses==model.accesses);
        if (mode!=3) {
            host_started(&model);
            if (mode==2) model.phy[0x13]=0;
            call(FES_EMBEDDED_USB_CONFIRM,generation+1,r);
            if (mode==2) {assert(r[0]==FES_EMBEDDED_USB_ERROR && fes_nano_usb_service_active(&fes_nano_owned_usb));return 0;}
            assert(!r[0] && usb_platform.confirmed);
        }
        if (mode==4) model.base.h.fault=4;
        call(FES_EMBEDDED_USB_STOP,usb_platform.generation,r);
        if (mode==4) {assert(r[0]==FES_EMBEDDED_USB_ERROR && fes_nano_usb_service_active(&fes_nano_owned_usb));return 0;}
        assert(!r[0] && !usb_platform.active && !usb_platform.confirmed && !fes_nano_owned_bridges.device_lease);
        assert(model.base.h.per0&FES_NANO_USB_RESET);
    }
    puts("PASS trusted USB cold binding success/prepared-abort and containment");return 0;
}
