/* SPDX-License-Identifier: BSD-3-Clause */
/* Included after the pinned raw driver in the reset-manager translation unit. */
#include <lib/spinlock.h>
#include <arch_helpers.h>
#include "nano_bridge_platform.h"

struct fes_nano_bridges fes_nano_owned_bridges;
/* No default SD authority. A reviewed secure platform must install its ops
 * under this same lock after accepting the matched boot record. */
struct fes_nano_sd fes_nano_owned_sd;
static struct fes_nano_boot fes_nano_owned_boot;
static spinlock_t fes_nano_owned_lock;
void fes_nano_bridge_platform_lock(void) { spin_lock(&fes_nano_owned_lock); }
void fes_nano_bridge_platform_unlock(void) { spin_unlock(&fes_nano_owned_lock); }
void fes_nano_bridge_platform_invalidate(void)
{
    fes_nano_bridge_platform_lock();
    fes_nano_owned_bridges.failed = 1;
    fes_nano_boot_invalidate(&fes_nano_owned_boot);
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
#ifdef FES_NANO_COLD_SERIAL
    /* Only the serialized, secure read-only probe can reach this path. All
     * non-control bridges and masters retain their cold containment. */
    struct fes_nano_identity_probe *probe=fes_nano_platform_identity_probe();
    if (!probe || !probe->active || mask!=FES_NANO_BRIDGES_CONTROL ||
        fabric!=FES_NANO_FABRIC_ABSENT || fes_nano_platform_control_target(1)) return -1;
    return fes_nano_raw_enable(mask);
#else
    if (fabric != fes_nano_owned_bridges.state.fabric) return -1;
    return fes_nano_raw_enable(mask);
#endif
}
static int fes_nano_owned_disable(void *context, uint32_t mask, unsigned fabric)
{
    (void)context;
    if (fabric != fes_nano_owned_bridges.state.fabric) return -1;
#ifdef FES_NANO_COLD_SERIAL
    struct fes_nano_identity_probe *probe=fes_nano_platform_identity_probe();
    if (!probe || !probe->active || mask!=FES_NANO_BRIDGES_CONTROL ||
        fes_nano_platform_control_target(0)) return -1;
#endif
    fes_nano_bridge_diag_reset();
    return fes_nano_raw_disable(mask);
}
static int fes_nano_owned_per0(void *context, uint32_t *value)
{ (void)context; *value = mmio_read_32(SOCFPGA_RSTMGR(PER0MODRST)); return 0; }
static int fes_nano_owned_assert(void *context, uint32_t mask)
{ (void)context; mmio_setbits_32(SOCFPGA_RSTMGR(PER0MODRST), mask); return 0; }
static void fes_nano_owned_barrier(void *context) { (void)context; dsbsy(); }
static void fes_nano_owned_wait(void *context) { (void)context; udelay(10); }

int fes_nano_bridge_platform_handoff(const struct fes_nano_boot_record *record,
                                     const struct fes_nano_boot_policy *policy, struct fes_nano_master_ops masters)
{
    int result;
    struct fes_nano_bridge_ops raw = {0};
    raw.read_reset = fes_nano_owned_reset;
    raw.enable = fes_nano_owned_enable;
    raw.disable = fes_nano_owned_disable;
    masters.read_per0 = fes_nano_owned_per0;
    masters.assert_per0 = fes_nano_owned_assert;
    masters.barrier = fes_nano_owned_barrier;
    masters.wait = fes_nano_owned_wait;
    fes_nano_bridge_platform_lock();
    result = fes_nano_boot_accept(&fes_nano_owned_boot, &fes_nano_owned_bridges, record, policy, masters, raw);
    fes_nano_bridge_platform_unlock();
    return result;
}
__attribute__((weak)) int fes_nano_platform_boot_record(struct fes_nano_boot_record *record,
                    struct fes_nano_boot_policy *policy, struct fes_nano_master_ops *masters)
{
    /* Replaced only by reviewed secure board code. Reset bits and an NS
     * descriptor cannot fill the SPL/Boot ROM history and master-drain gaps. */
    (void)record; (void)policy; (void)masters;
    return 0;
}
__attribute__((weak)) int fes_nano_platform_sd_setup(void) { return 0; }
__attribute__((weak)) void fes_nano_platform_sd_report(void) {}
void fes_nano_bridge_platform_setup(void)
{
    struct fes_nano_boot_record record = {0};
    struct fes_nano_boot_policy policy = {0};
    struct fes_nano_master_ops masters = {0};
    int available = fes_nano_platform_boot_record(&record, &policy, &masters);
    int result = -1;
    int sd = 0;
    if (available == 1) result = fes_nano_bridge_platform_handoff(&record, &policy, masters);
    else if (available != 0) fes_nano_bridge_platform_invalidate();
    if (available == 1 && result == 0) {
        fes_nano_bridge_platform_lock();
        int identity=fes_nano_platform_identity_setup();
        sd = fes_nano_platform_sd_setup();
        if (sd < 0 || identity < 0) {
            fes_nano_owned_sd.attempted = fes_nano_owned_sd.failed = 1;
            fes_nano_owned_bridges.failed = 1;
            fes_nano_boot_invalidate(&fes_nano_owned_boot);
        }
        fes_nano_bridge_platform_unlock();
    }
    NOTICE("FES_NANO_OWNED_BOOT provider=%d result=%d reset=%08x\n",
           available, result, fes_nano_owned_bridges.state.reset);
    NOTICE("FES_NANO_SD_PLATFORM setup=%d ready=%u\n", sd, fes_nano_owned_sd.initialized);
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
