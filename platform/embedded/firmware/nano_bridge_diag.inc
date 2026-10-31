/* SPDX-License-Identifier: BSD-3-Clause */
/* Inserted into the pinned reset-manager translation unit, not built alone. */
#define FES_NANO_TFA_DIAG
#include "nano_bridge_diag.h"

static uint64_t fes_nano_bridge_diag_words[3];

void fes_nano_bridge_diag_reset(void)
{
	fes_nano_bridge_diag_words[0] = (uint64_t)FES_NANO_BRIDGE_DIAG_TAG << 32;
	fes_nano_bridge_diag_words[1] = 0;
	fes_nano_bridge_diag_words[2] = 0;
}

void fes_nano_bridge_diag_record(uint32_t stage, int error)
{
	if ((uint32_t)fes_nano_bridge_diag_words[0] != 0U) {
		return;
	}
	/* Snapshot immediately after the first failed poll, before later writes.
	 * These reads do not prove drain or replace the poll's returned error. */
	fes_nano_bridge_diag_words[0] = ((uint64_t)FES_NANO_BRIDGE_DIAG_TAG << 32) | stage;
	fes_nano_bridge_diag_words[1] = ((uint64_t)(uint32_t)error << 32) |
		mmio_read_32(SOCFPGA_RSTMGR(HDSKACK));
	fes_nano_bridge_diag_words[2] = ((uint64_t)mmio_read_32(SOCFPGA_RSTMGR(HDSKREQ)) << 32) |
		mmio_read_32(SOCFPGA_RSTMGR(BRGMODRST));
}

void fes_nano_bridge_diag_read(uint64_t result[3])
{
	for (unsigned int i = 0; i < 3U; i++) {
		result[i] = fes_nano_bridge_diag_words[i];
	}
}

