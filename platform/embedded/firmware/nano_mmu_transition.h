/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MMU_TRANSITION_H
#define FES_NANO_MMU_TRANSITION_H
#include <stdint.h>
/* Cold-only leaf routines. A diagnostic exception never returns. */
int fes_nano_mmu_transition_write(uint64_t value);
int fes_nano_mmu_transition_restore(void);
#endif
