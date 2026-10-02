// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_z80_ce.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

static void check(bool ok, const char *message) {
    if (!ok) { std::cerr << message << '\n'; std::exit(1); }
}

int main() {
    Vfes_z80_ce dut;
    dut.clk = 1; dut.eval();
    unsigned positives = 0, negatives = 0, last_edge = 0;
    bool expect_positive = true;
    for (unsigned cycle = 1; cycle <= 52224000; ++cycle) {
        dut.clk = 0; dut.eval();
        check(!(dut.positive && dut.negative), "CPU half cycles overlap");
        if (dut.positive || dut.negative) {
            check(bool(dut.positive) == expect_positive, "CPU half cycles must alternate");
            if (last_edge) check(cycle - last_edge == 7 || cycle - last_edge == 8,
                                 "NTSC CPU half-cycle spacing");
            last_edge = cycle;
            expect_positive = !expect_positive;
        }
        positives += dut.positive;
        negatives += dut.negative;
        // Compare against elapsed ideal half cycles throughout the second,
        // so an eventual matching total cannot hide bad short-term cadence.
        const unsigned expected = uint64_t(cycle) * 7159090 / 52224000;
        check(positives + negatives == expected, "CPU cumulative phase error");
        dut.clk = 1; dut.eval();
    }
    check(positives == 3579545 && negatives == 3579545, "exact NTSC CPU frequency");
    std::cout << "NTSC Z80 exact enable counts, alternating edges and phase passed\n";
}
