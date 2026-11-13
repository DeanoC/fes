/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SDM_READER_ROUTE_H
#define NANO_SDM_READER_ROUTE_H
#include "nano_smmu.h"
/* Shape-only architectural predicate. The strong platform must separately
 * establish sealed secure-SID1 wiring, live ownership, retained source and
 * contained masters. This function never supplies an access/drain/reuse grant. */
int nano_sdm_reader_observed(const struct fes_nano_smmu_observation *);
int fes_nano_sdm_source_reader_ready(void);
#endif
