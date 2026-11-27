/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MAC_JOINED_FIXTURE_H
#define FES_NANO_MAC_JOINED_FIXTURE_H
#include <stdint.h>

/* Only host register models. The secure supplier, channel/PHY observations
 * and generation/domain checks remain the actual C implementations. */
void fes_mac_fixture_boot(void);
void fes_mac_fixture_driver_start(void);
void fes_mac_fixture_driver_stop(void);
unsigned fes_mac_fixture_restored(void);
enum fes_mac_fixture_fault { FES_FIXTURE_RX_PENDING, FES_FIXTURE_DMA_FAULT, FES_FIXTURE_CHANGE_RX_DATA };
void fes_mac_fixture_fault(enum fes_mac_fixture_fault);
/* Internal shared model wires. */
void fes_mac_fixture_channel_boot(void);
void fes_mac_fixture_set_reset(uint32_t);
void fes_mac_fixture_core_reset(void);
unsigned fes_mac_fixture_engines_live(void);
uint32_t channel_mmio(uint32_t);
uint64_t channel_time(void);
int channel_frequency(void);
#endif
