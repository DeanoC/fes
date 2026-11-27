/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MAC_PHY_H
#define FES_NANO_MAC_PHY_H
#include <stdint.h>

/* Trusted supplier callbacks under the same platform lock for the entire call.
 * owned checks the CPU, boot/owner/epoch, fixed MAC routing and DMA aperture.
 * drained independently proves all device activity and outstanding DMA stopped.
 * Both return exactly one on success. These are never nonsecure parameters. */
struct fes_nano_mac_phy_guards {
    void *context;
    int (*owned)(void *);
    int (*drained)(void *);
};
struct fes_nano_mac_phy {
    uint32_t original_mode;
    uint16_t bmcr, adv, gig_adv, selector, pointer, dev0_pointer, high, low;
    uint8_t begun, released, portal, pointers, snapshot, timing_changed, ready;
};
/* Zero before first use. Only a verified retire permits another prepare.
 * Failed operations retain their original snapshot. Do not reset the state
 * to work around an incomplete restoration. Zero return denotes success. */
int fes_nano_mac_phy_prepare(struct fes_nano_mac_phy *, const struct fes_nano_mac_phy_guards *);
int fes_nano_mac_phy_active(const struct fes_nano_mac_phy *, const struct fes_nano_mac_phy_guards *);
int fes_nano_mac_phy_retire(struct fes_nano_mac_phy *, const struct fes_nano_mac_phy_guards *);
#endif
