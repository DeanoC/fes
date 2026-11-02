/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_CONFIGURATION_H
#define FES_NANO_CONFIGURATION_H
#include "nano_bridge_sip.h"

#define FES_NANO_CONFIG_START 0xc2000001U
#define FES_NANO_CONFIG_WRITE 0x42000002U
#define FES_NANO_CONFIG_COMPLETED 0xc2000003U
#define FES_NANO_CONFIG_ISDONE 0xc2000004U
#define FES_NANO_CONFIG_DRAM 0x80000000ULL
#define FES_NANO_CONFIG_BUFFERS 4U

/* Trusted platform check: compare the fixed staged whole image or indexed
 * chunk with its compiled digest. No non-secure address or digest is accepted. */
struct fes_nano_configuration_ops {
    void *context;
    int (*check)(void *, uint64_t address, uint32_t bytes, unsigned whole);
    void (*trace)(void *, unsigned stage, uint32_t completed, uint32_t pending);
};
enum fes_nano_configuration_stage {
    FES_CONFIG_INIT, FES_CONFIG_ARM, FES_CONFIG_START, FES_CONFIG_WRITE,
    FES_CONFIG_POLL, FES_CONFIG_STATUS, FES_CONFIG_COMPLETE, FES_CONFIG_FAILURE
};
struct fes_nano_configuration {
    struct fes_nano_bridges *bridges;
    struct fes_nano_configuration_ops io;
    uint64_t base, owner, generation;
    uint32_t bytes, chunk, completed, pending;
    unsigned initialized, attempted, prepared, started, finished, failed;
    unsigned stage, issued;
    int first_error;
};
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_configuration_init(struct fes_nano_configuration *, struct fes_nano_bridges *,
                                 struct fes_nano_configuration_ops, uint64_t base,
                                 uint32_t bytes, uint32_t chunk, unsigned qualified);
void fes_nano_configuration_prepare(struct fes_nano_configuration *, uint64_t owner,
                                     uint64_t generation, unsigned configuring, uint64_t result[4]);
int fes_nano_configuration_before(struct fes_nano_configuration *, uint32_t fid,
                                    uint64_t address, uint64_t bytes, unsigned configuring);
void fes_nano_configuration_after(struct fes_nano_configuration *, uint32_t fid, uint64_t result[4]);
unsigned fes_nano_configuration_busy(const struct fes_nano_configuration *);
/* Withdraw a running cold transfer without completing, draining or freeing it.
 * The caller may then collect one bounded diagnostic; every buffer stays owned. */
int fes_nano_configuration_retire(struct fes_nano_configuration *, uint64_t owner,
                                    uint64_t generation, unsigned configuring);
/* Strong secure platform only; no default programming authority. */
struct fes_nano_configuration *fes_nano_platform_configuration(void);
int fes_nano_platform_configuration_setup(void);
int fes_nano_platform_configuration_diagnostic_ready(void);
#ifdef __cplusplus
}
#endif
#endif
