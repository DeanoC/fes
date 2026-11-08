/* SPDX-License-Identifier: BSD-3-Clause */
#include <arch_helpers.h>
#include "nano_cold.h"
#include "nano_mmu.h"
#include "nano_mmu_platform.h"
#include "fes_nano_cold_profile.h"
#if !defined(FES_NANO_FIRMWARE_MMU) || !FES_NANO_FIRMWARE_MMU
#error Explicit_static_EL3_MMU_selection_required
#endif

extern const uint64_t fes_nano_mmu_root[];
static struct fes_nano_mmu mmu;
static const struct fes_nano_mmu_policy policy={(uintptr_t)fes_nano_mmu_root};

static int observe(void *context, struct fes_nano_mmu_registers *r)
{
    (void)context;
    *r=(struct fes_nano_mmu_registers){
        .el=read_current_el(),.midr=read_midr(),.mpidr=read_mpidr(),
        .mmfr0=read_id_aa64mmfr0_el1(),.daif=read_daif(),.scr=read_scr_el3(),
        .sctlr=read_sctlr_el3(),.mair=read_mair_el3(),.tcr=read_tcr_el3(),
        .root=read_ttbr0_el3(),.sole_cpu=(unsigned)fes_nano_cold_cpu_release_locked()};
    return 0;
}
static int admit(void *context)
{ (void)context;return fes_nano_cold_mmu_admit(); }
static int write_register(void *context, unsigned reg, uint64_t value)
{
    (void)context;
    switch(reg) {
    case FES_MMU_MAIR:write_mair_el3(value);break;
    case FES_MMU_TCR:write_tcr_el3(value);break;
    case FES_MMU_ROOT:write_ttbr0_el3(value);break;
    case FES_MMU_SCTLR:
        write_sctlr_el3(value);
        isb(); /* Before returning to C or touching the stack in the new regime. */
        break;
    default:return -1;
    }
    return 0;
}
static int synchronize(void *context, unsigned phase)
{
    (void)context;
    if(phase==FES_MMU_INVALIDATE) {dsbsy();tlbialle3();dsbsy();isb();return 0;}
    if(phase==FES_MMU_COMMIT) {dsbsy();isb();return 0;}
    return -1;
}
static struct fes_nano_mmu_ops operations(void)
{ return (struct fes_nano_mmu_ops){0,observe,admit,write_register,synchronize}; }
int fes_nano_mmu_platform_enable(void)
{ return fes_nano_mmu_enable(&mmu,&policy,operations()); }
int fes_nano_mmu_platform_scope(void)
{ return fes_nano_mmu_scope(&mmu,&policy,operations()); }
