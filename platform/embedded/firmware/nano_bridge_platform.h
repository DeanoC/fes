/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_BRIDGE_PLATFORM_H
#define FES_NANO_BRIDGE_PLATFORM_H
#include "nano_bridge_sip.h"
extern struct fes_nano_bridges fes_nano_owned_bridges;
void fes_nano_bridge_platform_setup(void);
void fes_nano_bridge_platform_lock(void);
void fes_nano_bridge_platform_unlock(void);
void fes_nano_bridge_platform_invalidate(void);
/* Secure boot/platform code only. The installed boot observer never calls
 * this without proven history and qualified master/ownership callbacks. */
int fes_nano_bridge_platform_bind(struct fes_nano_bridge_ops, uint64_t owner,
                                 uint64_t epoch, unsigned fabric, unsigned never_enabled);
#endif
