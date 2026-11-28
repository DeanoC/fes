/* Host-only fault injection. Each named case runs in a fresh process with
 * the real collector, adapter and arena. The controlled clock supplies test
 * observations only and is never compiled into the firmware. */
#include "../nano_jitter.h"
#include "../nano_jitter_port/arena.h"
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

enum { BAD_CALL = -1, BAD_CLOCK = -2, OVER_BUDGET = -3, WRONG_PROFILE = -5, HEALTH = -6 };

#ifdef CONTROLLED_CLOCK
enum test_clock { VARIABLE, MULTIPLE, CONSTANT, HELD, DEADLINE, BACKWARD };
static enum test_clock style;
static uint32_t sequence = 1;
static uint64_t tick = 1000000, calls;
static uint64_t release_after = UINT64_MAX;
static uint64_t switch_at = UINT64_MAX;
static enum test_clock next_style;
static unsigned int seconds;

void fes_jent_clock_raw(uint64_t *value)
{
    calls++;
    if (calls == switch_at)
        style = next_style;
    if (style == DEADLINE || (style == HELD && calls >= release_after)) {
        tick += (uint64_t)seconds * FES_JITTER_TIMER_HZ;
        style = HELD;
        release_after = UINT64_MAX;
    } else if (style == BACKWARD) {
        tick--;
        style = HELD;
    } else if (style == CONSTANT) {
        tick += 7;
    } else if (style != HELD) {
        sequence ^= sequence << 13;
        sequence ^= sequence >> 17;
        sequence ^= sequence << 5;
        unsigned int stride = 80U + (sequence & 0xffU);
        tick += style == MULTIPLE ? 6U * stride : stride;
    }
    *value = tick;
}
#endif

#ifdef READ_CAP
struct rand_data;
extern void jent_get_nstime(uint64_t *value);

unsigned int jent_measure_jitter(struct rand_data *collector, uint64_t loops,
                                 uint64_t *delta, int health)
{
    uint64_t value;
    (void)collector;
    (void)loops;
    (void)delta;
    if (!health) {
        jent_get_nstime(&value);
        return 0;
    }
    /* Each read must be counted even when its value never advances. The
     * watchdog must unwind this in-flight call before it can return. */
    for (unsigned int i = 0; i < 32000001U; i++)
        jent_get_nstime(&value);
    assert(!"capture exceeded its maximum raw timer reads");
    return 0;
}
#endif

#ifdef SEED
static void cleared(const unsigned char *bytes, size_t size)
{
    for (size_t i = 0; i < size; i++)
        assert(bytes[i] == 0);
}

static void run(const char *name)
{
    unsigned char data[FES_JITTER_SEED_BYTES];
    struct fes_nano_jitter_metrics m = {0};
    memset(data, 0x9b, sizeof(data));
    assert(fes_nano_jitter_seed(NULL,sizeof(data),&m) == BAD_CALL);
    assert(fes_nano_jitter_seed(data,sizeof(data),NULL) == BAD_CALL);
    assert(fes_nano_jitter_seed(data,sizeof(data)-1,&m) == BAD_CALL);
    assert(data[0] == 0x9b && data[sizeof(data)-1] == 0x9b);
#ifdef CONTROLLED_CLOCK
    seconds = 15;
    if (strcmp(name,"gcd") == 0) {
        style = MULTIPLE;
    } else if (strcmp(name,"health") == 0 || strcmp(name,"deadline") == 0 || strcmp(name,"backward") == 0) {
        switch_at = 800;
        next_style = strcmp(name,"health") == 0 ? CONSTANT :
                     (strcmp(name,"deadline") == 0 ? DEADLINE : BACKWARD);
    }
#endif
    int rc = fes_nano_jitter_seed(data,sizeof(data),&m);
    printf("%s rc=%d gcd=%llu health=%u reads=%llu ticks=%llu peak=%zu\n",
           name,rc,(unsigned long long)m.timer_gcd,m.health,(unsigned long long)m.time_reads,
           (unsigned long long)m.ticks,m.arena_peak);
    assert(rc == m.status && fes_jent_heap_empty());
#ifdef CONTROLLED_CLOCK
    if (strcmp(name,"gcd") == 0) {
        assert(rc == WRONG_PROFILE && m.timer_gcd == 6 && !m.health);
    } else if (strcmp(name,"health") == 0) {
        assert(rc == HEALTH && m.health);
    } else if (strcmp(name,"deadline") == 0 || strcmp(name,"backward") == 0) {
        assert(rc == OVER_BUDGET && m.time_reads == switch_at - 1 && m.arena_peak);
    } else {
        assert(strcmp(name,"seed") == 0 && rc == 0 && m.timer_gcd == 1 && !m.health);
    }
#else
    /* A host timer is an observation: divided clocks must fail the A55
     * profile. A real unit-GCD machine can satisfy it. A software CI host
     * can also legitimately fail the startup, health or deadline gates. */
    if (m.timer_gcd > 1)
        assert(rc == WRONG_PROFILE);
    if (!rc)
        assert(m.timer_gcd == 1 && !m.health);
#endif
    if (rc) {
        cleared(data,sizeof(data));
    } else {
        unsigned int any = 0;
        for (size_t i=0; i<sizeof(data); i++)
            any |= data[i];
        assert(any && m.osr == 3 && m.memory_bytes == 131072 && m.hash_loops == 1);
        assert(m.arena_peak > 131072 && m.arena_peak <= FES_JENT_ARENA_BYTES);
    }
    memset(data,0x7e,sizeof(data));
    assert(fes_nano_jitter_seed(data,sizeof(data),&m) == BAD_CALL);
    cleared(data,sizeof(data));
}
#else
static void run(const char *name)
{
    struct fes_nano_jitter_metrics m = {0};
    uint64_t delta = UINT64_MAX;
    seconds = 3U * 60U * 60U;
    assert(fes_nano_jitter_record_sample(&delta) == BAD_CALL && delta == UINT64_MAX);
    assert(fes_nano_jitter_record_close(&m) == BAD_CALL);
    assert(fes_nano_jitter_record_open() == 0);
    assert(fes_nano_jitter_record_open() == BAD_CALL);
    assert(fes_nano_jitter_record_sample(NULL) == BAD_CALL);
    if (strcmp(name,"raw") == 0) {
        /* A progressing raw collector can cross the previous 20-minute deadline. */
        tick += UINT64_C(1201) * FES_JITTER_TIMER_HZ;
        style = HELD;
        assert(fes_nano_jitter_record_sample(&delta) == 0);
        delta = UINT64_MAX;
        assert(fes_nano_jitter_record_sample(&delta) == 0 && delta == 0);
        style = VARIABLE;
        assert(fes_nano_jitter_record_sample(&delta) == 0 && delta != 0);
        assert(fes_nano_jitter_record_close(&m) == 0);
        assert(m.timer_gcd == 1 && m.arena_peak > 131072 && fes_jent_heap_empty());
        assert(fes_nano_jitter_record_open() == 0);
        assert(fes_nano_jitter_record_sample(&delta) == 0);
        assert(fes_nano_jitter_record_close(&m) == 0);
    } else if (strcmp(name, "close-backward") == 0 || strcmp(name, "close-deadline") == 0) {
        assert(fes_nano_jitter_record_sample(&delta) == 0);
        style = strcmp(name, "close-backward") == 0 ? BACKWARD : DEADLINE;
        assert(fes_nano_jitter_record_close(&m) == OVER_BUDGET);
        assert(m.status == OVER_BUDGET && fes_jent_heap_empty());
        assert(fes_nano_jitter_record_open() == BAD_CLOCK);
    } else {
        if (strcmp(name,"backward") == 0)
            style = BACKWARD;
        else if (strcmp(name,"deadline") == 0)
            style = DEADLINE;
        else if (strcmp(name, "read-cap") == 0)
            style = HELD;
        else {
            assert(strcmp(name,"held-deadline") == 0);
            style = HELD;
            /* Deliver zero deltas long enough to activate upstream's stuck
             * handling. Expire during the subsequent upstream measurement. */
            release_after = calls + 5000;
        }
        int rc;
        do {
            delta = UINT64_MAX;
            rc = fes_nano_jitter_record_sample(&delta);
        } while (!rc);
        if (strcmp(name, "read-cap") == 0)
            assert(calls == 32000003U); /* start + 32,000,001 reads + finish */
        struct fes_jitter_limit limit = fes_nano_jitter_record_limit();
        const char *expected = !strcmp(name, "backward") ? "counter-regression" :
                               !strcmp(name, "read-cap") ? "clock-reads" : "elapsed-time";
        assert(limit.reason && !strcmp(limit.reason, expected) && limit.reads);
        if (!strcmp(expected, "elapsed-time"))
            assert(limit.ticks >= (uint64_t)seconds * FES_JITTER_TIMER_HZ);
        if (!strcmp(expected, "clock-reads"))
            assert(limit.reads == 32000001U);
        assert(rc == OVER_BUDGET && delta == UINT64_MAX && fes_jent_heap_empty());
        assert(fes_nano_jitter_record_close(&m) == BAD_CALL);
        assert(fes_nano_jitter_record_open() == BAD_CLOCK);
    }
    printf("%s passed\n",name);
}
#endif

int main(int argc, char **argv)
{
    assert(argc == 2);
    setvbuf(stdout, NULL, _IONBF, 0);
    run(argv[1]);
    return 0;
}
