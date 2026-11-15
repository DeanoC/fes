// SPDX-License-Identifier: GPL-3.0-or-later
#include "probe.hpp"

#ifdef __ZEPHYR__
#include <zephyr/kernel.h>
#include <zephyr/sys/printk.h>
#define PROBE_PRINT printk
static std::uint64_t clock_us()
{
    return k_cyc_to_us_floor64(k_cycle_get_64());
}
// A bounded hint lets Link detect timer stalls; do not busy-wait on the timer.
static void idle() { asm volatile("yield"); }
#else
#include <cstdio>
#define PROBE_PRINT std::printf
#endif

static void report(const char* name, bool ok)
{
    PROBE_PRINT("%s %s\n", ok ? "PASS" : "FAIL", name);
}

int main()
{
    PROBE_PRINT("FES exploratory GP probe; transport=emulated; no board MMIO\n");
    bool passed = false;
#ifdef __ZEPHYR__
    const unsigned count = fes_probe::selftest(report, &passed, clock_us, idle);
#else
    const unsigned count = fes_probe::selftest(report, &passed);
#endif
    PROBE_PRINT("FES_EMBEDDED_PROBE %s tests=%u transport=emulated\n",
                passed ? "PASS" : "FAIL", count);
    return passed ? 0 : 1;
}
