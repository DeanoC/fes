/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MAC_TX_DIAGNOSTIC_H
#define FES_NANO_MAC_TX_DIAGNOSTIC_H
#ifdef __UBOOT__
#include <linux/types.h>
#else
#include <stdint.h>
#endif

/* Exclusive DDR scratch in the standalone after-calibration SPL. No firmware,
 * stack, table or queue address appears in a device-accessible translation.
 * The two nonidentity DMA addresses also have reserved, empty physical backing
 * so a bypass transaction cannot fetch a valid descriptor or nonce. */
#define FES_TX_TABLES 0x94000000U
#define FES_TX_CMDQ   0x94008000U
#define FES_TX_EVTQ   0x94009000U
#define FES_TX_DESC   0x94800000U
#define FES_TX_DATA   0x94801000U
#define FES_TX_IOVA   0x94802000U
#define FES_TX_BUFFER 0x94803000U

struct fes_tx_io {
    void *ctx;
    uint32_t (*read)(void *, uint32_t);
    void (*write)(void *, uint32_t, uint32_t);
    uint64_t (*load)(void *, uint32_t);
    void (*store)(void *, uint32_t, uint64_t);
    void (*sync_memory)(void *);
    uint64_t (*time_us)(void *);
    /* Trusted SPL implementation checks CPU/boot, cached/translated state,
     * completed memory handoff, reset/pin/clock suppliers and actual closed
     * master configuration. Called before any write and during bounded waits. */
    int (*owns)(void *);
};
enum fes_tx_stage {
    FES_TX_INITIAL, FES_TX_TABLE_BUILD, FES_TX_ARM, FES_TX_RELEASE_MAC,
    FES_TX_PHY, FES_TX_DISPATCH, FES_TX_STOP, FES_TX_REVOKE, FES_TX_PARKED
};
struct fes_tx_result {
    enum fes_tx_stage stage;
    uint32_t last_address, last_value, descriptor_status, channel_status;
    unsigned scope_lost, write_attempted, arm_attempted, mapped, mac_release_attempted, dma_possible, dispatched;
    uint32_t terminal_per0, terminal_route, terminal_sid, terminal_cr0, terminal_ack, terminal_ste;
    uint32_t fault_address, fault_value;
    unsigned process_stopped, revoke_started, translation_revoked, mac_in_reset;
    uint64_t started_us, last_us;
};
enum fes_tx_disposition { FES_TX_COLD_RECOVERY, FES_TX_CLOSED, FES_TX_UNCHANGED, FES_TX_REVOKED_UNDISPATCHED };
enum fes_tx_disposition fes_tx_terminal(const struct fes_tx_io *, struct fes_tx_result *, int rc);
int fes_tx_once(const struct fes_tx_io *, const uint8_t frame[60], struct fes_tx_result *);
#endif
