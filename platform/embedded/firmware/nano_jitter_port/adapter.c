/* Cold-boot, single-CPU component. Its caller establishes the trusted MMU,
 * memory ownership and the primary CPU before entry. No IRQ or SMC caller
 * may enter it. All state in the resulting object goes in private BL31 DDR. */
#include "../nano_jitter.h"
#include "arena.h"
#include "jitterentropy.h"
#include "jitterentropy-internal.h"
#ifdef FES_JITTER_RECORD
#include "jitterentropy-noise.h"
#endif

#define JENT_FLAGS (JENT_MAX_MEMSIZE_128kB | JENT_FORCE_FIPS | JENT_DISABLE_INTERNAL_TIMER)
#define SEED_SECONDS 15U
#define RECORD_SECONDS 1200U
#define RECORD_SAMPLES 1000000U
#define SEED_CLOCK_READS 1000000U
#define RECORD_CLOCK_READS 32000000U

enum { BAD_CALL = -1, BAD_CLOCK = -2, OVER_BUDGET = -3, STARTUP = -4,
       WRONG_PROFILE = -5, HEALTH = -6, BAD_MEMORY = -7 };

/* Compiled from the unmodified upstream AArch64 timer object under a private
 * name. The timeout wrapper is shared by the recorder and operational read. */
extern void fes_jent_clock_raw(uint64_t *sample);
static struct {
    uint64_t start, last, reads, tick_limit, read_limit;
    intptr_t escape[5];
    int may_escape, error, armed;
} budget;

#ifdef FES_JITTER_RECORD
static struct fes_jitter_limit limit;

static void record_limit(uint64_t now)
{
    limit.reason = !budget.armed ? "unarmed" :
                   now < budget.last ? "counter-regression" :
                   now - budget.start >= budget.tick_limit ? "elapsed-time" : "clock-reads";
    limit.ticks = now >= budget.start ? now - budget.start : 0;
    limit.reads = budget.reads;
}

struct fes_jitter_limit fes_nano_jitter_record_limit(void)
{
    return limit;
}
#endif

static unsigned int frequency(void)
{
#ifdef FES_JITTER_HOST_FIXTURE
    return FES_JITTER_TIMER_HZ;
#else
    uint64_t hz;
    __asm__ volatile("mrs %0, cntfrq_el0" : "=r" (hz));
    return hz == FES_JITTER_TIMER_HZ ? FES_JITTER_TIMER_HZ : 0U;
#endif
}

void jent_get_nstime(uint64_t *out)
{
    fes_jent_clock_raw(out);
    ++budget.reads;
    if (!budget.armed || *out < budget.last ||
        *out - budget.start >= budget.tick_limit || budget.reads > budget.read_limit) {
#ifdef FES_JITTER_RECORD
        record_limit(*out);
#endif
        budget.error = OVER_BUDGET;
        if (budget.may_escape)
            __builtin_longjmp(budget.escape, 1);
    }
    budget.last = *out;
}

static int begin(unsigned int seconds, uint64_t reads)
{
    if (budget.armed || budget.error || frequency() != FES_JITTER_TIMER_HZ)
        return BAD_CLOCK;
    fes_jent_clock_raw(&budget.start);
    budget.last = budget.start;
    budget.reads = 0;
    budget.tick_limit = (uint64_t)seconds * FES_JITTER_TIMER_HZ;
    budget.read_limit = reads;
    budget.armed = 1;
    return 0;
}

static void inspect(const struct rand_data *ec, struct fes_nano_jitter_metrics *m)
{
    m->osr = jent_entropy_collector_osr(ec);
    m->flags = jent_entropy_collector_flags(ec);
    m->memory_bytes = (unsigned int)jent_entropy_collector_memsize(ec);
    m->hash_loops = jent_entropy_collector_hashloops(ec);
    m->internal_timer = ec->enable_notime;
    m->timer_gcd = ec->jent_common_timer_gcd;
    m->health = jent_entropy_collector_health_failure(ec);
    m->reinitializations = jent_entropy_collector_reinitializations(ec);
}

static int profile(const struct fes_nano_jitter_metrics *m)
{
    return m->osr == FES_JITTER_OSR && (m->flags & JENT_FLAGS) == JENT_FLAGS &&
           m->memory_bytes == FES_JITTER_MEMORY_BYTES && m->hash_loops == FES_JITTER_HASH_LOOPS &&
           m->timer_gcd == 1U && !m->internal_timer && !m->reinitializations;
}

static int finish(struct fes_nano_jitter_metrics *m, int rc)
{
    m->arena_peak = fes_jent_heap_high_water();
    m->time_reads = budget.reads;
    uint64_t end;
    fes_jent_clock_raw(&end);
    m->ticks = end - budget.start;
    budget.may_escape = 0;
    if (budget.error)
        rc = budget.error;
    else if (end < budget.last || m->ticks >= budget.tick_limit) {
#ifdef FES_JITTER_RECORD
        record_limit(end);
#endif
        rc = OVER_BUDGET;
    }
    if (!rc && !fes_jent_heap_empty())
        rc = BAD_MEMORY;
    fes_jent_arena_reset();
    budget.armed = 0;
    /* Stack unwinding on a time/operation limit abandons library state.
     * No new collector is allowed after any error in this boot. */
    if (rc)
        budget.error = rc;
    m->status = rc;
    return rc;
}

#ifndef FES_JITTER_RECORD
static int consumed;
static unsigned int failed_health;

/* Upstream would otherwise permit a startup collector to retry a health
 * failure with changed parameters. This profile has one qualified setting. */
static void health_stop(struct rand_data *ec, unsigned int mask)
{
    (void)ec;
    failed_health |= mask;
    budget.error = HEALTH;
    __builtin_longjmp(budget.escape, 1);
}

static void erase(void *dst, size_t bytes)
{
    volatile uint8_t *p = dst;
    while (bytes--)
        *p++ = 0U;
}

int fes_nano_jitter_seed(uint8_t *dst, size_t bytes, struct fes_nano_jitter_metrics *m)
{
    if (!dst || bytes != FES_JITTER_SEED_BYTES || !m)
        return BAD_CALL;
    erase(dst, bytes);
    *m = (struct fes_nano_jitter_metrics){0};
    if (consumed)
        return m->status = BAD_CALL;
    consumed = 1;
    int rc = begin(SEED_SECONDS, SEED_CLOCK_READS);
    if (rc)
        return m->status = rc;
    if (__builtin_setjmp(budget.escape)) {
        erase(dst, bytes);
        m->health = failed_health;
        return finish(m, budget.error);
    }
    budget.may_escape = 1;
    if (jent_set_fips_failure_callback(health_stop) || jent_entropy_init_ex(FES_JITTER_OSR, JENT_FLAGS))
        return finish(m, STARTUP);
    struct rand_data *ec = jent_entropy_collector_alloc(FES_JITTER_OSR, JENT_FLAGS);
    if (!ec)
        return finish(m, STARTUP);
    inspect(ec, m);
    rc = profile(m) ? 0 : WRONG_PROFILE;
    if (!rc && (m->health || jent_read_entropy(ec, (char *)dst, bytes) != (ssize_t)bytes))
        rc = HEALTH;
    inspect(ec, m);
    if (!rc && (!profile(m) || m->health || jent_entropy_collector_bytes_output(ec) != bytes))
        rc = HEALTH;
    jent_entropy_collector_free(ec);
    rc = finish(m, rc);
    if (rc)
        erase(dst, bytes);
    return rc;
}
#else
static struct rand_data *record;
static unsigned int samples;

/* Matches upstream jent_record_raw's common source: raw ticks without GCD
 * normalization, a priming measurement, then every sample including stuck ones. */
int fes_nano_jitter_record_open(void)
{
    if (record)
        return BAD_CALL;
    int rc = begin(RECORD_SECONDS, RECORD_CLOCK_READS);
    if (rc)
        return rc;
    if (__builtin_setjmp(budget.escape)) {
        struct fes_nano_jitter_metrics m = {0};
        record = NULL;
        return finish(&m, OVER_BUDGET);
    }
    budget.may_escape = 1;
    record = jent_entropy_collector_alloc_raw(FES_JITTER_OSR, JENT_FLAGS);
    if (!record) {
        struct fes_nano_jitter_metrics m = {0};
        return finish(&m, STARTUP);
    }
    record->jent_common_timer_gcd = 1;
    samples = 0;
    jent_measure_jitter(record, 0, NULL, 0);
    budget.may_escape = 0;
    return 0;
}

int fes_nano_jitter_record_sample(uint64_t *delta)
{
    if (!record || !delta || samples == RECORD_SAMPLES || budget.error)
        return BAD_CALL;
    uint64_t actual;
    /* A stuck sample can enter the upstream RCT-memory recovery collector.
     * A deadline/read cap must unwind even while that inner loop is active. */
    if (__builtin_setjmp(budget.escape)) {
        struct fes_nano_jitter_metrics m = {0};
        record = NULL;
        return finish(&m, OVER_BUDGET);
    }
    budget.may_escape = 1;
    (void)jent_measure_jitter(record, 0, &actual, 1);
    budget.may_escape = 0;
    *delta = actual;
    ++samples;
    return 0;
}

int fes_nano_jitter_record_close(struct fes_nano_jitter_metrics *m)
{
    if (!record || !m)
        return BAD_CALL;
    *m = (struct fes_nano_jitter_metrics){0};
    inspect(record, m);
    int rc = profile(m) && samples ? 0 : WRONG_PROFILE;
    jent_entropy_collector_free(record);
    record = NULL;
    /* A health-failed row remains reportable for rejection by the assessor.
     * Its raw observations are never returned as conditioned output. */
    return finish(m, rc);
}
#endif
