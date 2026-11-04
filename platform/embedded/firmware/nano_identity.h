/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_IDENTITY_H
#define FES_NANO_IDENTITY_H
#include "nano_bridges.h"
#include "generated/fes_embedded_bridges.h"
#include "generated/fes_embedded_control.h"
#define FES_NANO_READER_WINDOW 3U

struct fes_nano_identity_ops {
    void *context;
    /* Read-only SDM status: exactly 1 ready, 0 not ready, negative on error.
     * No bridge mutation or endpoint access is permitted by this callback. */
    int (*configured)(void *);
    /* Fixed platform aperture only; no address or write comes from NS. */
    int (*read32)(void *, uint32_t offset, uint32_t *);
    /* Assert control reset and independently check data containment. Retire
     * platform authority even if that check fails. Never grant a new owner. */
    int (*contain)(void *);
    void (*trace)(void *, unsigned stage); /* optional saved-state progress */
    /* Optional fixed diagnostic responder writes. Together with the on-chip
     * ABI this permits only its bounded recipe, never DDR or general MMIO. */
    int (*write32)(void *, uint32_t offset, uint32_t value);
    /* Optional sealed DDR diagnostic only. Prepare checks the fixed buffer,
     * traffic attributes, live translation/firewall state and other masters
     * while data is held. Cycle runs only after owned DDR-only opening; its
     * receipt never substitutes for the subsequent all-master/bridge close. */
    int (*ddr_prepare)(void *);
    int (*ddr_cycle)(void *);
};
struct fes_nano_identity_probe {
    struct fes_nano_bridges *bridges;
    struct fes_nano_identity_ops io;
    uint32_t abi, build[4];
    struct fes_nano_bridge_closure closure; /* populated only after verified close */
    unsigned initialized, failed, attempted, queried, active, stage, reads, reader;
    int first_error, cleanup_error;
};
enum fes_nano_identity_stage {
    FES_ID_NONE, FES_ID_INIT, FES_ID_OPEN, FES_ID_READ, FES_ID_MATCH,
    FES_ID_CLOSE, FES_ID_CONTAIN, FES_ID_COMPLETE, FES_ID_CONFIG,
    FES_ID_READER_ENABLED, FES_ID_READER_HELD, FES_ID_READER_DISABLED,
    FES_ID_READER_PARTIAL, FES_ID_READER_DRAINED,
    FES_ID_DDR_PREPARE, FES_ID_DDR_RELEASE, FES_ID_DDR_READ, FES_ID_DDR_VERIFY
};
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_identity_init(struct fes_nano_identity_probe *, struct fes_nano_bridges *,
                             struct fes_nano_identity_ops, uint32_t abi,
                             const uint32_t build[4], unsigned qualified);
void fes_nano_identity_sip(struct fes_nano_identity_probe *, uint64_t owner,
                            uint64_t generation, unsigned configuring, uint64_t result[4]);
void fes_nano_identity_configuration_sip(struct fes_nano_identity_probe *, uint64_t owner,
                            uint64_t generation, unsigned configuring, uint64_t result[4]);
void fes_nano_reader_sip(struct fes_nano_identity_probe *, uint64_t owner,
                            uint64_t generation, unsigned configuring, uint64_t result[4]);
/* Secure platform only. Default weak providers return no probe/permission. */
struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void);
int fes_nano_platform_identity_setup(void);
int fes_nano_platform_control_target(unsigned open);
int fes_nano_platform_ddr_target(unsigned open);
/* Source-bound window gate; runs only in the owned control scope with data held. */
int fes_nano_platform_window_prepare(void);
void fes_nano_platform_window_report(unsigned);
/* Cached complete window stop/closure proof under the shared platform lock. */
int fes_nano_platform_window_return(uint64_t *generation);
/* Consume a completed window/SD return before configuration mutates its state.
 * Called only by the owned dispatcher under the shared platform lock. */
int fes_nano_platform_window_rearm(uint64_t owner, uint64_t generation);
void fes_nano_platform_identity_report(void);
#ifdef __cplusplus
}
#endif
#endif
