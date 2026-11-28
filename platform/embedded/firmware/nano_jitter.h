#ifndef FES_NANO_JITTER_H
#define FES_NANO_JITTER_H

#include <stddef.h>
#include <stdint.h>

#define FES_JITTER_OSR 3U
#define FES_JITTER_MEMORY_BYTES 131072U
#define FES_JITTER_HASH_LOOPS 1U
#define FES_JITTER_TIMER_HZ 400000000U
#define FES_JITTER_SEED_BYTES 128U

struct fes_nano_jitter_metrics {
    unsigned int osr, flags, memory_bytes, hash_loops, internal_timer;
    unsigned int health, reinitializations;
    uint64_t ticks, time_reads, timer_gcd;
    size_t arena_peak;
    int status;
};

/* The firmware links one mode. A seed can be requested only once before EL1.
 * Each diagnostic collector is independent and never emits conditioned bytes. */
int fes_nano_jitter_seed(uint8_t *dst, size_t bytes, struct fes_nano_jitter_metrics *metrics);
/* Diagnostic counters latched at the first watchdog failure. */
struct fes_jitter_limit {
    const char *reason;
    uint64_t ticks, reads;
};
struct fes_jitter_limit fes_nano_jitter_record_limit(void);
int fes_nano_jitter_record_open(void);
int fes_nano_jitter_record_sample(uint64_t *raw_delta);
int fes_nano_jitter_record_close(struct fes_nano_jitter_metrics *metrics);

#endif
