/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_ROM_H
#define FES_NANO_ROM_H
#include <stddef.h>
#include <stdint.h>
#include "generated/fes_embedded_rom.h"

/* Unbound transport, not an SMC or authority constructor. Secure platform
 * code keeps this object private, holds its lifecycle lock, supplies relative
 * MMIO hooks and independently checks the current owner/epoch, control-only
 * bridge scope, parked HPS masters and held emulation CPU. Hook calls must
 * return within a bound. now() counts the qualified 400 MHz system timer.
 * Source bytes/identity must already be retained and admitted by the caller.
 */
struct fes_nano_rom_ops {
    void *context;
    int (*owns)(void *, uint64_t owner, uint64_t generation); /* exactly one */
    int (*read32)(void *, uint32_t offset, uint32_t *value); /* zero on success */
    int (*write32)(void *, uint32_t offset, uint32_t value);
    uint64_t (*now)(void *);
    void (*barrier)(void *);
};
enum fes_nano_rom_error {
    FES_NANO_ROM_ARGUMENT=-1, FES_NANO_ROM_OWNER=-2, FES_NANO_ROM_IO=-3,
    FES_NANO_ROM_IDENTITY=-4, FES_NANO_ROM_STATE=-5, FES_NANO_ROM_LIMIT=-6,
    FES_NANO_ROM_INTEGRITY=-7, FES_NANO_ROM_BUSY=-8
};
/* Optional trusted supplier for one natural 16-byte FIFO protocol step. The
 * caller holds the secure lifecycle lock throughout. begin/end(success=1)
 * provide complete fresh ownership framing; each read/write still calls
 * owns(), whose supplier independently guards CPU/reset/access safety.
 * end(success=0) consumes the private step without reads, writes or retries.
 * Both hooks return zero on success. A failed begin leaves no permission;
 * every end consumes it before checking success. Callbacks are copied once.
 * No caller outside the trusted platform may supply or retain these hooks. */
struct fes_nano_rom_steps {
    void *context;
    int (*begin)(void *,uint64_t owner,uint64_t generation,uint32_t offset);
    int (*end)(void *,uint64_t owner,uint64_t generation,uint32_t offset,unsigned success);
};
struct fes_nano_rom {
    struct fes_nano_rom_ops io;
    uint64_t owner, generation, began, last;
    uint32_t build[4], crc, bytes;
    unsigned initialized, busy, used, failed, complete;
    int first_error;
    uint8_t image[FES_EMBEDDED_ROM_MAX_BYTES];
};
#ifdef __cplusplus
extern "C" {
#endif
/* Zero once; trusted setup only. Expected build words are manifest order. */
int fes_nano_rom_init(struct fes_nano_rom *, struct fes_nano_rom_ops,
                     uint64_t owner, uint64_t generation, const uint32_t build[4]);
/* One transfer: private source copy, identity, held state, bounded FIFO writes,
 * exact counters/CRC and final identity. It never opens a bridge, releases the
 * CPU, retries uncertain I/O, resets the sink or frees memory. On failure the
 * enclosing platform owns containment. A completion is saved evidence only.
 */
int fes_nano_rom_load(struct fes_nano_rom *, const uint8_t *, size_t, uint32_t crc32);
/* Secure provider variant: verify the private captured image against its saved
 * digest before any endpoint I/O. A failed check retires this transfer. */
int fes_nano_rom_load_checked(struct fes_nano_rom *, const uint8_t *, size_t, uint32_t crc32,
    int (*verify)(void *, const uint8_t *, size_t), void *context);
/* Same snapshot, identity, counter, CRC and deadline checks, with an explicitly
 * selected private step supplier. Missing/partial hooks reject before I/O. */
int fes_nano_rom_load_steps_checked(struct fes_nano_rom *,const uint8_t *,size_t,uint32_t,
    int (*verify)(void *,const uint8_t *,size_t),void *,const struct fes_nano_rom_steps *);
#ifdef __cplusplus
}
#endif
#endif
