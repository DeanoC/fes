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
/* Opt-in ROM computer profile only, under the existing platform lock. Called
 * after control-only opening and before its successful reply reaches NS. */
int fes_nano_platform_computer_rom(struct fes_nano_configuration *,uint64_t owner,uint64_t generation);
/* Emit a saved ROM failure once, after containment; no fresh hardware reads. */
void fes_nano_platform_computer_rom_report(void);
/* Explicit initial-tape profile only, under the same secure lock. Returns
 * negative only after a selected uncertain attempt, requiring containment. */
int fes_nano_platform_computer_tape(uint64_t operation,uint64_t owner,uint64_t generation,uint64_t result[4]);
/* Cached first failure only, after containment; no new hardware/timer access. */
void fes_nano_platform_computer_tape_report(unsigned containment_attempted,int containment_status);
/* Mixed-role firmware only. Trusted platform completion callback, under the
 * shared lock, after installing the new stop/ROM inventory and before success.
 * This is never an NS command or authority supplied by descriptor capture. */
struct fes_nano_program;
int fes_nano_program_control_activate(struct fes_nano_configuration *,const struct fes_nano_program *);
/* Report saved first-failure observations only; never sample failed hardware. */
void fes_nano_platform_sd_report(void);
/* Read-only sealed pinmux observation. The weak unbound platform is not ready. */
void fes_nano_platform_hps_io_sip(uint64_t operation, uint64_t owner,
                                 uint64_t generation, unsigned busy, uint64_t result[4]);
int fes_nano_bridge_platform_handoff(const struct fes_nano_boot_record *,
                                     const struct fes_nano_boot_policy *, struct fes_nano_master_ops);
/* Trusted setup only, before releasing NS execution. The initializer must
 * serialize construction of its retained platform objects under this lock.
 * This binding function expects that lock already held, like device setup. */
int fes_nano_bridge_platform_bind_usb_pio(uint64_t owner,uint64_t epoch,
    struct fes_nano_retained_master_ops);
#endif
