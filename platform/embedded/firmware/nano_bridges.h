/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_BRIDGES_H
#define FES_NANO_BRIDGES_H
#include <stdint.h>

/* Local firmware/platform prototype, not a SiP wire ABI or a board binding. */
#define FES_NANO_BRIDGES_ALL 15U
#define FES_NANO_BRIDGES_CONTROL 2U
#define FES_NANO_BRIDGES_DATA 13U
#define FES_NANO_BRIDGES_DRAIN 12U
#define FES_NANO_FABRIC_ABSENT 1U
#define FES_NANO_FABRIC_PRESENT 2U

struct fes_nano_bridge_ops {
    void *context;
    /* Stable boot-scoped exclusive ownership, invalidated by any unmanaged
     * change, including firmware paths or an external reset-and-restore. */
    int (*owns)(void *, uint64_t owner, uint64_t epoch);
    int (*read_reset)(void *, uint32_t *);
    int (*masters_contained)(void *); /* exactly 0 or 1; negative is error */
    int (*stop_producers)(void *); /* stop every producer, not only scanout */
    /* Return zero only after every required live poll and reset/ready step.
     * Preserve the first error; fabric=ABSENT skips only optional warnings. */
    int (*disable)(void *, uint32_t mask, unsigned fabric_handshake);
    int (*enable)(void *, uint32_t mask, unsigned fabric_handshake);
};

struct fes_nano_bridge_snapshot {
    uint64_t owner, generation;
    uint32_t reset, fabric;
    unsigned never_enabled, masters_contained;
};
struct fes_nano_bridge_closure {
    struct fes_nano_bridge_snapshot after;
    unsigned producers_quiescent, fabric_acknowledged, data_remained_contained;
    uint32_t hardware_drained;
};
struct fes_nano_bridges {
    struct fes_nano_bridge_ops ops;
    struct fes_nano_bridge_snapshot state;
    uint64_t epoch;
    /* A trusted device lease blocks observations/mutations until independent
     * containment restores the cached all-master state. */
    unsigned initialized, failed, access, device_lease;
};

/* Zero-initialize once per boot. Only trusted boot/platform code may initialize
 * this object. never_enabled is a boot-history proof, never an SMC argument or
 * an inference from reset bits. No default callbacks or cold proof exist. */
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_bridges_init(struct fes_nano_bridges *, struct fes_nano_bridge_ops,
                         uint64_t owner, uint64_t epoch, unsigned fabric,
                         unsigned never_enabled);
/* Output structures are written only on success. */
int fes_nano_bridges_observe(struct fes_nano_bridges *, struct fes_nano_bridge_snapshot *);
int fes_nano_bridges_close(struct fes_nano_bridges *, uint64_t owner, uint64_t generation,
                          unsigned require_fabric, unsigned preserve_data,
                          struct fes_nano_bridge_closure *);
int fes_nano_bridges_open(struct fes_nano_bridges *, uint64_t owner, uint64_t generation,
                         uint32_t mask, struct fes_nano_bridge_snapshot *);
/* Used by configuration START instead of a redundant shutdown. It performs no
 * bridge writes and accepts only this owner's still-proven closed state. */
int fes_nano_bridges_configuration_guard(struct fes_nano_bridges *);
#ifdef __cplusplus
}
#endif
#endif
