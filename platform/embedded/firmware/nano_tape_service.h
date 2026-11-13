/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_TAPE_SERVICE_H
#define FES_NANO_TAPE_SERVICE_H
#include "nano_tape.h"
#include "generated/fes_embedded_tape.h"

/* Trusted fixed-window supplier, under the secure lifecycle lock. guard is
 * the complete fresh active-program/CPU/master proof, not cached permission.
 * read reads only the fixed descriptor. verify hashes only the fixed source
 * bytes and compares SHA-384. They return zero on success, synchronously.
 * Expected identity comes from accepted secure program state, never header
 * metadata. No NS request supplies an address, callback, identity or timer.
 */
struct fes_nano_tape_service_ops {
    void *context;
    int (*guard)(void *,uint64_t owner,uint64_t generation);
    int (*read)(void *,uint8_t header[FES_EMBEDDED_TAPE_HEADER_BYTES]);
    int (*verify)(void *,uint32_t bytes,const uint8_t sha384[48]);
};
struct fes_nano_tape_service {
    struct fes_nano_tape_service_ops io;
    struct fes_nano_tape engine;
    uint8_t header[FES_EMBEDDED_TAPE_HEADER_BYTES];
    uint64_t began,last,step_began,budget;
    unsigned sequence;
    unsigned initialized,used,failed,complete,calling;
    int first_error;
};
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_tape_service_init(struct fes_nano_tape_service *,struct fes_nano_tape_service_ops,
    struct fes_nano_tape_ops,uint64_t owner,uint64_t generation,const uint16_t identity[16],uint32_t hz);
/* Called only after exact FID/cookie decoding under the existing SiP lock.
 * Wrong operation/owner/generation and replay perform no supplier access.
 * Pending replies carry verified byte progress/generation/next sequence only.
 * Only complete success publishes final bytes/generation/GPO/CRC. Any attempted
 * uncertain capture retires; the physical caller owns containment. */
void fes_nano_tape_service_call(struct fes_nano_tape_service *,uint64_t operation,
    uint64_t owner,uint64_t generation,uint64_t result[4]);
#ifdef __cplusplus
}
#endif
#endif
