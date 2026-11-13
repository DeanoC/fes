/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_BOOT_H
#define FES_NANO_BOOT_H
#include "nano_bridges.h"

/* Secure C handoff, never an NS/SiP message. The independent platform policy
 * qualifies the chain, history, fabric wiring and complete producer inventory.
 * A matching hash is identity, not authentication or physical qualification. */
#define FES_NANO_BOOT_MAGIC 0x464e4231U
#define FES_NANO_BOOT_VERSION 1U
#define FES_NANO_BOOT_NEVER_ENABLED 1U
#define FES_NANO_BOOT_ACTIVE 2U
#define FES_NANO_BOOT_SOURCE_QUALIFIED 1U
#define FES_NANO_BOOT_HISTORY_QUALIFIED 2U
#define FES_NANO_BOOT_MASTERS_QUALIFIED 4U
#define FES_NANO_BOOT_FABRIC_QUALIFIED 8U
#define FES_NANO_BOOT_QUALIFIED 15U
#define FES_NANO_PRODUCERS_HPS 0x1ffU /* EMAC0..2, USB0..1, NAND, SD, DMA, other CPUs */
#define FES_NANO_PRODUCERS_FPGA 0x200U /* every fabric producer, not just scanout */
#define FES_NANO_PRODUCERS_ALL 0x3ffU
#define FES_NANO_MASTER_FUNCTION 0xff0100bfU
#define FES_NANO_MASTER_OCP 0x0020bf00U
#define FES_NANO_MASTER_RESET (FES_NANO_MASTER_FUNCTION | FES_NANO_MASTER_OCP)
#define FES_NANO_MASTER_POLLS 100U
/* Only a compiled, live-audited USB0 PIO policy may retain this exact pair. */
#define FES_NANO_MASTER_USB0_PIO_RESET 0x808U

struct fes_nano_boot_record {
    uint32_t magic, version, size, reserved;
    uint64_t owner, epoch;
    uint8_t chain[32];
    uint32_t history, fabric, producers, reserved_end;
};
struct fes_nano_boot_policy {
    uint8_t chain[32];
    uint32_t qualifications, history, fabric, producers;
};
struct fes_nano_master_ops {
    void *context;
    int (*owns)(void *, uint64_t owner, uint64_t epoch);
    /* Success stops new requests for ALL producers. The following independent
     * bitmap must prove their DMA/transactions drained before reset writes. */
    int (*quiesce)(void *, uint32_t producers);
    int (*quiescent)(void *, uint32_t *producers);
    int (*read_per0)(void *, uint32_t *reset);
    int (*assert_per0)(void *, uint32_t mask);
    void (*barrier)(void *);
    void (*wait)(void *); /* bounded delay, 10 us in the real TF-A adapter */
};
/* Optional trusted post-boot policy. observe returns the exact reset bits whose
 * controller is retained with DMA disabled and its interconnect master idle.
 * quiesce/quiescent still cover ALL memory producers, including independent
 * FPGA stop/drain. USB packets may continue; USB DMA may not. No NS binding. */
struct fes_nano_retained_master_ops {
    void *context;
    int (*observe)(void *,uint64_t owner,uint64_t epoch,uint32_t *retained_reset);
    int (*quiesce)(void *,uint32_t producers);
    int (*quiescent)(void *,uint32_t *producers);
    /* Optional complete coherent read-only observation. It validates live CPU
     * and cold ownership as well as owner/epoch, private device generation,
     * retained reset mask and DMA/idle around the producer bitmap. Thus its
     * consumer must not nest ordinary full owns() callbacks around it. Only
     * its trusted supplier may select this path. Returned bridge/peripheral
     * reset values must come from matching, ordered complete cold samples
     * inside that CPU guard; no consumer MMIO precedes the guard. */
    int (*snapshot)(void *,uint64_t owner,uint64_t epoch,
                    uint32_t *retained_reset,uint32_t *producers,uint32_t *bridge_reset,uint32_t *per0);
};
struct fes_nano_boot {
    struct fes_nano_master_ops masters;
    struct fes_nano_bridge_ops raw;
    uint64_t owner, epoch;
    unsigned attempted, failed, producers_stopped;
    int first_error;
    struct fes_nano_retained_master_ops retained;
    /* Immutable callback identity for the optional combined observer, not a
     * cache of hardware observations. Replacing one legacy callback must not
     * silently leave the combined callback authoritative. */
    struct fes_nano_retained_master_ops snapshot_policy;
    unsigned retained_attempted,retained_bound;
};

/* Zero-initialize once per secure boot; even a failed handoff consumes the
 * attempt. No record pointer or caller-owned policy is retained. */
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_boot_accept(struct fes_nano_boot *, struct fes_nano_bridges *,
                         const struct fes_nano_boot_record *, const struct fes_nano_boot_policy *,
                         struct fes_nano_master_ops, struct fes_nano_bridge_ops raw);
void fes_nano_boot_invalidate(struct fes_nano_boot *);
/* Bind once to this exact accepted cold bridge object while all devices and
 * bridges remain held. Failed or duplicate setup cannot replace authority. */
int fes_nano_boot_bind_usb_pio(struct fes_nano_boot *,struct fes_nano_bridges *,
    uint64_t owner,uint64_t epoch,struct fes_nano_retained_master_ops);
/* Cached secure binding identity only; device/bridge operations must still
 * observe their live hardware scopes under the same lock. */
/* Pass the supplier's exact identities directly, avoiding a by-value callback
 * table in the per-access path. This never caches an observation or result. */
int fes_nano_boot_usb_pio_matches(const struct fes_nano_bridges *,void *context,
    int (*observe)(void *,uint64_t,uint64_t,uint32_t *),
    int (*quiesce)(void *,uint32_t),int (*quiescent)(void *,uint32_t *),
    int (*snapshot)(void *,uint64_t,uint64_t,uint32_t *,uint32_t *,uint32_t *,uint32_t *));
#ifdef __cplusplus
}
#endif
#endif
