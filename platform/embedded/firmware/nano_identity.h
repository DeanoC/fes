/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_IDENTITY_H
#define FES_NANO_IDENTITY_H
#include "nano_bridges.h"
#include "generated/fes_embedded_bridges.h"
#include "generated/fes_embedded_control.h"

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
    /* Optional fixed diagnostic responder writes. Its presence permits only
     * the bounded reader probe; it supplies no DDR or general MMIO authority. */
    int (*write32)(void *, uint32_t offset, uint32_t value);
};
struct fes_nano_identity_probe {
    struct fes_nano_bridges *bridges;
    struct fes_nano_identity_ops io;
    uint32_t abi, build[4];
    unsigned initialized, failed, attempted, queried, active, stage, reads, reader;
    int first_error, cleanup_error;
};
enum fes_nano_identity_stage {
    FES_ID_NONE, FES_ID_INIT, FES_ID_OPEN, FES_ID_READ, FES_ID_MATCH,
    FES_ID_CLOSE, FES_ID_CONTAIN, FES_ID_COMPLETE, FES_ID_CONFIG,
    FES_ID_READER_ENABLED, FES_ID_READER_HELD, FES_ID_READER_DISABLED,
    FES_ID_READER_PARTIAL, FES_ID_READER_DRAINED
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
void fes_nano_platform_identity_report(void);
#ifdef __cplusplus
}
#endif
#endif
