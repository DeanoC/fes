/* SPDX-License-Identifier: BSD-3-Clause */
#define main service_contract_cases
#include "nano_bridges_test.c"
#undef main
#include "nano_bridge_sip.h"

int main(void)
{
    struct fes_nano_bridges b = {0};
    struct fixture f = {15, 1, 0, 0, 0, 3};
    uint64_t r[4];
    fes_nano_bridge_sip(&b, 0, 0, 0, 0, r);
    assert(r[0] == 5 && r[1] == (uint64_t)FES_EMBEDDED_BRIDGE_TAG << 32 && !r[2] && !r[3]);
    fes_nano_bridge_sip(&b, 4, 0, 0, 0, r);
    assert(r[0] == 2 && !b.initialized); /* no NS initialization operation */
    assert(fes_nano_bridges_init(&b, ops(&f), 17, 3, 2, 1) == 0);
    fes_nano_bridge_sip(&b, 0, 0, 0, 0, r);
    assert(r[0] == 0 && r[1] == 0x46424f31000000efULL && r[2] == 17 && r[3] == 1);
    uint8_t context[32];
    for (unsigned i = 0; i < 32; i++) context[i] = (uint8_t)i;
    fes_nano_boot_context_sip(&b, FES_EMBEDDED_BRIDGE_CONTEXT_PART0, 17, 1, 0, context, r);
    assert(!r[0] && r[1] == 0x4648433100000000ULL && r[2] == 0x0001020304050607ULL && r[3] == 0x08090a0b0c0d0e0fULL);
    fes_nano_boot_context_sip(&b, FES_EMBEDDED_BRIDGE_CONTEXT_PART1, 17, 1, 0, context, r);
    assert(!r[0] && r[1] == 0x4648433100000001ULL && r[2] == 0x1011121314151617ULL && r[3] == 0x18191a1b1c1d1e1fULL);
    assert(b.state.generation == 1 && b.state.never_enabled && !f.mutations);
    fes_nano_boot_context_sip(&b, FES_EMBEDDED_BRIDGE_CONTEXT_PART0, 17, 1, 0, NULL, r);
    assert(r[0] == 5 && !r[2] && !r[3]);
    fes_nano_boot_context_sip(&b, FES_EMBEDDED_BRIDGE_CONTEXT_PART0, 18, 1, 0, context, r);
    assert(r[0] == 2 && !r[2] && !r[3] && !b.failed);
    fes_nano_boot_context_sip(&b, FES_EMBEDDED_BRIDGE_CONTEXT_PART0, 17, 2, 0, context, r);
    assert(r[0] == 2 && !r[2] && !r[3] && !b.failed);
    fes_nano_boot_context_sip(&b, FES_EMBEDDED_BRIDGE_CONTEXT_PART0, 17, 1, 1, context, r);
    assert(r[0] == 1 && !r[2] && !r[3] && !b.failed);
    b.device_lease = 1;
    fes_nano_boot_context_sip(&b, FES_EMBEDDED_BRIDGE_CONTEXT_PART0, 17, 1, 0, context, r);
    assert(r[0] == 1 && !r[2] && !r[3] && !b.failed);
    b.device_lease = 0;
    fes_nano_bridge_sip(&b, 2, 17, 1, 1, r);
    assert(r[0] == 1 && !f.mutations); /* no opening during configuration */
    fes_nano_bridge_sip(&b, 0x100000001ULL, 17, 1, 0, r);
    assert(r[0] == 2 && !f.mutations);
    fes_nano_bridge_sip(&b, 0, 17, 1, 0, r);
    assert(r[0] == 2 && !f.mutations); /* snapshot is read-only, no claim args */
    fes_nano_bridge_sip(&b, 2, 17, 1, 0, r);
    assert(r[0] == 0 && r[1] == 0x46424f31000000adULL && r[3] == 2);
    fes_nano_boot_context_sip(&b, FES_EMBEDDED_BRIDGE_CONTEXT_PART0, 17, 2, 0, context, r);
    assert(r[0] == 4 && !r[2] && !r[3] && f.mutations == 1);
    fes_nano_bridge_sip(&b, 0x201, 17, 2, 0, r);
    assert(r[0] == 0 && r[1] == 0x46424f31000041afULL && r[3] == 3);
    fes_nano_bridge_sip(&b, 2, 18, 3, 0, r);
    assert(r[0] == 4 && b.failed && !r[2] && !r[3]);
    unsigned mutations = f.mutations;
    fes_nano_bridge_sip(&b, FES_EMBEDDED_BRIDGE_RELEASE | (13U << FES_EMBEDDED_BRIDGE_RELEASE_SHIFT), 17, 3, 0, r);
    assert(r[0] == 4 && f.mutations == mutations);

    b = (struct fes_nano_bridges){0};
    f = (struct fixture){15, 1, 0, 0, 0, 3}; /* simulated cold reboot */
    assert(fes_nano_bridges_init(&b, ops(&f), 17, 3, 2, 1) == 0);
    fes_nano_bridge_sip(&b, 2, 17, 1, 0, r);
    assert(!r[0] && r[3] == 2);
    const uint64_t invalid[] = {3, 0x20003, 0xe0003, 0x80007, 0x80003 | (1ULL << 32)};
    for (unsigned i = 0; i < sizeof(invalid) / sizeof(invalid[0]); i++) {
        fes_nano_bridge_sip(&b, invalid[i], 17, 2, 0, r);
        assert(r[0] == 2 && !b.failed && b.state.generation == 2 && f.mutations == 1);
    }
    fes_nano_bridge_sip(&b, 0x80003, 17, 2, 0, r);
    assert(!r[0] && r[1] == 0x46424f3100000025ULL && r[3] == 3 && f.reset == 5);
    fes_nano_bridge_sip(&b, 0x101, 17, 3, 0, r);
    assert(!r[0] && r[1] == 0x46424f31000023afULL && r[3] == 4 && f.reset == 15);
    assert(!b.failed && f.stops == 1 && f.mutations == 3);
    assert(service_contract_cases() == 0);
    puts("PASS owned bridge wire: unready, reserved, busy, held-data, explicit paths and stale-owner cases");
    return 0;
}
