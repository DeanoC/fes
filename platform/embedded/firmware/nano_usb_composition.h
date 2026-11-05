/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_USB_COMPOSITION_H
#define FES_NANO_USB_COMPOSITION_H
#include "nano_usb_platform.h"
#include "nano_sd_platform.h"
#include "nano_usb_service.h"

/* Trusted boot-local composition, not a SiP capability or default permission.
 * Bind only initialized, contained physical platform objects from one boot.
 * They and this object must remain rooted through any failure until reboot.
 * Default USB-only gates remain exclusive. Shared service validation requires
 * this exact composition and its accepted boot/master binding. */
struct fes_nano_usb_composition {
    struct fes_nano_cold *cold;
    struct fes_nano_usb_platform *usb;
    struct fes_nano_sd_platform *sd;
    uint64_t owner,epoch;
    void *usb_context;
    int (*prepare_master)(void *); /* private supplier identity, never cached hardware state */
    int (*contained_scope)(void *,struct fes_nano_cold *,uint64_t,uint64_t);
    unsigned attempted,initialized,failed;
};
int fes_nano_usb_composition_init(struct fes_nano_usb_composition *,
    struct fes_nano_cold *,struct fes_nano_usb_platform *,struct fes_nano_sd_platform *);
/* Optional master provider over the existing independent FPGA producer stop
 * callbacks. Those callbacks must not reset USB or fabricate a fabric receipt.
 * Does not replace the old cold all-reset quiescence API. */
struct fes_nano_usb_producer_ops {
    void *context;
    int (*owns)(void *,uint64_t owner,uint64_t epoch);
    int (*quiesce)(void *,uint32_t producers);
    int (*quiescent)(void *,uint32_t *producers);
    /* Mandatory complete fresh owner/CPU/cold/USB observation enclosing the
     * independent producer sample. No nested owns() call is required by the
     * consumer: this callback supplies those facts itself, not cached bits. */
    int (*snapshot)(void *,uint64_t owner,uint64_t epoch,struct fes_nano_cold_observation *);
};
struct fes_nano_usb_master {
    struct fes_nano_usb_composition *composition;
    struct fes_nano_usb_producer_ops producers,producer_policy;
    uint64_t owner,epoch;
    unsigned attempted,initialized,failed;
    int first_error;
};
int fes_nano_usb_master_init(struct fes_nano_usb_master *,struct fes_nano_usb_composition *,
    struct fes_nano_usb_producer_ops);
struct fes_nano_retained_master_ops fes_nano_usb_master_ops(struct fes_nano_usb_master *);
struct fes_nano_usb_shared_ops fes_nano_usb_master_service_ops(struct fes_nano_usb_master *);
/* Direct private validation paths. They perform the same live private checks
 * as the factories' identities, without constructing temporary callback tables.
 * The factories remain the one-time boot/service binding interfaces. */
int fes_nano_usb_master_binding_matches(struct fes_nano_usb_master *,const struct fes_nano_bridges *);
int fes_nano_usb_master_service_valid(struct fes_nano_usb_master *,const struct fes_nano_bridges *,
    const struct fes_nano_usb_platform *);
#endif
