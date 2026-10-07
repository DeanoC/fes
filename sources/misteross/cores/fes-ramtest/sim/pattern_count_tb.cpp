// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vpattern_count_bench.h"
#include "verilated.h"
#include <cstdint>
#include <iostream>

static void require(bool ok, const char* message) {
    if (!ok) {
        std::cerr << message << '\n';
        std::exit(1);
    }
}

template <typename W>
static void copy6(const W& src, uint32_t dst[6]) {
    for (int i = 0; i < 6; ++i)
        dst[i] = src[i];
}

static bool same6(const uint32_t a[6], const uint32_t b[6]) {
    for (int i = 0; i < 6; ++i)
        if (a[i] != b[i])
            return false;
    return true;
}

static uint32_t sum6(const uint32_t a[6]) {
    uint32_t total = 0;
    for (int i = 0; i < 6; ++i)
        total += a[i];
    return total;
}

static void slot_of(unsigned rate, const uint32_t pat50[6], const uint32_t pat75[6],
                    const uint32_t pat100[6], uint32_t out[6]) {
    const uint32_t* slot = rate == 0 ? pat50 : rate == 1 ? pat75 : pat100;
    for (int i = 0; i < 6; ++i)
        out[i] = slot[i];
}

static void tick(Vpattern_count_bench& top) {
    top.clk = 1;
    top.eval();
    top.clk = 0;
    top.eval();
}

struct Run {
    const char* name;
    unsigned rate;
    unsigned corrupt_phases;
    bool last_only;
    bool withhold;
    // The last counted word is still one cycle behind pass/fail.
    bool skew;
    uint32_t counts[6];
};

static void sample(Vpattern_count_bench& top, uint32_t pat50[6], uint32_t pat75[6],
                   uint32_t pat100[6]) {
    copy6(top.pat50, pat50);
    copy6(top.pat75, pat75);
    copy6(top.pat100, pat100);
}

static void run_once(Vpattern_count_bench& top, const Run& run) {
    uint32_t before50[6], before75[6], before100[6], before[6];
    sample(top, before50, before75, before100);
    slot_of(run.rate, before50, before75, before100, before);
    const uint32_t ok_before = top.pat_ok;

    top.rate = run.rate;
    top.corrupt_phases = run.corrupt_phases;
    top.corrupt_last_only = run.last_only;
    top.withhold_done = run.withhold;
    top.reset = 1;
    for (int i = 0; i < 4; ++i)
        tick(top);
    top.reset = 0;

    bool finished = false;
    for (int i = 0; i < 40000 && !finished; ++i) {
        tick(top);
        finished = top.pass || top.fail;
    }
    require(finished, run.name);

    uint32_t now50[6], now75[6], now100[6], now[6], live[6];
    sample(top, now50, now75, now100);
    slot_of(run.rate, now50, now75, now100, now);
    // pass/fail is visible before the snapshot, and before the deferred count.
    require(same6(now, before), run.name);
    copy6(top.live, live);
    if (run.skew)
        require(sum6(live) + 1 == top.errors, run.name);
    else
        require(sum6(live) == top.errors, run.name);

    tick(top);
    sample(top, now50, now75, now100);
    slot_of(run.rate, now50, now75, now100, now);
    require(same6(now, before), run.name);
    copy6(top.live, live);
    require(sum6(live) == top.errors, run.name);

    tick(top);
    uint32_t got50[6], got75[6], got100[6], got[6];
    sample(top, got50, got75, got100);
    slot_of(run.rate, got50, got75, got100, got);
    require(same6(got, run.counts), run.name);
    copy6(top.live, live);
    require(same6(live, run.counts), run.name);
    require(top.errors == sum6(run.counts), run.name);
    require(static_cast<bool>(top.fail) == (sum6(run.counts) != 0), run.name);
    require(static_cast<bool>(top.pass) == (sum6(run.counts) == 0), run.name);
    require((top.pat_ok & (1u << run.rate)) != 0, run.name);
    require((top.pat_ok & ~(1u << run.rate)) == (ok_before & ~(1u << run.rate)), run.name);
    if (run.rate != 0)
        require(same6(got50, before50), run.name);
    if (run.rate != 1)
        require(same6(got75, before75), run.name);
    if (run.rate != 2)
        require(same6(got100, before100), run.name);
}

int main() {
    Vpattern_count_bench top;
    top.clk = 0;
    top.reset = 1;
    top.rate = 0;
    top.corrupt_phases = 0;
    top.corrupt_last_only = 0;
    top.withhold_done = 0;
    top.eval();

    const Run runs[] = {
        {"phase 0 and INVR final words", 2, 0x21, true, false, true, {1, 0, 0, 0, 0, 1}},
        {"INVR final word only", 2, 0x20, true, false, true, {0, 0, 0, 0, 0, 1}},
        {"clean 50 MHz pass", 0, 0, true, false, false, {0, 0, 0, 0, 0, 0}},
        {"5555 final word", 1, 0x04, true, false, false, {0, 0, 1, 0, 0, 0}},
        {"every read fails", 2, 0x3f, false, false, true, {4, 4, 4, 4, 4, 4}},
        {"timeout on the first word", 2, 0, true, true, true, {1, 0, 0, 0, 0, 0}},
    };
    for (const Run& run : runs)
        run_once(top, run);

    std::cout << "PASS: pattern table matches the total, including the final word\n";
    return 0;
}
