/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_SMMU_H
#define FES_NANO_SMMU_H
#include <stdint.h>

/* Agilex 5 TCU, SMMUv3 architectural offsets. The platform supplies the fixed
 * secure aperture; no physical address or register selector comes from NS. */
#define FES_NANO_SMMU_BASE 0x16000000UL
#define FES_NANO_SMMU_IDR0 0x0000U
#define FES_NANO_SMMU_IDR1 0x0004U
#define FES_NANO_SMMU_AIDR 0x001cU
#define FES_NANO_SMMU_CR0 0x0020U
#define FES_NANO_SMMU_CR0ACK 0x0024U
#define FES_NANO_SMMU_GBPA 0x0044U
#define FES_NANO_SMMU_AGBPA 0x0048U
#define FES_NANO_SMMU_S_IDR1 0x8004U
#define FES_NANO_SMMU_S_CR0 0x8020U
#define FES_NANO_SMMU_S_CR0ACK 0x8024U
#define FES_NANO_SMMU_S_INIT 0x803cU
#define FES_NANO_SMMU_S_GBPA 0x8044U
#define FES_NANO_SMMU_S_AGBPA 0x8048U
#define FES_NANO_SMMU_ENABLE 1U
#define FES_NANO_SMMU_ABORT (1U << 20)
#define FES_NANO_SMMU_UPDATE (1U << 31)
#define FES_NANO_SMMU_SECURE_IMPL (1U << 31)

struct fes_nano_smmu_io {
    void *context;
    int (*owns)(void *); /* exactly one: trusted cold owner, all bridges closed */
    int (*read32)(void *, uint32_t offset, uint32_t *);
};
struct fes_nano_smmu_snapshot {
    uint32_t idr0, idr1, aidr, s_idr1;
    uint32_t cr0, cr0ack, gbpa, agbpa;
    uint32_t s_cr0, s_cr0ack, s_init, s_gbpa, s_agbpa;
};
struct fes_nano_smmu_observation {
    struct fes_nano_smmu_snapshot registers;
    unsigned identified, secure_implemented, controls_settled, bypass_controls_clear;
};
struct fes_nano_smmu_probe {
    struct fes_nano_smmu_io io;
    unsigned attempted, failed, reads;
    int first_error;
};

/* One bounded read-only observation: two identical samples, ownership around
 * every read, no writes, polling, retry or translation-table access. A result
 * describes only sampled controls. Even bypass_controls_clear is NOT a DDR
 * address, firewall, traffic-security or drain qualification/access grant. */
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_smmu_observe(struct fes_nano_smmu_probe *, struct fes_nano_smmu_observation *);
int fes_nano_smmu_offset(uint32_t offset);
#ifdef __cplusplus
}
#endif
#endif
