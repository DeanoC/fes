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
#if defined(FES_NANO_CONTROLLED_SD) && FES_NANO_CONTROLLED_SD
#include <common/debug.h>
#include "nano_sd_platform.h"
#endif

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

#if defined(FES_NANO_CONTROLLED_SD) && FES_NANO_CONTROLLED_SD
static struct fes_nano_sd_platform sd_platform;
static unsigned sd_reported;
static int sd_read(void *context, uintptr_t address, uint32_t *value)
{ (void)context; dsbsy(); *value=mmio_read_32(address); dsbsy(); return 0; }
static int sd_write(void *context, uintptr_t address, uint32_t value)
{ (void)context; dsbsy(); mmio_write_32(address,value); dsbsy(); return 0; }
static void sd_barrier(void *context) { (void)context; dsbsy(); }
static void sd_wait(void *context) { (void)context; udelay(10); }
void fes_nano_platform_sd_report(void)
{
    if (sd_reported++ || (!sd_platform.failed && !fes_nano_owned_sd.failed)) return;
    NOTICE("FES_NANO_SD_FAILURE stage=%u error=%d address=%lx value=%08x mask=%08x expected=%08x\n",
           sd_platform.stage, sd_platform.first_error, (unsigned long)sd_platform.last_address,
           sd_platform.last_value, sd_platform.last_mask, sd_platform.last_expected);
    for (unsigned i=0; i<FES_NANO_SD_CLOCK_REGS; i++)
        NOTICE("FES_NANO_SD_CLOCK index=%u captured=%08x observed=%08x\n",
               i,sd_platform.clocks[i],sd_platform.observed[i]);
}
int fes_nano_platform_sd_setup(void)
{
    struct fes_nano_sd_io io={0,sd_read,sd_write,sd_barrier,sd_wait};
    int result=fes_nano_sd_platform_init(&sd_platform,&provider,io,FES_NANO_SD_QUALIFIED);
    if (!result) result=fes_nano_sd_init(&fes_nano_owned_sd,&fes_nano_owned_bridges,
                                       fes_nano_sd_platform_ops(&sd_platform),FES_NANO_SD_QUALIFIED);
    if (result) { fes_nano_platform_sd_report(); return -1; }
    return 1;
}
#endif
