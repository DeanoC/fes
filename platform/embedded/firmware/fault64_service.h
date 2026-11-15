/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_SDM_FAULT64_SERVICE_H
#define FES_NANO_SDM_FAULT64_SERVICE_H
#if !defined(FES_NANO_SDM_FAULT64) || FES_NANO_SDM_FAULT64 != 1
#error Explicit_unqualified_fault64_diagnostic_only
#endif
#include "fault64.h"
/* Real platform suppliers are deliberately not defined by this prototype.
 * Any missing/failed scope check must park before the vendor mailbox call.
 * AFTER must always park, holding the lock and all payload/table reservations.
 */
void fes_nano_sdm_fault64_arm(uint32_t bypass);
void fes_nano_sdm_fault64_start_result(int status);
void fes_nano_sdm_fault64_submit(unsigned observed);
uint32_t fes_nano_sdm_fault64_before(const struct nano_fault64_vendor *);
__attribute__((noreturn)) void fes_nano_sdm_fault64_after(int mailbox_status);
#endif
