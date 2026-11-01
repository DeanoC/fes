/* SPDX-License-Identifier: BSD-3-Clause */
#include <arch_helpers.h>
#include <drivers/delay_timer.h>
#include <lib/mmio.h>
#include <string.h>
#include "nano_bridge_platform.h"
#include "nano_cold.h"
#include "fes_nano_cold_profile.h"
#define FES_COLD_READ32(a) mmio_read_32(a)
#define FES_COLD_READ16(a) mmio_read_16(a)
#define FES_COLD_WRITE32(a,v) mmio_write_32(a,v)
#define FES_COLD_WRITE16(a,v) mmio_write_16(a,v)
#define FES_COLD_BARRIER() dsbsy()
#define FES_COLD_WAIT() udelay(10)
#include "nano_cold_mmio.inc"

static struct fes_nano_cold_record captured;
static struct fes_nano_cold provider;
static unsigned capture_attempted, capture_valid;

/* Runs in early EL3 setup, before BL33. No pointer from a later SMC is used. */
void fes_nano_cold_capture(uintptr_t pointer, uint64_t cookie)
{
    if (capture_attempted++) return;
    if (cookie!=FES_NANO_COLD_COOKIE || pointer&7U || pointer<0x1000 ||
        pointer>0x40000-sizeof(captured) || GET_EL(read_current_el())!=MODE_EL3) return;
    memcpy(&captured,(const void *)pointer,sizeof(captured));
    capture_valid=captured.checksum==fes_nano_cold_checksum(&captured);
}
int fes_nano_platform_boot_record(struct fes_nano_boot_record *record,
                    struct fes_nano_boot_policy *policy, struct fes_nano_master_ops *masters)
{
    static const uint8_t chain[32]=FES_NANO_COLD_CHAIN;
    uint64_t owner=(FES_NANO_COLD_OWNER_SEED ^ captured.boot.epoch)|1U;
    if (!capture_valid || fes_nano_cold_cpu_release_locked()!=1 ||
        captured.core!=MPIDR_AFFLVL1_VAL(read_mpidr()) ||
        fes_nano_cold_consume(&provider,&captured,fes_cold_hw_ops(),chain,owner)) return -1;
    *record=provider.record.boot;
    *policy=(struct fes_nano_boot_policy){.qualifications=FES_NANO_BOOT_QUALIFIED,
        .history=FES_NANO_BOOT_NEVER_ENABLED,.fabric=FES_NANO_FABRIC_ABSENT,.producers=FES_NANO_PRODUCERS_ALL};
    memcpy(policy->chain,chain,32);
    *masters=(struct fes_nano_master_ops){.context=&provider,.owns=fes_nano_cold_owns,
        .quiesce=fes_nano_cold_quiesce,.quiescent=fes_nano_cold_quiescent};
    return 1;
}
