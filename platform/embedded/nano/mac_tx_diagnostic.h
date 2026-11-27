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
/* Agilex 5 HPS TRM 7.6.5.2: RGMII (1), SS=011, TX clock = clk_ref_i / 2. */
#define FES_TX_EMAC_REF_HZ 250000000U
#define FES_TX_RGMII_SELECT 1U
#define FES_TX_GIGABIT_SPEED (3U << 29)

#define FES_TX_TABLES 0x94000000U
#define FES_TX_CMDQ   0x94008000U
#define FES_TX_EVTQ   0x94009000U
#define FES_TX_DESC   0x94800000U
#define FES_TX_DATA   0x94801000U
#define FES_TX_IOVA   0x94802000U
#define FES_TX_BUFFER 0x94803000U

struct fes_tx_caps {
    uint32_t version, feature0, feature1, feature2, interface;
};
struct fes_tx_phy_sample {
    uint64_t time_us;
    uint16_t id1, id2, bmcr, bmsr_latch, bmsr, adv, partner, gig_adv, gig_partner, pcs, resolved;
};
struct fes_tx_result;
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
    /* Read-only CPU and register-access scope. Only the zero-write terminal
     * path may use this. It cannot grant a hardware write or DMA dispatch. */
    int (*observe_unchanged)(void *);
    void (*report_caps)(void *, const struct fes_tx_caps *);
    void (*report_phy)(void *, unsigned, const struct fes_tx_phy_sample *);
#ifdef FES_CBL_BIDIRECTIONAL
    void (*report_grant)(void *, unsigned vmid, unsigned command, unsigned ste);
#endif
};
enum fes_tx_stage {
    FES_TX_INITIAL, FES_TX_TABLE_BUILD, FES_TX_ARM, FES_TX_RELEASE_MAC,
    FES_TX_PHY, FES_TX_DISPATCH, FES_TX_STOP, FES_TX_REVOKE, FES_TX_PARKED
};
struct fes_tx_result {
    enum fes_tx_stage stage;
#ifdef FES_CBL_BIDIRECTIONAL
    struct fes_rx_result *rx;
    struct fes_tx_result *previous;
    unsigned vmid, command, retained, consumed;
    uint8_t nonce[16];
    uint64_t dwell_us;
#endif
    uint32_t last_address, last_value, descriptor_status, channel_status;
    unsigned scope_lost, write_attempted, arm_attempted, mapped, mac_release_attempted, dma_possible, dispatched;
    uint32_t terminal_per0, terminal_route, terminal_sid, terminal_cr0, terminal_ack, terminal_ste;
    uint32_t fault_address, fault_value;
    uint16_t phy_status;
    uint32_t drain_dma, drain_mtl, drain_status;
    unsigned process_stopped, revoke_started, translation_revoked, mac_in_reset;
    uint64_t started_us, last_us;
};
enum fes_tx_disposition { FES_TX_COLD_RECOVERY, FES_TX_CLOSED, FES_TX_UNCHANGED, FES_TX_REVOKED_UNDISPATCHED, FES_TX_FAILED_DRAINED };
enum fes_tx_disposition fes_tx_terminal(const struct fes_tx_io *, struct fes_tx_result *, int rc);
int fes_tx_once(const struct fes_tx_io *, const uint8_t frame[60], struct fes_tx_result *);
#endif
