/* SPDX-License-Identifier: BSD-3-Clause */
#include <arch_helpers.h>
#include <common/debug.h>
#include "nano_cold.h"
#include "nano_mmu.h"
#include "nano_mmu_platform.h"
#include "nano_mmu_memory.h"
#include "fes_nano_cold_profile.h"
#if !defined(FES_NANO_FIRMWARE_MMU) || !FES_NANO_FIRMWARE_MMU
#error Explicit_static_EL3_MMU_selection_required
#endif

extern const uint64_t fes_nano_mmu_root[];
static struct fes_nano_mmu mmu;
static const struct fes_nano_mmu_policy policy={(uintptr_t)fes_nano_mmu_root};
volatile uint64_t fes_nano_mmu_witness[8] __attribute__((section(".fes_mmu_witness"),aligned(64)));

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
static int clean_invalidate(void *context)
{
    (void)context;
    dsbsy();dcsw_op_all(DCCISW);dsbsy();isb();
    return 0;
}
static int memory_probe(void *context)
{
    uintptr_t address=(uintptr_t)fes_nano_mmu_witness;
    if(address<0x58000U || address>0x60000U-sizeof(fes_nano_mmu_witness))return -1;
    return fes_nano_mmu_memory_probe(fes_nano_mmu_witness,context,clean_invalidate);
}
static struct fes_nano_mmu_ops operations(void)
{ return (struct fes_nano_mmu_ops){0,observe,admit,write_register,synchronize,memory_probe}; }
int fes_nano_mmu_platform_enable(void)
{ return fes_nano_mmu_enable(&mmu,&policy,operations()); }
int fes_nano_mmu_platform_scope(void)
{ return fes_nano_mmu_scope(&mmu,&policy,operations()); }
static void report(const char *phase)
{
    struct fes_nano_mmu_registers r;observe(0,&r);
    NOTICE("FES_NANO_EL3_MMU phase=%s el=%llx midr=%llx mpidr=%llx mmfr0=%llx daif=%llx scr=%llx sctlr=%llx tcr=%llx root=%llx mair=%llx witness=%llx writes=%u memory=%u\n",
        phase,(unsigned long long)r.el,(unsigned long long)r.midr,(unsigned long long)r.mpidr,
        (unsigned long long)r.mmfr0,(unsigned long long)r.daif,(unsigned long long)r.scr,
        (unsigned long long)r.sctlr,(unsigned long long)r.tcr,(unsigned long long)r.root,
        (unsigned long long)r.mair,(unsigned long long)(uintptr_t)fes_nano_mmu_witness,mmu.writes,mmu.memory_verified);
}
int fes_nano_mmu_platform_boot(void)
{
    report("before");
    int error=fes_nano_mmu_platform_enable();
    if(error || !fes_nano_mmu_platform_scope()) {
        NOTICE("FES_NANO_EL3_MMU FAIL status=%d writes=%u memory=%u\n",mmu.first_error,mmu.writes,mmu.memory_verified);
        return -1;
    }
    report("after");
    return 0;
}
