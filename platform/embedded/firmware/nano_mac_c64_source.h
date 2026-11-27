/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MAC_C64_SOURCE_H
#define FES_NANO_MAC_C64_SOURCE_H

#include <string.h>
#include "input_tables.h"
#include "nano_program.h"

/* Called with a C64 identity emitted by the matched package producer in
 * trusted firmware, and the protected, completed SDM binding. Role 3 also
 * serves Spectrum; neither the role nor the mailbox ABI identifies C64. */
static inline int fes_nano_program_matches_c64(const struct fes_nano_program *p,
                                 const struct fes_nano_spectrum48_program *identity,
                                 const struct nano_sdm_input_binding *source)
{
    if (!p || !identity || !source || !fes_nano_program_has_spectrum48_resources(p) ||
        !p->shared_core_shell || p->spectrum48 || !p->owner || !p->generation ||
        source->owner != p->owner || source->generation != p->generation || !source->epoch ||
        source->bytes != p->bytes || source->chunk != p->chunk ||
        memcmp(source->digest384, identity->sha384, sizeof(source->digest384)))
        return 0;
    return fes_nano_program_matches_upload(p,p->bytes,identity);
}

#endif
