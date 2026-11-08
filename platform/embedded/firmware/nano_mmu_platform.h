/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MMU_PLATFORM_H
#define FES_NANO_MMU_PLATFORM_H
int fes_nano_mmu_platform_scope(void);
/* Trusted late startup only; caller parks permanently on failure. */
int fes_nano_mmu_platform_enable(void);
/* Existing cold owner supplies fresh held-master proof, never an NS request. */
int fes_nano_cold_mmu_admit(void);
#endif
