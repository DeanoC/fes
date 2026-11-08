/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_TAPE_H
#define FES_NANO_TAPE_H
#include <stdint.h>
#include "generated/fes_computer.h"
#include "generated/fes_de25_computer.h"

#define FES_NANO_TAPE_POLLS 64U
#define FES_NANO_TAPE_IDENTITY_WORDS 16U /* explicitly selected GP ABI 1.0 */
enum fes_nano_tape_phase { FES_TAPE_PREPARE, FES_TAPE_CHUNK, FES_TAPE_FINISH };
enum fes_nano_tape_error {
    FES_TAPE_ARGUMENT=-1, FES_TAPE_OWNER=-2, FES_TAPE_IO=-3,
    FES_TAPE_IDENTITY=-4, FES_TAPE_STATE=-5, FES_TAPE_LIMIT=-6,
    FES_TAPE_INTEGRITY=-7, FES_TAPE_BUSY=-8
};
/* Unbound secure-side transport. The trusted supplier holds its secure
 * lifecycle lock throughout load; NS execution/other CPUs cannot run and all
 * DMA/data masters stay contained. begin/end frame the complete live proof
 * around preparation, each <=512-byte chunk and completion. Every CSR still
 * has fresh before/after owns checks for CPU/reset/access safety. owns must
 * not infer permission from a cached frame alone. No supplied physical address
 * or deadline enters this interface. All callbacks have bounded return.
 *
 * image reads only the supplier's fixed, retained source window. end(0)
 * consumes permission without I/O or retry. end(1) consumes before checking
 * the fresh complete proof. There is no fallback for missing frame hooks.
 * Constructor-only ticks_per_second must match the qualified monotonic timer;
 * every public load has a fixed one-second total bound, including callbacks.
 */
struct fes_nano_tape_ops {
    void *context;
    int (*owns)(void *,uint64_t owner,uint64_t generation); /* exactly 1 */
    int (*begin)(void *,uint64_t owner,uint64_t generation,unsigned phase,uint32_t offset);
    int (*end)(void *,uint64_t owner,uint64_t generation,unsigned phase,uint32_t offset,unsigned success);
    int (*read32)(void *,uint32_t offset,uint32_t *);
    int (*write32)(void *,uint32_t offset,uint32_t);
    int (*image)(void *,uint32_t offset,uint8_t *,uint32_t bytes);
    uint64_t (*now)(void *);
    void (*barrier)(void *);
};
struct fes_nano_tape_receipt {
    uint64_t owner,generation;
    uint32_t bytes,crc,gpo,fetches;
    uint16_t address;
};
struct fes_nano_tape {
    struct fes_nano_tape_ops io;
    uint64_t owner,generation,began,last;
    uint32_t hz,gpo,bytes,crc,reads,writes,polls,offset,opcode,index;
    uint32_t fetches,fetch_address,frame_offset;
    uint16_t identity[FES_NANO_TAPE_IDENTITY_WORDS];
    uint8_t chunk[FES_COMPUTER_MEDIA_CHUNK_MAX_BYTES];
    unsigned initialized,busy,used,failed,complete,framed,phase;
    int first_error;
};
#ifdef __cplusplus
extern "C" {
#endif
/* Trusted setup, zero once; selected identity words are not request metadata. */
int fes_nano_tape_init(struct fes_nano_tape *,struct fes_nano_tape_ops,
    uint64_t owner,uint64_t generation,const uint16_t identity[FES_NANO_TAPE_IDENTITY_WORDS],uint32_t ticks_per_second);
/* One held transfer. Initial GPO must be the matching acknowledged Hold;
 * returned GPO synchronizes the caller's subsequent protocol commands. Check
 * selected identity before and after, CRC before begin and before commit,
 * exact chunk/ACK/state and unchanged fetch count/address. Never Run, reset,
 * recover, retry an uncertain write or release source memory. Failure retires
 * this object and leaves receipt untouched; physical containment is external.
 */
int fes_nano_tape_load(struct fes_nano_tape *,uint64_t owner,uint64_t generation,
    uint32_t initial_gpo,uint32_t bytes,uint32_t crc,struct fes_nano_tape_receipt *);
/* Trusted service-only origin from this same qualified timer. Includes its
 * descriptor/hash prologue in the original one-second bound; never an NS
 * deadline or extra allowance. previous preserves the last prologue sample;
 * backwards time across the service/engine boundary also rejects. */
int fes_nano_tape_load_started(struct fes_nano_tape *,uint64_t owner,uint64_t generation,
    uint32_t initial_gpo,uint32_t bytes,uint32_t crc,struct fes_nano_tape_receipt *,uint64_t origin,uint64_t previous);
#ifdef __cplusplus
}
#endif
#endif
