/* SPDX-License-Identifier: BSD-3-Clause */
/* Substitute registers only; actual platform adapter and cold admission run. */
#include "nano_mmu.h"
#include "nano_mmu_platform.h"
static uint64_t mmu_scr=0x530,mmu_mair,mmu_tcr,mmu_root;
static unsigned mmu_writes,mmu_invalidations,mmu_isbs;
static unsigned mmu_device_reads,mmu_device_writes;
static unsigned mmu_drop_mair;
static unsigned mmu_clean;
static unsigned mmu_vector_installed,mmu_vector_restored,mmu_restore_failure,mmu_install_failure;
extern volatile uint64_t fes_nano_mmu_witness[8];
/* Keep the Linux test heap above the fixed32-bit DMA models. The unused
 * section is linked above4GiB; it is never present in Nano firmware. */
static unsigned char mmu_test_heap_anchor[1]
    __attribute__((section(".fixture_heap_anchor"),used))={0};
uint64_t read_midr(void) {return 0x412fd050;}
uint64_t read_id_aa64mmfr0_el1(void) {return 2;}
uint64_t read_daif(void) {return 0x3c0;}
uint64_t read_scr_el3(void) {return mmu_scr;}
uint64_t read_mair_el3(void) {return mmu_mair;}
uint64_t read_tcr_el3(void) {return mmu_tcr;}
uint64_t read_ttbr0_el3(void) {return mmu_root;}
void write_mair_el3(uint64_t value) {assert(mmu_writes++==0);if(!mmu_drop_mair)mmu_mair=value;}
void write_tcr_el3(uint64_t value) {assert(mmu_writes++==1);mmu_tcr=value;}
void write_ttbr0_el3(uint64_t value) {assert(mmu_writes++==2);mmu_root=value;}
void write_sctlr_el3(uint64_t value) {assert(mmu_writes++==3);sctlr=value;}
void tlbialle3(void) {assert(!mmu_writes && !mmu_invalidations++ && !(sctlr&5U));}
void isb(void) {mmu_isbs++;}
void dcsw_op_all(uint64_t op) {assert(op==1 && (sctlr&5U)==5U);mmu_clean++;}
int fes_nano_mmu_transition_write(uint64_t value)
{
    if(mmu_install_failure)return -1;
    assert(!mmu_vector_installed++ && !mmu_vector_restored);
    for(unsigned i=0;i<17;i++)printf("FES_NANO_EL3_MMU_STATUS phase=before index=%02u value=0000000000000000\n",i);
    puts("FES_NANO_EL3_MMU_STATUS phase=before END ras=skip reason=feature");
    puts("FES_NANO_EL3_MMU_LOG phase=arm-before\nFES_NANO_EL3_MMU_LOG phase=armed");
    write_sctlr_el3(value);isb();
    printf("FES_NANO_EL3_MMU_TRANSITION phase=enabled sctlr=%016llx vbar=0000000080032000\n",(unsigned long long)sctlr);
    return 0;
}
int fes_nano_mmu_transition_restore(void)
{
    assert(mmu_vector_installed==1 && !mmu_vector_restored++);
    if(mmu_restore_failure)return -1;
    puts("FES_NANO_EL3_MMU_LOG phase=restore-before\nFES_NANO_EL3_MMU_LOG phase=restored");
    isb();
    printf("FES_NANO_EL3_MMU_TRANSITION phase=restored sctlr=%016llx vbar=0000000080030000\n",(unsigned long long)sctlr);
    return 0;
}
static void fes_test_mmu_start(void)
{
    assert(!fes_nano_mmu_platform_boot());
    assert(mmu_writes==4 && mmu_invalidations==1 && mmu_isbs==6 && mmu_clean==2);
    assert(mmu_vector_installed==1 && mmu_vector_restored==1);
    for(unsigned i=0;i<8;i++)assert(!fes_nano_mmu_witness[i]);
    assert(sctlr==(0x30c5183aULL|FES_NANO_MMU_SET));
    assert(mmu_mair==FES_NANO_MMU_MAIR && mmu_tcr==FES_NANO_MMU_TCR && mmu_root==0x8003c000);
    assert(fes_nano_mmu_platform_scope()==1);
    mmu_scr|=1; /* EL3 returns to NS; subsequent modeled SMCs retain SCR.NS. */
    assert(fes_nano_mmu_platform_scope()==1);
}
static int fes_test_mmu_failure(unsigned mode)
{
    assert(mode>=60 && mode<=67);
    assert(fes_nano_cold_mmu_admit()==1);
    switch(mode) {
    case 60:fes_nano_owned_bridges.state.owner^=1;break;
    case 61:fes_nano_owned_bridges.epoch^=1;break;
    case 62:fes_nano_owned_bridges.device_lease=1;break;
    case 63:fes_nano_owned_bridges.state.reset=13;break;
    case 64:per0&=~0x80U;break;
    case 65:release_locked=0;break;
    case 66:bridge=13;break;
    case 67:fes_nano_owned_usb.platform->active=1;break;
    }
    assert(fes_nano_mmu_platform_enable()<0 && !mmu_writes);
    assert(!fes_nano_mmu_platform_scope());
    assert(fes_nano_mmu_platform_enable()<0 && !mmu_writes);
    puts("PASS actual MMU cold admission failure: no register writes, no replay");
    return 0;
}
