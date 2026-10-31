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
    fes_nano_bridge_sip(&b, 2, 17, 1, 1, r);
    assert(r[0] == 1 && !f.mutations); /* no opening during configuration */
    fes_nano_bridge_sip(&b, 0x100000001ULL, 17, 1, 0, r);
    assert(r[0] == 2 && !f.mutations);
    fes_nano_bridge_sip(&b, 0, 17, 1, 0, r);
    assert(r[0] == 2 && !f.mutations); /* snapshot is read-only, no claim args */
    fes_nano_bridge_sip(&b, 2, 17, 1, 0, r);
    assert(r[0] == 0 && r[1] == 0x46424f31000000adULL && r[3] == 2);
    fes_nano_bridge_sip(&b, 0x201, 17, 2, 0, r);
    assert(r[0] == 0 && r[1] == 0x46424f31000041afULL && r[3] == 3);
    fes_nano_bridge_sip(&b, 2, 18, 3, 0, r);
    assert(r[0] == 4 && b.failed && !r[2] && !r[3]);
    unsigned mutations = f.mutations;
    fes_nano_bridge_sip(&b, 3, 17, 3, 0, r);
    assert(r[0] == 4 && f.mutations == mutations);
    assert(service_contract_cases() == 0);
    puts("PASS owned bridge wire: unready, reserved, busy, held-data and stale-owner cases");
    return 0;
}
