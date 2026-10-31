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
    auto cycle = stale.begin_cycle(true, 0);
    ok &= check(cycle.store(0, 0, 0), "record first REP byte");
    ok &= check(cycle.store(1, 0, 0), "record second REP byte");
    ok &= check(!stale.executed(0, 2, address), "do not flag the storing REP instruction");

    // The same REP can also overwrite the already fetched instruction after
    // it; that later instruction must still be checked against the writer.
    ok &= check(cycle.store(2, 0, 0), "record next instruction byte");
    ok &= check(stale.executed(2, 4, address) && address == 2, "flag later instruction using REP-written bytes");

    stale.reset();
    // A20 folds the bus address, but fetches and retirement are keyed by the
    // linear address. Keep the store shadow in that same address space.
    constexpr uint32_t a20_current = 0x100000;
    constexpr uint32_t a20_store_linear = 0x100008;
    constexpr uint32_t a20_store_physical = a20_store_linear & ~(1u << 20);
    cycle = stale.begin_cycle(true, a20_current);
    ok &= check(!cycle.store(a20_store_physical, a20_current, a20_current),
                "physical A20 alias is outside the linear fetch window");
    ok &= check(cycle.store(a20_store_linear, a20_current, a20_current),
                "record A20 store in the linear fetch window");
    ok &= check(stale.executed(a20_store_linear, a20_store_linear + 1, address) && address == a20_store_linear,
                "detect stale fetch through an A20 alias");

    stale.reset();
    // A front end more than 96 bytes ahead is still within the observed fetch
    // range and must not be silently dropped by the boot gate.
    for (uint32_t block = 0; block < 256; block += 16) stale.fetch(block);
    ok &= check(stale.store(200, 0, 0), "record byte beyond 96-byte window");
    ok &= check(stale.executed(200, 202, address) && address == 200, "detect byte beyond 96-byte window");

    stale.reset();
    // DMA can land while the next instruction is open for issue.
    cycle = stale.begin_cycle(true, 0);
    ok &= check(cycle.store(10, SmcStaleTracker::DMA_OWNER, 10), "record DMA write");
    ok &= check(stale.executed(10, 11, address) && address == 10, "detect DMA-written instruction byte");
    stale.reset();
    cycle = stale.begin_cycle(true, 0);
    ok &= check(cycle.store(11, 10, 10), "record DMA write during current instruction");
    ok &= check(!stale.executed(10, 11, address), "allow current instruction to finish after DMA write");
    ok &= check(stale.executed(11, 12, address) && address == 11, "check later instruction after in-flight DMA write");

    // The fetch block is loaded on the same edge as this store. The new
    // block must be in the window before the store is attributed.
    stale.reset();
    cycle = stale.begin_cycle(true, 0x1000);
    ok &= check(cycle.store(0x1008, 0x0ff0, 0x1000), "record same-cycle fetch and store");
    ok &= check(stale.executed(0x1008, 0x1009, address) && address == 0x1008,
                "detect stale bytes from same-cycle fetch and store");

    stale.reset();
    cycle = stale.begin_cycle(true, 0x1100);
    ok &= check(cycle.store(0x1108, SmcStaleTracker::DMA_OWNER, 0x1100), "record DMA-done-cycle fetch and write");
    ok &= check(stale.executed(0x1108, 0x1109, address) && address == 0x1108,
                "detect stale bytes from same-cycle fetch and DMA write");

    // A later store by the instruction which executes the byte must not
    // replace the earlier writer and hide the stale fetch.
    stale.reset();
    cycle = stale.begin_cycle(true, 0x2000);
    ok &= check(cycle.store(0x2005, 0x2000, 0x2000), "record earlier instruction writer");
    ok &= check(cycle.store(0x2005, 0x2004, 0x2004), "record retiring instruction writer");
    ok &= check(stale.executed(0x2004, 0x2006, address) && address == 0x2005,
                "retain earlier writer when retiring instruction stores same byte");

    return ok ? 0 : 1;
}
