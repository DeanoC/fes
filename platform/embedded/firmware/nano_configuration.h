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

/* Trusted platform check against its owned binding. No DMA address comes from
 * a package. A platform may bind a fresh source once before checking its bytes. */
struct fes_nano_configuration_ops {
    void *context;
    int (*check)(void *, uint64_t address, uint32_t bytes, unsigned whole);
    void (*trace)(void *, unsigned stage, uint32_t completed, uint32_t pending);
    /* Trusted completion only: install a fresh platform activation after exact
     * final status/digests, before publishing finished. Errors stay terminal. */
    int (*complete)(void *, uint64_t owner, uint64_t generation, unsigned rearmed);
    /* Optional for injected fixed-source engines. The physical cold provider
     * captures a per-load descriptor and returns its bounded size. Failure is
     * terminal; the engine never falls back to a preceding source. */
    int (*bind)(void *, uint64_t owner, uint64_t generation, uint32_t *bytes);
};
enum fes_nano_configuration_stage {
    FES_CONFIG_INIT, FES_CONFIG_ARM, FES_CONFIG_START, FES_CONFIG_WRITE,
    FES_CONFIG_POLL, FES_CONFIG_STATUS, FES_CONFIG_COMPLETE, FES_CONFIG_FAILURE
};
struct fes_nano_configuration {
    struct fes_nano_bridges *bridges;
    struct fes_nano_configuration_ops io;
    uint64_t base, owner, generation;
    uint32_t bytes, maximum, chunk, completed, pending;
    unsigned initialized, attempted, prepared, started, finished, failed, rearmed;
    unsigned stage, issued;
    int first_error;
    uint64_t source_release_source,source_release_generation;
    unsigned source_release_attempted,source_released;
};
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_configuration_init(struct fes_nano_configuration *, struct fes_nano_bridges *,
                                 struct fes_nano_configuration_ops, uint64_t base,
                                 uint32_t bytes, uint32_t chunk, unsigned qualified);
void fes_nano_configuration_prepare(struct fes_nano_configuration *, uint64_t owner,
                                     uint64_t generation, unsigned configuring, uint64_t result[4]);
/* Rearm only a successfully completed fixed-source transfer after a newer
 * owned all-master close. This never recovers a failed/ambiguous transaction. */
void fes_nano_configuration_rearm(struct fes_nano_configuration *, uint64_t owner,
                                   uint64_t generation, unsigned configuring, uint64_t result[4]);
/* Release the previous source under the same owned call lock, before CPU
 * overwrite. The strong provider must prove completed mailbox/parser/SMMU,
 * actual session return and stopped devices. No default permission exists. */
void fes_nano_configuration_release_source(struct fes_nano_configuration *, uint64_t owner,
                                            uint64_t generation, unsigned configuring, uint64_t result[4]);
int fes_nano_platform_release_source(struct fes_nano_configuration *, uint64_t owner, uint64_t generation);
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
