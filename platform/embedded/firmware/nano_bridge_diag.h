/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_BRIDGE_DIAG_H
#define FES_NANO_BRIDGE_DIAG_H

/* Opt-in return registers of the local contained Terasic probe only.
 * x3 request cookie; x1 tag:stage, x2 signed-error:ack, x3 request:reset.
 * Each pair is packed high-32:low-32. Stage zero means no failure snapshot. */
#define FES_NANO_BRIDGE_DIAG_REQUEST 0x46455344ULL
#define FES_NANO_BRIDGE_DIAG_TAG 0x46424431U
#define FES_NANO_BRIDGE_STAGE_NONE 0U
#define FES_NANO_BRIDGE_STAGE_S2F_ACK_CLEAR 1U
#define FES_NANO_BRIDGE_STAGE_LW_ACK_CLEAR 2U
#define FES_NANO_BRIDGE_STAGE_F2S_FPGA_ACK_SET 3U
#define FES_NANO_BRIDGE_STAGE_F2S_FLUSH_ACK_SET 4U
#define FES_NANO_BRIDGE_STAGE_SDRAM_FLUSH_ACK_CLEAR 5U
#define FES_NANO_BRIDGE_STAGE_SDRAM_FPGA_ACK_CLEAR 6U
#define FES_NANO_BRIDGE_STAGE_NOC_IDLE_ACK 7U
#define FES_NANO_BRIDGE_STAGE_NOC_IDLE_STATUS 8U
#define FES_NANO_BRIDGE_STAGE_MAX 8U

#ifdef FES_NANO_TFA_DIAG
#include <stdint.h>
void fes_nano_bridge_diag_reset(void);
void fes_nano_bridge_diag_record(uint32_t stage, int error);
void fes_nano_bridge_diag_read(uint64_t result[3]);
#endif
#endif
