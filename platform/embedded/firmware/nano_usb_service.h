/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_USB_SERVICE_H
#define FES_NANO_USB_SERVICE_H
#include "nano_usb_platform.h"
#include "nano_bridges.h"
#include "generated/fes_embedded_usb.h"

/* Explicit secure binding; the unbound firmware contains no USB MMIO provider. */
struct fes_nano_usb_service_ops {
    int (*observe)(struct fes_nano_usb_platform *,uint64_t,uint64_t);
    int (*acquire)(struct fes_nano_usb_platform *,uint64_t,uint64_t);
    int (*confirm)(struct fes_nano_usb_platform *,uint64_t,uint64_t);
    int (*stop)(struct fes_nano_usb_platform *,uint64_t,uint64_t);
};
struct fes_nano_usb_service {
    struct fes_nano_usb_platform *platform;
    struct fes_nano_bridges *bridges;
    struct fes_nano_usb_service_ops ops;
    uint64_t owner,epoch;
    unsigned attempted,initialized,failed;
};
/* Secure setup and calls share the bridge/configuration lock. No NS init. */
int fes_nano_usb_service_init(struct fes_nano_usb_service *, struct fes_nano_bridges *,
                             struct fes_nano_usb_platform *, struct fes_nano_usb_service_ops);
void fes_nano_usb_service_sip(struct fes_nano_usb_service *,uint64_t operation,
                              uint64_t owner,uint64_t generation,unsigned configuring,uint64_t result[4]);
int fes_nano_usb_service_active(const struct fes_nano_usb_service *);
#endif
