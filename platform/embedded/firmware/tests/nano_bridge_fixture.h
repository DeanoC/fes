/* SPDX-License-Identifier: BSD-3-Clause */
/* Host-only register/poll injection; no address is physical. */
#include "nano_bridges.h"
#ifdef __cplusplus
extern "C" {
#endif
extern struct fes_nano_bridges fes_nano_fixture_service;
int fes_nano_fixture_boot(unsigned reset, unsigned cold_proof, unsigned fabric);
int fes_nano_fixture_boot_handoff(unsigned reset, unsigned cold_proof, unsigned fabric);
void fes_nano_fixture_fault(unsigned fault);
void fes_nano_fixture_stats(uint64_t result[6]);
#ifdef __cplusplus
}
#endif
