// SPDX-License-Identifier: GPL-2.0-or-later
// Harness for unchanged VICE 3.10 CIA/timer/alarm sources. See cia_timing.md
// and cia_timing_vectors.py for source provenance and reproducible generation.
#define _GNU_SOURCE
#include <stdlib.h>
#include <stdarg.h>
#include <assert.h>
#include "ciacore.c"
#include "cia_timing_program.h"

void *lib_malloc(size_t n) { return malloc(n); }
void *lib_calloc(size_t n, size_t m) { return calloc(n, m); }
void lib_free(void *p) { free(p); }
char *lib_strdup(const char *s) { return strdup(s); }
char *lib_msprintf(const char *s, ...) {
    va_list args;
    va_start(args, s);
    char *result = NULL;
    vasprintf(&result, s, args);
    va_end(args);
    return result;
}
unsigned int lib_unsigned_rand(unsigned int min, unsigned int max) { return min; }
log_t log_open(const char *id) { return 0; }
int log_message(log_t log, const char *s, ...) { return 0; }
int log_error(log_t log, const char *s, ...) {
    va_list args;
    va_start(args, s);
    vfprintf(stderr, s, args);
    va_end(args);
    return 0;
}
unsigned int interrupt_cpu_status_int_new(interrupt_cpu_status_t *cpu,
                                         const char *name) { return 0; }

static CLOCK clock_now, pending_at;
static int pending, pending_value, physical_pin, rmw;
static unsigned case_id;

static void apply_pin(int value, CLOCK at) {
    if (value != physical_pin) {
        printf("E,%u,%lu,%d\n", case_id, at, value);
        physical_pin = value;
    }
}

static void apply_pending(void) {
    if (pending && pending_at <= clock_now) {
        pending = 0;
        apply_pin(pending_value, pending_at);
    }
}

static void set_irq(cia_context_t *cia, int value, CLOCK at) {
    // VICE's shortcut updates irq_enabled when it schedules a future callback.
    // Queue the physical pin until its timestamp. Preserve both transitions
    // when a Phi2 assertion is followed by an ICR-read clear in the same cycle.
    if (at > clock_now) {
        pending = 1;
        pending_at = at;
        pending_value = value;
    } else {
        apply_pending();
        pending = 0;
        apply_pin(value, at);
    }
}

static void null_cia(cia_context_t *cia) {}
static void null_port(cia_context_t *cia, CLOCK at, uint8_t value) {}
static void null_sdr(cia_context_t *cia, uint8_t value) {}
static uint8_t read_port(cia_context_t *cia) { return 255; }

static void run_case(unsigned id, const CIATimingCase *program) {
    case_id = id;
    clock_now = pending_at = 0;
    pending = pending_value = physical_pin = rmw = 0;
    cia_context_t context = {0};
    cia_context_t *cia = &context;
    cia->myname = "CIA_TIMING";
    cia->clk_ptr = &clock_now;
    cia->rmw_flag = &rmw;
    cia->todticks = cia->power_ticks = 100000000;
    cia->ticks_per_sec = 1000000000;
    cia->power_freq = 50;
    cia->cia_set_int_clk = set_irq;
    cia->do_reset_cia = null_cia;
    cia->store_ciapa = cia->store_ciapb = null_port;
    cia->store_sdr = null_sdr;
    cia->read_ciapa = cia->read_ciapb = read_port;
    cia->read_ciaicr = cia->read_sdr = null_cia;
    ciacore_setup_context(cia);
    alarm_context_t *alarms = alarm_context_new("CIA_TIMING");
    ciacore_init(cia, alarms, NULL);
    ciacore_reset(cia);
    // Old 6526, bus timestamps supplied directly. The CPU API usually advances
    // its clock before store, hence the source's default write_offset of one.
    assert(cia->model == CIA_MODEL_6526);
    cia->write_offset = 0;
    const CIATimingEvent *events = cia_timing_events + program->first_event;
    unsigned next = 0;
    for (clock_now = 1; clock_now <= program->duration; ++clock_now) {
        int active = next < program->event_count && events[next].cycle == clock_now;
        int reset_now = active && events[next].op == 'X';
        // Reset at Phi2 takes priority over a queued same-cycle assertion.
        if (reset_now) pending = 0;
        else apply_pending();
        if (active) {
            CIATimingEvent event = events[next++];
            if (event.op == 'W') ciacore_store(cia, event.reg, event.value);
            else if (event.op == 'X') ciacore_reset(cia);
            else if (event.op == 'T') ciacore_inttod(0, cia);
            else {
                unsigned data = ciacore_read(cia, event.reg);
                printf("R,%u,%lu,%u,%u\n", id, clock_now, event.reg, data);
            }
        }
        while (clock_now >= alarm_context_next_pending_clk(alarms))
            alarm_context_dispatch(alarms, clock_now);
        cia_update_ta(cia, clock_now);
        cia_update_tb(cia, clock_now);
        cia_ifr_catchup(cia, clock_now);
        if (cia->ifr_clock == clock_now)
            cia_ifr_current(cia, clock_now, CIA_IFR_CUR_NXT);
        apply_pending();
        printf("S,%u,%lu,%u,%u,%u,%u,%u,%u,%u,%u,%u\n", id, clock_now,
               cia->ta->cnt, cia->ta->latch, cia->tb->cnt, cia->tb->latch,
               (cia->c_cia[14] & 254) | ciat_is_running(cia->ta, clock_now),
               (cia->c_cia[15] & 254) | ciat_is_running(cia->tb, clock_now),
               cia->irqflags & 255, cia->c_cia[13], physical_pin);
    }
    assert(next == program->event_count);
    alarm_context_destroy(alarms);
    free(cia->ta);
    free(cia->tb);
}

int main(void) {
    _Static_assert(sizeof(unsigned long) == 8 && sizeof(void *) == 8,
                   "oracle config assumes a 64-bit Linux host");
    for (unsigned id = 0; id < CIA_TIMING_CASE_COUNT; ++id)
        run_case(id, &cia_timing_cases[id]);
    return 0;
}
