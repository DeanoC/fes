/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_CAPABILITY_H
#define FES_NANO_CAPABILITY_H
#include <stdint.h>
#define FES_NANO_CAPABILITY_WORDS 13U
enum fes_nano_capability_word {
    FES_CAP_IDR0, FES_CAP_IDR1, FES_CAP_IDR3, FES_CAP_IDR5,
    FES_CAP_CR1, FES_CAP_CR2, FES_CAP_SDM_CTRL, FES_CAP_SDM_IDS,
    FES_CAP_SDM_AR, FES_CAP_SDM_AW, FES_CAP_POR1,
    FES_CAP_MAC_CTRL, FES_CAP_MAC_IDS
};
struct fes_nano_capability_snapshot { uint32_t word[FES_NANO_CAPABILITY_WORDS]; };
struct fes_nano_capability_io {
    void *context;
    int (*owns)(void *);
    int (*read32)(void *, uint32_t address, uint32_t *value);
};
struct fes_nano_capability_probe {
    struct fes_nano_capability_io io;
    unsigned attempted, reads;
    int error;
};
/* Fixed, read-only whitelist, independent of callers' register selectors. */
uint32_t fes_nano_capability_address(unsigned index);
/* One attempt, two identical samples, ownership around every read and at
 * publication. Failure leaves output unchanged; a consumed attempt cannot
 * retry. Raw register values confer no access, isolation or drain authority. */
int fes_nano_capability_observe(struct fes_nano_capability_probe *,
                              struct fes_nano_capability_snapshot *);
#endif
