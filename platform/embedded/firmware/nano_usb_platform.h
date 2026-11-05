/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_USB_PLATFORM_H
#define FES_NANO_USB_PLATFORM_H
#include "nano_cold.h"

#define FES_NANO_USB_BASE 0x10b00000UL
#define FES_NANO_USB_RESET FES_NANO_COLD_USB_RESET /* USB0 module and packet-RAM/ECC */
#define FES_NANO_USB_BOOT_SCOPE 1U
#define FES_NANO_USB_CONTROLLER_RESET 2U
#define FES_NANO_USB_CLOCK_PHY_VBUS 4U
#define FES_NANO_USB_PIO_DRAIN 8U
#define FES_NANO_USB_QUALIFIED 15U
/* Trusted compiled callbacks only. No SMC arguments, default readiness or
 * physical MMIO binding. Serialize acquire/observe/stop with driver and bridge
 * operations, and retain this object until reboot even after failure. */
struct fes_nano_usb_io {
    void *context;
    int (*read)(void *, uintptr_t, uint32_t *);
    int (*write)(void *, uintptr_t, uint32_t);
    void (*barrier)(void *);
    void (*wait)(void *); /* fixed bounded wait, e.g. 10us; never unbounded */
    /* Live confirmation after the host driver starts: verify PHY identity,
     * synchronous ULPI, host mode and internally sensed VBUS. */
    int (*ready)(void *);
    /* Before controller access: verify fixed board pins, bus clocks and
     * ownership. This is preparation, never live PHY/VBUS readiness. */
    int (*prepare)(void *);
    /* Optional complete confirmed observation from the trusted physical
     * supplier: fresh clock/pin/owner checks bracket both PHY observations,
     * with DMA/IRQ/host/PIO validation and final independent cold scope.
     * No state cache, mutation or generation publication. It subsumes the
     * separate prepare/PIO/ready/prepare sequence only after confirmation. */
    int (*ready_scope)(void *,struct fes_nano_cold *,uint64_t owner,uint64_t epoch);
};
struct fes_nano_usb_platform {
    struct fes_nano_cold *cold;
    struct fes_nano_usb_io io;
    uint64_t owner, epoch, generation;
    unsigned attempted, initialized, active, confirmed, failed;
    unsigned channels;
    int first_error;
    uintptr_t last_address;
    uint32_t last_value;
};
int fes_nano_usb_platform_init(struct fes_nano_usb_platform *, struct fes_nano_cold *,
                              struct fes_nano_usb_io, unsigned qualifications);
int fes_nano_usb_platform_observe(struct fes_nano_usb_platform *, uint64_t owner,
                                 uint64_t generation);
int fes_nano_usb_platform_acquire(struct fes_nano_usb_platform *, uint64_t owner,
                                 uint64_t generation);
/* Preparation grants only this USB-only controller lease. The host driver must
 * finish initialization before confirmation publishes a fresh ready generation. */
int fes_nano_usb_platform_confirm(struct fes_nano_usb_platform *, uint64_t owner,
                                 uint64_t generation);
int fes_nano_usb_platform_stop(struct fes_nano_usb_platform *, uint64_t owner,
                              uint64_t generation);
/* Confirmed shared PIO owner only: independently observe the DMA-disabled
 * interconnect master idle before granting a retained-reset stop policy.
 * Fresh clock/pin/boot scope brackets live DMA/IRQ/PIO and AHB-idle checks.
 * This is not PHY/device readiness; issues no writes and publishes no state. */
int fes_nano_usb_platform_idle(struct fes_nano_usb_platform *,uint64_t owner,uint64_t generation);
#endif
