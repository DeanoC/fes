/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_SDM_ARM_PROBE_H
#define FES_NANO_SDM_ARM_PROBE_H
#include <stdint.h>
/* Called only after the existing owned START validation, under its lock and
 * before the original vendor START. This first mode never submits SDM data. */
__attribute__((noreturn)) void fes_nano_sdm_probe_arm_park(uint32_t remapper_bypass);
#endif
