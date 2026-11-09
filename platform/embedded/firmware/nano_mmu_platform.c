/* SPDX-License-Identifier: BSD-3-Clause */
#include <arch_helpers.h>
#include <common/debug.h>
#include "nano_cold.h"
#include "nano_mmu.h"
#include "nano_mmu_platform.h"
#include "nano_mmu_memory.h"
#include "nano_mmu_transition.h"
#include "fes_nano_cold_profile.h"
#if !defined(FES_NANO_FIRMWARE_MMU) || !FES_NANO_FIRMWARE_MMU
#error Explicit_static_EL3_MMU_selection_required
#endif

extern const uint64_t fes_nano_mmu_root[];
extern unsigned char __DATA_START__[],__DATA_END__[];
static struct fes_nano_mmu mmu;
static const struct fes_nano_mmu_policy policy={(uintptr_t)fes_nano_mmu_root};
volatile uint64_t fes_nano_mmu_witness[8] __attribute__((section(".fes_mmu_witness"),aligned(64)))={0};

/* Cold boot only: these operations are never part of the tape/SMC hot path. */
static void step(const char *phase, unsigned operation)
{
    NOTICE("FES_NANO_EL3_MMU_STEP phase=%s operation=%u\n",phase,operation);
}

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
{
    (void)context;
    step("admit-before",0);
    int result=fes_nano_cold_mmu_admit();
    step("admit-after",0);
    return result;
}
static int write_register(void *context, unsigned reg, uint64_t value)
{
    (void)context;
    step("write-before",reg);
    switch(reg) {
    case FES_MMU_MAIR:write_mair_el3(value);break;
    case FES_MMU_TCR:write_tcr_el3(value);break;
    case FES_MMU_ROOT:write_ttbr0_el3(value);break;
    case FES_MMU_SCTLR:
        /* Keeps the stack-free fault vector installed through the first C
         * stack accesses and private memory test. Its leaf write includes ISB. */
        if(fes_nano_mmu_transition_write(value))return -1;
        break;
    default:return -1;
    }
    step("write-after",reg);
    return 0;
}
static int synchronize(void *context, unsigned phase)
{
    (void)context;
    step("barrier-before",phase);
    if(phase==FES_MMU_INVALIDATE) {dsbsy();tlbialle3();dsbsy();isb();}
    else if(phase==FES_MMU_COMMIT) {dsbsy();isb();}
    else return -1;
    step("barrier-after",phase);
    return 0;
}
static int clean_invalidate(void *context)
{
    (void)context;
    step("clean-before",0);
    dsbsy();dcsw_op_all(DCCISW);dsbsy();isb();
    step("clean-after",0);
    return 0;
}
static int memory_probe(void *context)
{
    uintptr_t address=(uintptr_t)fes_nano_mmu_witness;
    uintptr_t first=(uintptr_t)__DATA_START__,limit=(uintptr_t)__DATA_END__;
    if(first!=(uintptr_t)fes_nano_mmu_root+FES_NANO_MMU_TABLE_BYTES ||
       first<0x80000000ULL || limit>0x80200000ULL || limit<=first ||
       limit-first<sizeof(fes_nano_mmu_witness) ||
       address<first || address>limit-sizeof(fes_nano_mmu_witness))return -1;
    step("memory-before",0);
    int result=fes_nano_mmu_memory_probe(fes_nano_mmu_witness,context,clean_invalidate);
    step("memory-after",0);
    return result;
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
    if(fes_nano_mmu_transition_restore()) {
        mmu.first_error=-9;
        mmu.stage=FES_MMU_FAILED;
        NOTICE("FES_NANO_EL3_MMU FAIL status=%d writes=%u memory=%u\n",mmu.first_error,mmu.writes,mmu.memory_verified);
        return -1;
    }
    report("after");
    return 0;
}
