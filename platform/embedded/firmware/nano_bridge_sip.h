/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_BRIDGE_SIP_H
#define FES_NANO_BRIDGE_SIP_H
#include "nano_bridges.h"
#include "generated/fes_embedded_bridges.h"
#ifdef __cplusplus
extern "C" {
#endif
void fes_nano_bridge_sip(struct fes_nano_bridges *, uint64_t operation,
                         uint64_t owner, uint64_t generation, unsigned configuring,
                         uint64_t result[4]);
void fes_nano_boot_context_sip(struct fes_nano_bridges *, uint64_t operation,
                              uint64_t owner, uint64_t generation, unsigned configuring,
                              const uint8_t digest[32], uint64_t result[4]);
#ifdef __cplusplus
}
#endif
#endif
