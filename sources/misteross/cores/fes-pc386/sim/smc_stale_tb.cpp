// SPDX-License-Identifier: MIT
#include "smc_stale.h"

#include <cstdio>

namespace {

bool check(bool condition, const char *message) {
    if (!condition) std::fprintf(stderr, "FAIL %s\n", message);
    return condition;
}

} // namespace

int main() {
    bool ok = true;
    SmcStaleTracker stale;
    uint32_t address = 0;

    // REP stores into its own encoding while EIP still points at the REP.
    ok &= check(stale.store(0, 0, 0, 256), "record first REP byte");
    ok &= check(stale.store(1, 0, 0, 256), "record second REP byte");
    ok &= check(!stale.executed(0, 2, address), "do not flag the storing REP instruction");

    // The same REP can also overwrite the already fetched instruction after
    // it; that later instruction must still be checked against the writer.
    ok &= check(stale.store(2, 0, 0, 256), "record next instruction byte");
    ok &= check(stale.executed(2, 4, address) && address == 2, "flag later instruction using REP-written bytes");

    stale.clear();
    // A front end more than 96 bytes ahead is still within the observed fetch
    // range and must not be silently dropped by the boot gate.
    ok &= check(stale.store(200, 0, 0, 256), "record byte beyond 96-byte window");
    ok &= check(stale.executed(200, 202, address) && address == 200, "detect byte beyond 96-byte window");

    stale.clear();
    // DMA can land while the next instruction is open for issue.
    constexpr uint32_t DMA_OWNER = 0xffffffffu;
    ok &= check(stale.store(10, DMA_OWNER, 10, 256), "record DMA write");
    ok &= check(stale.executed(10, 11, address) && address == 10, "detect DMA-written instruction byte");
    stale.clear();
    ok &= check(stale.store(11, 10, 10, 256), "record DMA write during current instruction");
    ok &= check(!stale.executed(10, 11, address), "allow current instruction to finish after DMA write");
    ok &= check(stale.executed(11, 12, address) && address == 11, "check later instruction after in-flight DMA write");

    return ok ? 0 : 1;
}
