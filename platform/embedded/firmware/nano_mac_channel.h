/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MAC_CHANNEL_H
#define FES_NANO_MAC_CHANNEL_H
#include <stdint.h>

enum fes_nano_mac_channel_phase { FES_CHANNEL_NEW, FES_CHANNEL_PREPARED, FES_CHANNEL_ACTIVE,
    FES_CHANNEL_DRAINED, FES_CHANNEL_RETAINED, FES_CHANNEL_FAILED };
struct fes_nano_mac_channel {
    uint64_t generation, station, dwell_us;
    uint32_t phase, failed_stage, status, dma_debug, channel_debug, mac_debug, tx_debug, rx_debug;
    uint8_t arena_sha384[48];
    uint32_t stop_progress;
};
struct fes_nano_mac_channel_guards {
    void *context;
    /* Exactly one under the same security/lifecycle lock for all calls. Live:
     * sole CPU, clock, exact bound MAC/PHY, SID1/VMID, five pages and other
     * masters. Revoked: independently consumed invalidations/abort and original
     * domain. A failed grant cannot be proven contained by channel evidence. */
    int (*owned)(void *, uint64_t generation, unsigned revoked);
};
typedef struct fes_nano_mac_channel fes_channel;
typedef struct fes_nano_mac_channel_guards fes_channel_guards;
/* Strong trusted leaves. Prepare follows the fresh domain grant & PHY startup,
 * confirm follows actual driver initialization. Scope also admits the ordered
 * process-stop transitions and the stopped state required to enter STOP. Drain records the terminal
 * controller and arena. The drain guard is called throughout PHY retirement.
 * Retained is called after independent break/invalidate/abort and MAC reset.
 * Zero is success (including drained). Failed state retains all earlier proof. */
int fes_nano_mac_channel_prepare(fes_channel *, const fes_channel_guards *, uint64_t);
int fes_nano_mac_channel_confirm(fes_channel *, const fes_channel_guards *, uint64_t);
int fes_nano_mac_channel_scope(fes_channel *, const fes_channel_guards *, uint64_t);
int fes_nano_mac_channel_drain(fes_channel *, const fes_channel_guards *, uint64_t);
int fes_nano_mac_channel_drained(fes_channel *, const fes_channel_guards *, uint64_t);
int fes_nano_mac_channel_retained(fes_channel *, const fes_channel_guards *, uint64_t);
#endif
