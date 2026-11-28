/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MAC_RX_DIAGNOSTIC_H
#define FES_NANO_MAC_RX_DIAGNOSTIC_H
#include "mac_tx_diagnostic.h"

/* The bidirectional profile reserves these addresses before table publication.
 * RX descriptors share only the already writable TX descriptor page. The new
 * nonidentity data mapping permits device writes only. Its IOVA's physical
 * backing must remain empty, as in the TX profile. */
#define FES_RX_DESC (FES_TX_DESC + 0x800U)
#define FES_RX_RING_IOVA (FES_TX_IOVA + 0x800U)
#define FES_RX_DATA 0x94804000U
#define FES_RX_DATA_IOVA 0x94805000U
#define FES_RX_OFFSET 128U
#define FES_RX_CAPACITY 1600U
#define FES_RX_ENTRIES 32U
#define FES_RX_BYTES 60U

struct fes_rx_path {
    const struct fes_tx_io *io;
    void *trial;
    /* Zero only under the outer sticky scope, deadline, SMMU and all leaf
     * checks. This module has no independent hardware grant. */
    int (*guard)(void *trial);
};
enum fes_rx_failure { FES_RX_NO_FAILURE, FES_RX_TIMED_OUT, FES_RX_BAD_DESCRIPTOR, FES_RX_WRONG_FRAME };
/* Reserve this structure in CPU-only SPL storage outside every translated
 * page. It preserves an exact image, even of unexpected input, across revoke. */
struct fes_rx_stop_observation {
    unsigned phase, shared;
    uint32_t cfg, ctrl, status, mac, mtl, dma, ch, post_ctrl;
};
struct fes_rx_result {
    struct fes_rx_stop_observation stop;
    uint64_t retired_desc[2], retired_buffer[FES_RX_CAPACITY / 8];
    enum fes_rx_failure failure;
    unsigned arm_attempted, dma_possible, armed, wait_attempted, matched, stop_attempted, stopped;
    uint32_t desc_status, channel_status, mac_debug, mtl_debug, dma_debug, ch_debug;
    uint8_t expected[FES_RX_BYTES], bytes[FES_RX_BYTES];
};
/* Call once per zero-initialized result. The outer owner sets shared AXI and
 * 16-byte descriptor mode plus MAC0's station address with both engines off.
 * It holds and rechecks the translated domain through both process stops and
 * final revoke. Treat any error after dma_possible as uncertain until closed. */
int fes_rx_arm(const struct fes_rx_path *, struct fes_rx_result *, const uint8_t request[FES_RX_BYTES]);
int fes_rx_wait(const struct fes_rx_path *, struct fes_rx_result *);
/* Stop TX first so the shared AXI channel can independently go idle.
 * A stopped failed observation remains failed and cannot establish delivery. */
int fes_rx_stop(const struct fes_rx_path *, struct fes_rx_result *);
/* The outer owner can repeat this memory-only observation after its verified
 * abort STE, invalidation, reset and dwell. Supply that terminal guard. */
int fes_rx_retained(const struct fes_rx_path *, const struct fes_rx_result *);
#endif
