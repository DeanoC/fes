/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_boot.h"
#include "nano_bridge_sip.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

struct hardware {
    uint64_t epoch;
    uint32_t bridge, per0, quiet, writes[4];
    unsigned quiesces, resets, bridges, waits, fault, quiet_reads;
};
static int owner(void *p, uint64_t id, uint64_t epoch)
{ return id == 17 && epoch == ((struct hardware *)p)->epoch; }
static int bridge_read(void *p, uint32_t *r) { *r = ((struct hardware *)p)->bridge; return 0; }
static int quiesce(void *p, uint32_t bits)
{
    struct hardware *h = p; assert(bits == FES_NANO_PRODUCERS_ALL); h->quiesces++;
    if (h->fault == 1) return -71;
    h->quiet = h->fault == 2 ? 0 : bits;
    if (h->fault == 3) h->epoch++;
    return 0;
}
static int quiet_read(void *p, uint32_t *bits)
{
    struct hardware *h = p; h->quiet_reads++;
    *bits = h->quiet;
    if (h->fault == 8) *bits |= 0x80000000U;
    if (h->fault == 7 && h->resets == 2) *bits &= ~FES_NANO_PRODUCERS_FPGA;
    return 0;
}
static int per_read(void *p, uint32_t *r)
{
    struct hardware *h = p; *r = h->per0;
    return h->fault == 6 && h->resets ? -1 : 0;
}
static int assert_reset(void *p, uint32_t bits)
{
    struct hardware *h = p;
    assert(h->quiesces && h->quiet == FES_NANO_PRODUCERS_ALL);
    assert(h->resets < 4); h->writes[h->resets++] = bits;
    assert(bits == (h->resets == 1 ? FES_NANO_MASTER_FUNCTION : FES_NANO_MASTER_RESET));
    if (h->fault == 4) return -1;
    if (!(h->fault == 5 && h->resets == 2)) h->per0 |= bits;
    return 0;
}
static void barrier(void *p) { (void)p; }
static void wait_poll(void *p) { ((struct hardware *)p)->waits++; }
static int disable(void *p, uint32_t mask, unsigned fabric)
{
    struct hardware *h = p; (void)fabric;
    assert(h->quiet == FES_NANO_PRODUCERS_ALL && h->per0 == FES_NANO_MASTER_RESET);
    h->bridges++; h->bridge |= mask;
    if (h->fault == 9) h->epoch++;
    return 0;
}
static int enable(void *p, uint32_t mask, unsigned fabric)
{ struct hardware *h = p; (void)fabric; h->bridges++; h->bridge &= ~mask; return 0; }
struct test {
    struct hardware h;
    struct fes_nano_boot boot;
    struct fes_nano_bridges bridge;
    struct fes_nano_boot_record record;
    struct fes_nano_boot_policy policy;
};
static void prepare(struct test *t, unsigned cold)
{
    memset(t, 0, sizeof(*t));
    t->h.epoch = 7; t->h.bridge = cold ? 15 : 0;
    t->h.per0 = cold ? FES_NANO_MASTER_RESET : 0;
    t->h.quiet = cold ? FES_NANO_PRODUCERS_ALL : 0;
    t->record = (struct fes_nano_boot_record){.magic=FES_NANO_BOOT_MAGIC,
        .version=1, .size=sizeof(t->record), .owner=17, .epoch=7,
        .history=cold ? FES_NANO_BOOT_NEVER_ENABLED : FES_NANO_BOOT_ACTIVE,
        .fabric=FES_NANO_FABRIC_ABSENT, .producers=FES_NANO_PRODUCERS_ALL};
    t->policy = (struct fes_nano_boot_policy){.qualifications=FES_NANO_BOOT_QUALIFIED,
        .history=t->record.history, .fabric=t->record.fabric, .producers=t->record.producers};
    t->record.chain[0] = t->policy.chain[0] = 0x42;
}
static int accept(struct test *t)
{
    struct fes_nano_master_ops m = {&t->h, owner, quiesce, quiet_read, per_read, assert_reset, barrier, wait_poll};
    struct fes_nano_bridge_ops raw = {&t->h, 0, bridge_read, 0, 0, disable, enable};
    return fes_nano_boot_accept(&t->boot, &t->bridge, &t->record, &t->policy, m, raw);
}
int main(void)
{
    unsigned cases = 0;
    for (unsigned cold = 0; cold < 2; cold++) {
        struct test t; prepare(&t, cold); assert(accept(&t) == 0);
        struct fes_nano_bridge_snapshot s;
        struct fes_nano_bridge_closure c;
        if (!cold) {
            assert(fes_nano_bridges_close(&t.bridge, 17, 1, 0, 0, &c) == 0);
            assert(t.h.quiesces == 1 && t.h.resets == 2 && t.h.bridges == 1);
            assert(c.hardware_drained == 12 && !c.fabric_acknowledged);
        }
        unsigned stops = t.h.quiesces, writes = t.h.resets;
        assert(fes_nano_bridges_configuration_guard(&t.bridge) == 0);
        assert(t.h.quiesces == stops && t.h.resets == writes);
        /* Mutating the producer's original record/policy cannot replace the
         * already consumed authority. Replay does not retire a valid owner. */
        t.record.owner++; t.policy.chain[0]++;
        assert(accept(&t) < 0 && !t.bridge.failed);
        assert(fes_nano_bridges_open(&t.bridge, 17, t.bridge.state.generation, 2, &s) == 0);
        t.h.quiet &= ~FES_NANO_PRODUCERS_FPGA; /* new reader armed, data still held */
        assert(fes_nano_bridges_observe(&t.bridge, &s) == 0 && s.masters_contained);
        assert(fes_nano_bridges_close(&t.bridge, 17, s.generation, 0, 1, &c) == 0);
        assert(c.data_remained_contained && !c.hardware_drained);
        assert(fes_nano_bridges_open(&t.bridge, 17, t.bridge.state.generation, 2, &s) == 0);
        assert(fes_nano_bridges_open(&t.bridge, 17, s.generation, 13, &s) == 0);
        assert(!s.masters_contained);
        assert(fes_nano_bridges_close(&t.bridge, 17, s.generation, 0, 0, &c) == 0);
        assert(c.hardware_drained == 12);
        fes_nano_boot_invalidate(&t.boot);
        assert(fes_nano_bridges_observe(&t.bridge, &s) < 0);
        cases++;
    }
    for (unsigned fault = 1; fault <= 9; fault++) {
        struct test t; prepare(&t, 0); assert(accept(&t) == 0); t.h.fault = fault;
        struct fes_nano_bridge_closure c;
        assert(fes_nano_bridges_close(&t.bridge, 17, 1, 0, 0, &c) < 0 && t.bridge.failed);
        unsigned writes = t.h.resets, calls = t.h.quiesces, bridge_calls = t.h.bridges;
        assert(fes_nano_bridges_close(&t.bridge, 17, 1, 0, 0, &c) < 0);
        assert(t.h.resets == writes && t.h.quiesces == calls && t.h.bridges == bridge_calls);
        if (fault <= 3 || fault == 8) assert(!writes && !bridge_calls);
        if (fault == 2 || fault == 5) assert(t.boot.first_error == -60 && t.h.waits == 100);
        if (fault == 1) assert(t.boot.first_error == -71);
        cases++;
    }
    for (unsigned mutation = 0; mutation < 14; mutation++) {
        struct test t; prepare(&t, 1);
        switch (mutation) {
        case 0: t.policy.qualifications = 0; break;
        case 1: t.policy.chain[0] ^= 1; break;
        case 2: t.record.owner = 0; break;
        case 3: t.record.epoch++; break;
        case 4: t.record.size++; break;
        case 5: t.record.version++; break;
        case 6: t.record.reserved = 1; break;
        case 7: t.record.reserved_end = 1; break;
        case 8: t.record.producers ^= 1; break;
        case 9: t.policy.history = t.record.history = 0; break;
        case 10: t.h.bridge = 13; break;
        case 11: t.h.quiet = 0; break; /* matching reset bits are insufficient */
        case 12: t.h.per0 &= ~1U; break;
        case 13: t.record.fabric = 0; break;
        }
        assert(accept(&t) < 0 && t.boot.failed && !t.h.resets && !t.h.bridges);
        unsigned writes = t.h.resets; prepare(&t, 1); /* a genuine simulated reboot */
        assert(accept(&t) == 0 && t.h.resets == writes);
        cases++;
    }
    struct test t; prepare(&t, 1); t.policy.qualifications = 0;
    assert(accept(&t) < 0); t.policy.qualifications = FES_NANO_BOOT_QUALIFIED;
    assert(accept(&t) < 0 && !t.bridge.initialized); /* failed attempt cannot retry */
    uint64_t reply[4]; fes_nano_bridge_sip(&t.bridge, 4, 0, 0, 0, reply);
    assert(reply[0] == 2 && !t.bridge.initialized); /* no NS claim/init path */
    printf("PASS %u boot/master ownership cases; injected producers and registers only\n", cases + 1);
    return 0;
}
