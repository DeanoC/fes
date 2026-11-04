/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_USB_BOARD_H
#define FES_NANO_USB_BOARD_H
#include "nano_usb_platform.h"
#include "nano_hps_io.h"

#define FES_NANO_USB_BUS_HZ 200000000U
#define FES_NANO_USB_PHY_VENDOR 0x0424U
#define FES_NANO_USB_PHY_PRODUCT 0x0007U
#define FES_NANO_USB_CLOCK_COUNT 13U
/* Compiled policy only: copy the complete validated sealed HPS I/O map.
 * No caller-supplied MMIO addresses, PHY IDs, clock rates or masks at runtime. */
struct fes_nano_usb_board {
    struct fes_nano_cold *cold;
    struct fes_nano_usb_io physical;
    struct fes_nano_hps_io_register pins[FES_NANO_HPS_IO_COUNT];
    uint32_t clocks[FES_NANO_USB_CLOCK_COUNT];
    uint8_t policy_digest[32], phy[8];
    uint64_t owner, epoch;
    unsigned attempted, initialized, failed, live_checks;
    int first_error;
    uintptr_t last_address;
    uint32_t last_value;
};
int fes_nano_usb_board_init(struct fes_nano_usb_board *, struct fes_nano_cold *,
                            struct fes_nano_usb_io,
                            const struct fes_nano_hps_io_register *, unsigned count,
                            const uint8_t policy_digest[32]);
struct fes_nano_usb_io fes_nano_usb_board_ops(struct fes_nano_usb_board *);
#endif
