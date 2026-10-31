/* SPDX-License-Identifier: BSD-3-Clause */
/* Included after the pinned raw driver in the reset-manager translation unit. */
#include <lib/spinlock.h>
#include "nano_bridge_platform.h"

struct fes_nano_bridges fes_nano_owned_bridges;
static spinlock_t fes_nano_owned_lock;
void fes_nano_bridge_platform_lock(void) { spin_lock(&fes_nano_owned_lock); }
void fes_nano_bridge_platform_unlock(void) { spin_unlock(&fes_nano_owned_lock); }
void fes_nano_bridge_platform_invalidate(void)
{
    fes_nano_bridge_platform_lock();
    fes_nano_owned_bridges.failed = 1;
    fes_nano_bridge_platform_unlock();
}

int fes_nano_bridge_fabric_handshake(void)
{
    return fes_nano_owned_bridges.state.fabric == FES_NANO_FABRIC_PRESENT;
}
static int fes_nano_owned_reset(void *context, uint32_t *reset)
{
    (void)context;
    *reset = mmio_read_32(SOCFPGA_RSTMGR(BRGMODRST));
    return 0;
}
static int fes_nano_owned_enable(void *context, uint32_t mask, unsigned fabric)
{
    (void)context;
    if (fabric != fes_nano_owned_bridges.state.fabric) return -1;
    return fes_nano_raw_enable(mask);
}
static int fes_nano_owned_disable(void *context, uint32_t mask, unsigned fabric)
{
    (void)context;
    if (fabric != fes_nano_owned_bridges.state.fabric) return -1;
    fes_nano_bridge_diag_reset();
    return fes_nano_raw_disable(mask);
}
int fes_nano_bridge_platform_bind(struct fes_nano_bridge_ops ops, uint64_t owner,
                                 uint64_t epoch, unsigned fabric, unsigned never_enabled)
{
    int result;
    ops.read_reset = fes_nano_owned_reset;
    ops.enable = fes_nano_owned_enable;
    ops.disable = fes_nano_owned_disable;
    fes_nano_bridge_platform_lock();
    result = fes_nano_bridges_init(&fes_nano_owned_bridges, ops, owner, epoch, fabric, never_enabled);
    fes_nano_bridge_platform_unlock();
    return result;
}
void fes_nano_bridge_platform_setup(void)
{
    /* A reset snapshot is not a trusted never-enabled history. No callback
     * set is qualified for the pinned SPL/Boot ROM boundary yet. Keep the
     * service uninitialized and expose the explicit NOT_READY reply. */
    NOTICE("FES_NANO_OWNED_BOOT proof=unavailable reset=%08x\n",
           mmio_read_32(SOCFPGA_RSTMGR(BRGMODRST)));
}

/* Legacy and alternate SiP callers cannot bypass the owned channel. The
 * contained configuration engine uses disable only as a closed-state guard. */
int socfpga_bridges_enable(uint32_t mask) { (void)mask; return -1; }
int socfpga_bridge_nongraceful_disable(uint32_t mask) { (void)mask; return -1; }
int socfpga_bridges_disable(uint32_t mask)
{
    /* Called under the SiP channel lock by the configuration engine. */
    return mask == ~0U || mask == FES_NANO_BRIDGES_ALL ?
        fes_nano_bridges_configuration_guard(&fes_nano_owned_bridges) : -1;
}
