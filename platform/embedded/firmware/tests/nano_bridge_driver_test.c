/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_bridge_fixture.h"
#include <assert.h>
#include <stdio.h>

int main(void)
{
    unsigned cases = 0;
    struct fes_nano_bridge_snapshot s;
    struct fes_nano_bridge_closure c;
    uint64_t stats[6];
    for (unsigned fabric = 1; fabric <= 2; fabric++) {
        for (unsigned cold = 0; cold <= 1; cold++) {
            assert(fes_nano_fixture_boot(cold ? 15 : 0, cold, fabric) == 0);
            if (!cold) {
                assert(fes_nano_bridges_close(&fes_nano_fixture_service, 17, 1, fabric == 2, 0, &c) == 0);
                assert(c.hardware_drained == 12);
                fes_nano_fixture_stats(stats);
                assert(stats[0] == 1 && stats[2] == (fabric == 2 ? 6U : 4U));
                assert(stats[3] == (fabric == 2 ? 2U : 0U) && stats[4] == 4);
            }
            fes_nano_fixture_stats(stats);
            uint64_t writes = stats[5];
            for (unsigned i = 0; i < 3; i++) assert(fes_nano_bridges_configuration_guard(&fes_nano_fixture_service) == 0);
            fes_nano_fixture_stats(stats); assert(stats[5] == writes);
            assert(fes_nano_bridges_observe(&fes_nano_fixture_service, &s) == 0);
            assert(fes_nano_bridges_open(&fes_nano_fixture_service, 17, s.generation, 2, &s) == 0);
            assert(s.reset == 13);
            assert(fes_nano_bridges_close(&fes_nano_fixture_service, 17, s.generation, 0, 1, &c) == 0);
            assert(c.data_remained_contained && !c.hardware_drained && !c.fabric_acknowledged);
            const uint32_t paths[] = {1, 4, 5, 8, 9, 12, 13};
            for (unsigned i = 0; i < sizeof(paths) / sizeof(paths[0]); i++) {
                assert(fes_nano_bridges_open(&fes_nano_fixture_service, 17, c.after.generation, 2, &s) == 0);
                assert(fes_nano_bridges_open(&fes_nano_fixture_service, 17, s.generation, paths[i], &s) == 0);
                assert(s.reset == (13U & ~paths[i]));
                assert(fes_nano_bridges_close(&fes_nano_fixture_service, 17, s.generation, fabric == 2, 0, &c) == 0);
                assert(c.hardware_drained == (paths[i] & 12U) && c.after.reset == 15);
                fes_nano_fixture_stats(stats);
                if (fabric == 1) assert(stats[3] == 0); /* optional clear polls also omitted */
                cases++;
            }
        }
        for (unsigned fault = 1; fault <= 7; fault++) {
            assert(fes_nano_fixture_boot(0, 0, fabric) == 0);
            fes_nano_fixture_fault(fault);
            int result = fes_nano_bridges_close(&fes_nano_fixture_service, 17, 1, fabric == 2, 0, &c);
            if (fault == 2 && fabric == 1) {
                assert(result == 0); /* absent fabric is not a missing required ACK */
            } else {
                assert(result < 0 && fes_nano_fixture_service.failed);
                assert(fes_nano_bridges_configuration_guard(&fes_nano_fixture_service) < 0);
            }
            cases++;
        }
    }
    printf("PASS %u actual-vendor-driver/service register cases; no physical drain proof\n", cases);
    return 0;
}
