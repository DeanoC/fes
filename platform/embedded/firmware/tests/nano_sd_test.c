/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_sd.h"
#include "nano_bridge_sip.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

struct hardware {
    unsigned active, fault, scopes, owns, resumes, prepares, stops;
    uint32_t reset, clock, mode, busy, signal, status_enable;
    uint64_t epoch;
};
static int owns(void *p, uint64_t owner, uint64_t epoch)
{
    struct hardware *h = p;
    /* The old cold provider must never be consulted during an SD lease. */
    assert(!h->active);
    h->owns++;
    return owner == 17 && epoch == h->epoch && !(h->fault == 8 && h->stops);
}
static int reset_read(void *p, uint32_t *reset)
{ struct hardware *h=p; assert(!h->active); *reset=h->reset; return 0; }
static int contained(void *p) { assert(!((struct hardware *)p)->active); return 1; }
static int no_stop(void *p) { (void)p; assert(0); return -1; }
static int no_bridge(void *p, uint32_t mask, unsigned fabric)
{ (void)p; (void)mask; (void)fabric; assert(0); return -1; }
static int scope(void *p, uint64_t owner, uint64_t epoch, unsigned active)
{
    struct hardware *h=p; h->scopes++;
    if (owner != 17 || epoch != h->epoch || h->reset != 15 || h->active != active ||
        h->fault == 1 || (h->fault == 3 && active) ||
        (h->fault == 5 && h->prepares) || (h->fault == 7 && h->stops)) return -70;
    return 0;
}
static int resume(void *p)
{ struct hardware *h=p; assert(!h->active); h->resumes++; h->active=1; return h->fault == 2 ? -72 : 0; }
static int prepare_clock(void *p)
{
    struct hardware *h=p; assert(h->active); h->prepares++;
    if ((h->clock & (7U | 7U<<24)) != 3 || h->mode || h->signal) return -79;
    return h->fault == 4 ? -74 : 0;
}
static int stop(void *p)
{
    struct hardware *h=p; assert(h->active); h->stops++;
    if ((h->clock & (7U | 7U<<24)) != 3 || h->mode || h->busy || h->signal || h->status_enable)
        return -80;
    if (h->fault == 6) return -76;
    h->active=0;
    return 0;
}
struct test {
    struct hardware h;
    struct fes_nano_bridges bridge;
    struct fes_nano_sd sd;
};
static void setup(struct test *t)
{
    memset(t, 0, sizeof(*t)); t->h.reset=15; t->h.epoch=7; t->h.clock=3;
    struct fes_nano_bridge_ops raw={&t->h, owns, reset_read, contained, no_stop, no_bridge, no_bridge};
    assert(!fes_nano_bridges_init(&t->bridge, raw, 17, 7, FES_NANO_FABRIC_ABSENT, 1));
}
static int initialize(struct test *t, unsigned qualified)
{
    struct fes_nano_sd_ops ops={&t->h, scope, resume, prepare_clock, stop, 0};
    return fes_nano_sd_init(&t->sd, &t->bridge, ops, qualified);
}
static void call(struct test *t, unsigned op, uint64_t result[4])
{
    fes_nano_sd_sip(&t->sd, op, op ? 17 : 0,
                    op ? t->bridge.state.generation : 0, 0, result);
}
static void reply(const uint64_t r[4], unsigned status, unsigned flags, uint64_t generation)
{
    assert(r[0] == status && r[1] == ((uint64_t)FES_EMBEDDED_SD_TAG<<32 | flags));
    assert(r[2] == (status ? 0 : 17) && r[3] == generation);
}
int main(void)
{
    struct test t; uint64_t r[4]; unsigned cases=0;
    setup(&t); call(&t, 0, r); reply(r, 5, 0, 0);
    assert(!t.sd.attempted && !initialize(&t, FES_NANO_SD_QUALIFIED));
    call(&t, 0, r); reply(r, 0, 0x39, 1);
    assert(initialize(&t, FES_NANO_SD_QUALIFIED) < 0 && !t.bridge.failed);
    for (unsigned n=0; n<2; n++) {
        uint64_t generation=t.bridge.state.generation;
        call(&t, 1, r); reply(r, 0, 0x33, generation+1);
        unsigned reads=t.h.owns, samples=t.h.scopes;
        struct fes_nano_bridge_snapshot s;
        assert(fes_nano_bridges_observe(&t.bridge, &s) < 0);
        assert(fes_nano_bridges_configuration_guard(&t.bridge) < 0);
        assert(fes_nano_bridges_open(&t.bridge, 17, generation+1, 2, &s) < 0);
        assert(!t.bridge.failed && t.h.owns == reads);
        call(&t, 0, r); reply(r, 0, 0x33, generation+1);
        call(&t, 1, r); reply(r, 2, 0, 0);
        assert(t.h.scopes == samples+1); /* repeated Resume did not observe/write */
        call(&t, 2, r); reply(r, 0, 0x37, generation+2);
        call(&t, 2, r); reply(r, 2, 0, 0);
        call(&t, 3, r); reply(r, 0, 0x39, generation+3);
        assert(!t.bridge.device_lease && !fes_nano_bridges_observe(&t.bridge, &s));
        assert(s.generation == generation+3 && s.never_enabled && s.masters_contained);
        call(&t, 3, r); reply(r, 2, 0, 0);
        cases++;
    }
    for (unsigned fault=1; fault<=8; fault++) {
        setup(&t); assert(!initialize(&t, FES_NANO_SD_QUALIFIED));
        if (fault >= 4) { call(&t, 1, r); reply(r, 0, 0x33, 2); }
        if (fault >= 6) { call(&t, 2, r); reply(r, 0, 0x37, 3); }
        t.h.fault=fault;
        call(&t, fault <= 3 ? 1 : fault <= 5 ? 2 : 3, r);
        reply(r, 4, 0, 0); assert(t.sd.failed && t.bridge.failed && t.sd.first_error);
        if (fault != 1) assert(t.sd.active && t.bridge.device_lease);
        struct hardware before=t.h; int first=t.sd.first_error;
        for (unsigned op=0; op<4; op++) { call(&t, op, r); reply(r, 4, 0, 0); }
        assert(!memcmp(&before, &t.h, sizeof(before)) && t.sd.first_error == first);
        cases++;
    }
    setup(&t); assert(!initialize(&t, FES_NANO_SD_QUALIFIED));
    struct hardware before=t.h;
    for (unsigned op=0; op<7; op++) {
        fes_nano_sd_sip(&t.sd, op, 99, 99, 0, r); reply(r, 2, 0, 0);
        assert(!t.sd.failed && !memcmp(&before, &t.h, sizeof(before)));
    }
    fes_nano_sd_sip(&t.sd, 1, 17, 1, 1, r); reply(r, 1, 0, 0);
    assert(!memcmp(&before, &t.h, sizeof(before)));
    for (unsigned active=0; active<2; active++) {
        setup(&t); assert(!initialize(&t, FES_NANO_SD_QUALIFIED));
        if (active) {call(&t, 1, r); reply(r, 0, 0x33, 2);}
        t.h.epoch++; /* matching bridge resets do not preserve boot ownership */
        call(&t, 0, r); reply(r, 4, 0, 0);
        assert(t.sd.failed && t.bridge.failed);
        before=t.h; call(&t, 0, r); reply(r, 4, 0, 0);
        assert(!memcmp(&before, &t.h, sizeof(before)));
        cases++;
    }
    setup(&t); assert(initialize(&t, FES_NANO_SD_QUALIFIED ^ 1) < 0);
    assert(initialize(&t, FES_NANO_SD_QUALIFIED) < 0 && !t.sd.initialized);
    call(&t, 0, r); reply(r, 4, 0, 0);
    setup(&t); assert(!initialize(&t, FES_NANO_SD_QUALIFIED));
    t.bridge.state.generation=UINT64_MAX;
    before=t.h; call(&t, 1, r); reply(r, 4, 0, 0);
    assert(!memcmp(&before, &t.h, sizeof(before)) && t.bridge.failed);
    printf("PASS SD firmware lease: %u lifecycle/failure cases; injected hardware only\n", cases+3);
    return 0;
}
