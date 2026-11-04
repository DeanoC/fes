/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_USB_COMPOSITION_H
#define FES_NANO_USB_COMPOSITION_H
#include "nano_usb_platform.h"
#include "nano_sd_platform.h"

/* Trusted boot-local composition, not a SiP capability or default permission.
 * Bind only initialized, contained physical platform objects from one boot.
 * They and this object must remain rooted through any failure until reboot.
 * Existing USB-only service/dispatcher gates are deliberately unchanged. */
struct fes_nano_usb_composition {
    struct fes_nano_cold *cold;
    struct fes_nano_usb_platform *usb;
    struct fes_nano_sd_platform *sd;
    uint64_t owner,epoch;
    unsigned attempted,initialized,failed;
};
int fes_nano_usb_composition_init(struct fes_nano_usb_composition *,
    struct fes_nano_cold *,struct fes_nano_usb_platform *,struct fes_nano_sd_platform *);
#endif
