/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_BRIDGE_PLATFORM_H
#define FES_NANO_BRIDGE_PLATFORM_H
#include "nano_bridge_sip.h"
#include "nano_boot.h"
#include "nano_sd.h"
#include "nano_usb_service.h"
#include "nano_identity.h"
#include "nano_configuration.h"
extern struct fes_nano_bridges fes_nano_owned_bridges;
extern struct fes_nano_sd fes_nano_owned_sd;
extern struct fes_nano_usb_service fes_nano_owned_usb;
void fes_nano_bridge_platform_setup(void);
void fes_nano_bridge_platform_lock(void);
void fes_nano_bridge_platform_unlock(void);
void fes_nano_bridge_platform_invalidate(void);
/* Secure platform integration only. No NS command can supply either policy
 * or callbacks. The default provider has no qualified boot evidence. */
int fes_nano_platform_boot_record(struct fes_nano_boot_record *, struct fes_nano_boot_policy *,
                                  struct fes_nano_master_ops *);
/* Called only by secure setup under the shared lock after boot acceptance.
 * Zero leaves SD unavailable; one installs the matched platform callbacks. */
int fes_nano_platform_sd_setup(void);
/* Separate secure binding. Zero leaves USB unavailable. */
int fes_nano_platform_usb_setup(void);
/* Report saved first-failure observations only; never sample failed hardware. */
void fes_nano_platform_sd_report(void);
/* Read-only sealed pinmux observation. The weak unbound platform is not ready. */
void fes_nano_platform_hps_io_sip(uint64_t operation, uint64_t owner,
                                 uint64_t generation, unsigned busy, uint64_t result[4]);
int fes_nano_bridge_platform_handoff(const struct fes_nano_boot_record *,
                                     const struct fes_nano_boot_policy *, struct fes_nano_master_ops);
#endif
