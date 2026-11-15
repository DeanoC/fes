/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MMU_H
#define FES_NANO_MMU_H
#include <stdint.h>

#define FES_NANO_MMU_MAIR 0x4400ffULL
#define FES_NANO_MMU_TCR 0x80803520ULL
#define FES_NANO_MMU_TABLE_BYTES 24576ULL
#define FES_NANO_MMU_SET 0x80005ULL /* WXN, C, M; retain all other SCTLR bits. */
#define FES_NANO_MMU_BOOT_MASK 0x3081005ULL /* EE/E0E, WXN, I, C, M */

enum fes_nano_mmu_stage { FES_MMU_COLD, FES_MMU_ARMING, FES_MMU_ACTIVE, FES_MMU_FAILED };
enum fes_nano_mmu_write { FES_MMU_MAIR, FES_MMU_TCR, FES_MMU_ROOT, FES_MMU_SCTLR };
enum fes_nano_mmu_barrier { FES_MMU_INVALIDATE, FES_MMU_COMMIT };
struct fes_nano_mmu_registers {
    uint64_t el, midr, mpidr, mmfr0, daif, scr, sctlr, mair, tcr, root;
    unsigned sole_cpu;
};
struct fes_nano_mmu_policy { uint64_t root; };
struct fes_nano_mmu_ops {
    void *context;
    int (*observe)(void *, struct fes_nano_mmu_registers *);
    int (*admit)(void *); /* exactly 1: trusted cold owner, all masters held */
    /* SCTLR write must include an ISB before returning or using the stack. */
    int (*write)(void *, unsigned, uint64_t);
    int (*barrier)(void *, unsigned);
    int (*memory)(void *); /* private clean/invalidate/readback, zero on success */
};
struct fes_nano_mmu {
    unsigned stage, attempted, writes, memory_verified;
    int first_error;
    struct fes_nano_mmu_registers before;
    uint64_t root;
};
/* Zero-initialized, secure private object; never reinitialize after failure. */
int fes_nano_mmu_enable(struct fes_nano_mmu *, const struct fes_nano_mmu_policy *,
                        struct fes_nano_mmu_ops);
/* COLD is admitted only before NS execution (SCR.NS=0). ACTIVE checks all
 * selected MMU registers afresh. A failed/partial profile has no off fallback. */
int fes_nano_mmu_scope(struct fes_nano_mmu *, const struct fes_nano_mmu_policy *,
                       void *, int (*observe)(void *, struct fes_nano_mmu_registers *));
#endif
