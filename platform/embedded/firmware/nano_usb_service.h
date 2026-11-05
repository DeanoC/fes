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
    /* Required only by explicit shared mode. Fresh read-only master
     * containment, separate from observe's full USB/PHY readiness grant. */
    int (*contained)(struct fes_nano_usb_platform *,uint64_t,uint64_t);
};
struct fes_nano_usb_shared_ops {
    void *context;
    /* Trusted cached identity/shape proof for the exact boot/master binding,
     * USB platform and SD lease. Not an NS readiness flag. Live observation is
     * separately required through service_ops.observe/contained. */
    int (*valid)(void *,const struct fes_nano_bridges *,const struct fes_nano_usb_platform *);
    struct fes_nano_usb_service_ops platform;
};
struct fes_nano_usb_service {
    struct fes_nano_usb_platform *platform;
    struct fes_nano_bridges *bridges;
    struct fes_nano_usb_service_ops ops;
    uint64_t owner,epoch;
    unsigned attempted,initialized,failed;
    struct fes_nano_usb_shared_ops shared;
    unsigned reservation;
};
/* Secure setup and calls share the bridge/configuration lock. No NS init. */
int fes_nano_usb_service_init(struct fes_nano_usb_service *, struct fes_nano_bridges *,
                             struct fes_nano_usb_platform *, struct fes_nano_usb_service_ops);
int fes_nano_usb_service_init_shared(struct fes_nano_usb_service *,struct fes_nano_bridges *,
    struct fes_nano_usb_platform *,struct fes_nano_usb_service_ops,struct fes_nano_usb_shared_ops);
void fes_nano_usb_service_sip(struct fes_nano_usb_service *,uint64_t operation,
                              uint64_t owner,uint64_t generation,unsigned configuring,uint64_t result[4]);
int fes_nano_usb_service_active(const struct fes_nano_usb_service *);
/* Shared dispatcher gate: 0 permits other owned calls, 1 means busy, -1 is
 * retired authority. A shared permit includes fresh live master containment, never USB readiness. */
int fes_nano_usb_service_gate(struct fes_nano_usb_service *);
#endif
