/* SPDX-License-Identifier: BSD-3-Clause */
#pragma once
#include <stdint.h>

void fes_nano_mailbox_observe(unsigned phase, unsigned completed, unsigned pending,
                              unsigned job, uint64_t owner, uint64_t generation);
