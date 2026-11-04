/* SPDX-License-Identifier: BSD-3-Clause */
/* Included only by the selected cold USB producer. All objects remain rooted
 * through failure; no non-secure initialization or register addresses enter. */
#include "nano_usb_board.h"
static struct fes_nano_usb_board usb_board;
static struct fes_nano_usb_platform usb_platform;
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
    if (!error) error=fes_nano_usb_service_init(&fes_nano_owned_usb,&fes_nano_owned_bridges,&usb_platform,ops);
    if (error) {
        fes_nano_owned_usb.attempted=fes_nano_owned_usb.failed=1;
        provider.failed=1;provider.valid=0;
    }
    NOTICE("FES_NANO_USB_SETUP status=%d board_error=%d platform_error=%d data=held\n",
           error,usb_board.first_error,usb_platform.first_error);
    return error ? -1 : 1;
}
