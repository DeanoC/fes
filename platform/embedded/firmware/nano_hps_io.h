/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_HPS_IO_H
#define FES_NANO_HPS_IO_H
#include <stdint.h>
#include "nano_bridges.h"
#define FES_NANO_HPS_IO_BASE 0x10d12000ULL
#define FES_NANO_HPS_IO_COUNT 164U
struct fes_nano_hps_io_register { uint32_t offset, value, mask; };
struct fes_nano_hps_io_ops {
    void *context;
    int (*owns)(void *, uint64_t owner, uint64_t generation);
    int (*read)(void *, uint32_t offset, uint32_t *value);
    void (*barrier)(void *);
    void (*trace)(void *, unsigned pass, unsigned index, uint32_t offset,
                  uint32_t expected, uint32_t observed);
};
struct fes_nano_hps_io {
    struct fes_nano_hps_io_ops io;
    const struct fes_nano_hps_io_register *registers;
    uint8_t digest[32];
    uint32_t first[FES_NANO_HPS_IO_COUNT];
    uint64_t owner, generation;
    unsigned initialized, failed, pending, reads;
    int first_error;
};
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_hps_io_offset(uint32_t offset);
int fes_nano_hps_io_init(struct fes_nano_hps_io *, struct fes_nano_hps_io_ops,
                         const struct fes_nano_hps_io_register *, unsigned count, const uint8_t digest[32]);
void fes_nano_hps_io_sip(struct fes_nano_hps_io *, uint64_t operation,
                        uint64_t owner, uint64_t generation, unsigned busy, uint64_t result[4]);
#ifdef __cplusplus
}
#endif
#endif
