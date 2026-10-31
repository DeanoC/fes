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
struct fes_nano_boot {
    struct fes_nano_master_ops masters;
    struct fes_nano_bridge_ops raw;
    uint64_t owner, epoch;
    unsigned attempted, failed, producers_stopped;
    int first_error;
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
#ifdef __cplusplus
}
#endif
#endif
