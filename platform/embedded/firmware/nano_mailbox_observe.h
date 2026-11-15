/* SPDX-License-Identifier: BSD-3-Clause */
#pragma once
#include <stdint.h>

void fes_nano_mailbox_observe(unsigned phase, unsigned completed, unsigned pending,
                              unsigned job, uint64_t owner, uint64_t generation);

/* Only after the cold gate has irreversibly retired configuration authority.
 * These observations cannot acknowledge a transfer or prove a DMA drain. */
#define FES_NANO_RETIRED_STATUS_WORDS 6U
#define FES_NANO_RETIRED_STATUS_POLLS 10000U
#define FES_NANO_RETIRED_STATUS_FIFO_WORDS 64U
struct fes_nano_retired_status {
    uint32_t header, data[FES_NANO_RETIRED_STATUS_WORDS];
    unsigned sent, received, skipped, polls, fifo_words;
    int status;
};
void fes_nano_mailbox_retired_status(struct fes_nano_retired_status *);
