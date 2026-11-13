/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_EVT_PUBLICATION_WAIT_H
#define NANO_EVT_PUBLICATION_WAIT_H
#include "event_capture.h"
#define NANO_EVT_WAIT_US UINT64_C(10000)
#define NANO_EVT_WAIT_POLLS 1024U
struct nano_evt_clock {
    void *context;
    /* Trusted fixed-frequency, monotonic microseconds. No delay callback. */
    int (*time)(void *,uint64_t *);
};
/* Distinct qualified source; a generic capture owns callback is not
 * sufficient. Exactly 1 means the supplier has freshly checked the trusted
 * boot/CPU/EL3 lock, all other masters, fixed NS queue/control/ACK/base and
 * unchanged empty consumer, no GERROR/GERRORN, callback identities and NC
 * memory policy. The caller proves this before/after each producer read and
 * counter call. There is no allocator/write/ACK/reset or arbitrary offset. */
struct nano_evt_wait_io {
    void *context;
    int (*owns)(void *,const struct nano_evt_binding *);
    int (*read_producer)(void *,uint32_t *);
    void (*barrier)(void *);
};
struct nano_evt_wait {
    struct nano_evt_binding binding;
    struct nano_evt_wait_io source;
    struct nano_evt_clock clock;
    uint64_t start,last,elapsed;
    uint32_t producer;
    unsigned attempted,initialized,begin_attempted,begun,finish_attempted;
    unsigned failed,timed_out,poll_limit,publication_seen,reads,polls,times;
    int first_error;
};
/* The clock starts immediately before the one DATA intent, after a fresh
 * empty snapshot. Polling observes status/publication only. It copies no
 * entries and never writes, acknowledges, resumes, resets or retries DATA.
 * A late publication is retained as late, never converted to timely success.
 * A timeout permits a separately guarded final snapshot, not a DATA retry.
 * Hardware callback latency and full platform/stack binding remain external.
 */
int nano_evt_wait_init(struct nano_evt_wait *,struct nano_evt_binding,
    struct nano_evt_wait_io,struct nano_evt_clock);
int nano_evt_wait_begin(struct nano_evt_wait *,const struct nano_evt_batch *empty);
int nano_evt_wait_finish(struct nano_evt_wait *);
#endif
