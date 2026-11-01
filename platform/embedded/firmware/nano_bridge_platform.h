/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_BRIDGE_PLATFORM_H
#define FES_NANO_BRIDGE_PLATFORM_H
#include "nano_bridge_sip.h"
#include "nano_boot.h"
#include "nano_sd.h"
extern struct fes_nano_bridges fes_nano_owned_bridges;
extern struct fes_nano_sd fes_nano_owned_sd;
void fes_nano_bridge_platform_setup(void);
void fes_nano_bridge_platform_lock(void);
void fes_nano_bridge_platform_unlock(void);
void fes_nano_bridge_platform_invalidate(void);
/* Secure platform integration only. No NS command can supply either policy
 * or callbacks. The default provider has no qualified boot evidence. */
int fes_nano_platform_boot_record(struct fes_nano_boot_record *, struct fes_nano_boot_policy *,
                                  struct fes_nano_master_ops *);
int fes_nano_bridge_platform_handoff(const struct fes_nano_boot_record *,
                                     const struct fes_nano_boot_policy *, struct fes_nano_master_ops);
#endif
