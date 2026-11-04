/* SPDX-License-Identifier: BSD-3-Clause */
/* Included only by the selected cold USB producer. All objects remain rooted
 * through failure; no non-secure initialization or register addresses enter. */
#include "nano_usb_board.h"
static struct fes_nano_usb_board usb_board;
static struct fes_nano_usb_platform usb_platform;
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
#if !defined(FES_NANO_READER_CPU_BASE) || !defined(FES_NANO_CONTROLLED_SD) || !FES_NANO_CONTROLLED_SD
#error Shared_USB_requires_owned_SD_and_independent_FPGA_master_stop
#endif
#include "nano_usb_composition.h"
static struct fes_nano_usb_composition usb_composition;
static struct fes_nano_usb_master usb_master;
#endif
static int usb_read(void *context,uintptr_t address,uint32_t *value)
{
    if (context!=&provider || !value) return -1;
    *value=FES_COLD_READ32(address);return 0;
}
static int usb_write(void *context,uintptr_t address,uint32_t value)
{
    if (context!=&provider) return -1;
    FES_COLD_WRITE32(address,value);return 0;
}
static void usb_barrier(void *context) { (void)context;FES_COLD_BARRIER(); }
static void usb_wait(void *context) { (void)context;FES_COLD_WAIT(); }
int fes_nano_platform_usb_setup(void)
{
    struct fes_nano_usb_io physical={.context=&provider,.read=usb_read,.write=usb_write,
        .barrier=usb_barrier,.wait=usb_wait};
    int error=fes_nano_usb_board_init(&usb_board,&provider,physical,hps_io_registers,
                                     FES_NANO_HPS_IO_COUNT,hps_io_digest);
    if (!error) error=fes_nano_usb_platform_init(&usb_platform,&provider,
                                  fes_nano_usb_board_ops(&usb_board),FES_NANO_USB_QUALIFIED);
    struct fes_nano_usb_service_ops ops={fes_nano_usb_platform_observe,
        fes_nano_usb_platform_acquire,fes_nano_usb_platform_confirm,fes_nano_usb_platform_stop};
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
    if (!error) error=fes_nano_usb_composition_init(&usb_composition,&provider,&usb_platform,&sd_platform);
    struct fes_nano_master_ops producers={.context=&provider,.owns=cold_master_owns,
        .quiesce=cold_master_quiesce,.quiescent=cold_master_quiescent};
    if (!error) error=fes_nano_usb_master_init(&usb_master,&usb_composition,producers);
    if (!error) error=fes_nano_bridge_platform_bind_usb_pio(provider.record.boot.owner,
        provider.record.boot.epoch,fes_nano_usb_master_ops(&usb_master));
    if (!error) error=fes_nano_usb_service_init_shared(&fes_nano_owned_usb,&fes_nano_owned_bridges,
        &usb_platform,ops,fes_nano_usb_master_service_ops(&usb_master));
#else
    if (!error) error=fes_nano_usb_service_init(&fes_nano_owned_usb,&fes_nano_owned_bridges,&usb_platform,ops);
#endif
    if (error) {
        fes_nano_owned_usb.attempted=fes_nano_owned_usb.failed=1;
        provider.failed=1;provider.valid=0;
    }
    NOTICE("FES_NANO_USB_SETUP status=%d board_error=%d platform_error=%d data=held\n",
           error,usb_board.first_error,usb_platform.first_error);
    return error ? -1 : 1;
}
