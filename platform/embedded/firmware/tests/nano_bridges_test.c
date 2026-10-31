/* SPDX-License-Identifier: BSD-3-Clause */
/* Hardware-independent contract tests; callbacks do not access registers. */
#include "nano_bridges.h"
#include <assert.h>
#include <stdio.h>

struct fixture { uint32_t reset; unsigned masters, mutations, stops, fault; uint64_t epoch; };
static int owns(void *p, uint64_t owner, uint64_t epoch)
{ return owner == 17 && epoch == ((struct fixture *)p)->epoch; }
static int read_reset(void *p, uint32_t *r) { *r = ((struct fixture *)p)->reset; return 0; }
static int masters(void *p) { return (int)((struct fixture *)p)->masters; }
static int stop(void *p)
{
    struct fixture *f = p; f->stops++; f->masters = 1;
    if (f->fault == 1) return -5;
    if (f->fault == 2) f->epoch++;
    return 0;
}
static int disable(void *p, uint32_t mask, unsigned fabric)
{
    struct fixture *f = p; (void)fabric; f->mutations++;
    if (f->fault == 3) return -60;
    f->reset |= mask;
    if (f->fault == 4) f->reset &= ~4U;
    return 0;
}
static int enable(void *p, uint32_t mask, unsigned fabric)
{
    struct fixture *f = p; (void)fabric; f->mutations++;
    f->reset &= ~mask; f->masters = mask == 2;
    if (f->fault == 5) return -60;
    if (f->fault == 6) f->reset = 0;
    return 0;
}
static struct fes_nano_bridge_ops ops(struct fixture *f)
{ return (struct fes_nano_bridge_ops){f, owns, read_reset, masters, stop, disable, enable}; }

int main(void)
{
    unsigned cases = 0;
    for (unsigned fabric = 1; fabric <= 2; fabric++) {
        for (unsigned cold = 0; cold <= 1; cold++) {
            struct fixture f = {cold ? 15U : 0U, cold, 0, 0, 0, 3};
            struct fes_nano_bridges b = {0};
            struct fes_nano_bridge_snapshot s;
            struct fes_nano_bridge_closure c;
            assert(fes_nano_bridges_init(&b, ops(&f), 17, 3, fabric, cold) == 0);
            assert(fes_nano_bridges_observe(&b, &s) == 0 && s.generation == 1);
            if (!cold) {
                assert(fes_nano_bridges_close(&b, 17, s.generation, fabric == 2, 0, &c) == 0);
                assert(c.hardware_drained == 12 && !c.data_remained_contained);
                assert(c.fabric_acknowledged == (fabric == 2));
            }
            unsigned before = f.mutations;
            for (unsigned i = 0; i < 3; i++) assert(fes_nano_bridges_configuration_guard(&b) == 0);
            assert(f.mutations == before); /* repeated START does not re-close */
            assert(fes_nano_bridges_open(&b, 17, b.state.generation, 2, &s) == 0);
            assert(s.reset == 13 && s.masters_contained);
            assert(fes_nano_bridges_close(&b, 17, s.generation, 0, 1, &c) == 0);
            assert(c.data_remained_contained && !c.hardware_drained && !c.fabric_acknowledged);
            assert(fes_nano_bridges_open(&b, 17, b.state.generation, 2, &s) == 0);
            assert(fes_nano_bridges_open(&b, 17, s.generation, 13, &s) == 0);
            assert(s.reset == 0 && !s.masters_contained);
            assert(fes_nano_bridges_close(&b, 17, s.generation, fabric == 2, 0, &c) == 0);
            assert(c.hardware_drained == 12 && !c.data_remained_contained);
            cases++;
        }
    }
    for (unsigned reset = 1; reset <= 15; reset++) {
        struct fixture f = {reset, 1, 0, 0, 0, 3};
        struct fes_nano_bridges b = {0};
        assert(fes_nano_bridges_init(&b, ops(&f), 17, 3, 1, 0) < 0);
        assert(b.failed && !f.mutations);
        cases++;
    }
    for (unsigned fault = 1; fault <= 6; fault++) {
        struct fixture f = {0, 0, 0, 0, 0, 3};
        struct fes_nano_bridges b = {0};
        struct fes_nano_bridge_snapshot s;
        struct fes_nano_bridge_closure c;
        assert(fes_nano_bridges_init(&b, ops(&f), 17, 3, 1, 0) == 0);
        if (fault < 5) {
            f.fault = fault;
            assert(fes_nano_bridges_close(&b, 17, 1, 0, 0, &c) < 0);
            if (fault == 2) assert(f.mutations == 0); /* lost scope before MMIO */
        } else {
            assert(fes_nano_bridges_close(&b, 17, 1, 0, 0, &c) == 0);
            f.fault = fault;
            assert(fes_nano_bridges_open(&b, 17, b.state.generation, 2, &s) < 0);
        }
        unsigned mutations = f.mutations;
        assert(b.failed && fes_nano_bridges_configuration_guard(&b) < 0);
        assert(fes_nano_bridges_close(&b, 17, b.state.generation, 0, 0, &c) < 0);
        assert(f.mutations == mutations);
        cases++;
    }
    for (unsigned fault = 0; fault < 7; fault++) {
        struct fixture f = {15, 1, 0, 0, 0, 3};
        struct fes_nano_bridges b = {0};
        struct fes_nano_bridge_closure c;
        assert(fes_nano_bridges_init(&b, ops(&f), 17, 3, 1, 1) == 0);
        switch (fault) {
        case 0: f.epoch++; break; /* ABA: same reset bits, lost epoch */
        case 1: f.reset = 7; break;
        case 2: f.masters = 0; break;
        case 3: assert(fes_nano_bridges_close(&b, 18, 1, 0, 0, &c) < 0); break;
        case 4: assert(fes_nano_bridges_close(&b, 17, 2, 0, 0, &c) < 0); break;
        case 5: b.state.generation = UINT64_MAX; assert(fes_nano_bridges_open(&b, 17, UINT64_MAX, 2, &b.state) < 0); break;
        case 6: assert(fes_nano_bridges_configuration_guard(&b) == 0); f.reset = 0; break;
        }
        assert(fes_nano_bridges_configuration_guard(&b) < 0 && !f.mutations);
        cases++;
    }
    printf("PASS %u firmware service cases; no physical MMIO\n", cases);
    return 0;
}
