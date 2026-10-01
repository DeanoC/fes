// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vzx81_machine_clock.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>

static void require(bool ok, const char *message) {
    if (!ok) throw std::runtime_error(message);
}

static void cycle(Vzx81_machine_clock &dut) {
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
}

int main() {
    try {
        Vzx81_machine_clock dut;
        dut.reset = 1;
        cycle(dut);
        require(!dut.ce_6m5 && !dut.ce_cpu_p && !dut.ce_cpu_n && !dut.cpu_clock,
                "reset must clear clock and enables");
        dut.reset = 0;
        uint64_t ula = 0, positive = 0, negative = 0;
        uint64_t last_event = 0;
        bool previous_clock = false;
        // Four exact rational periods exercise odd event counts across periods
        // and prove the divide-by-two bus relationship without rounding drift.
        for (uint64_t n = 1; n <= 4 * 13056; ++n) {
            cycle(dut);
            require(!(dut.ce_cpu_p && dut.ce_cpu_n), "CPU edges overlap");
            require(bool(dut.ce_cpu_p || dut.ce_cpu_n) == bool(dut.ce_6m5),
                    "CPU edge must coincide with every ULA event");
            ula += dut.ce_6m5;
            positive += dut.ce_cpu_p;
            negative += dut.ce_cpu_n;
            require(ula == n * 1625 / 13056, "6.5 MHz phase error exceeds one host cycle");
            if (dut.ce_6m5) {
                if (last_event) require(n - last_event == 8 || n - last_event == 9,
                                        "ULA event spacing must be eight or nine cycles");
                last_event = n;
                require(bool(dut.cpu_clock) != previous_clock, "CPU clock must toggle on ULA event");
                require(bool(dut.ce_cpu_p) == bool(dut.cpu_clock), "CPU rising edge polarity");
                require(bool(dut.ce_cpu_n) != bool(dut.cpu_clock), "CPU falling edge polarity");
            } else {
                require(bool(dut.cpu_clock) == previous_clock, "CPU clock changed without an event");
            }
            previous_clock = dut.cpu_clock;
        }
        require(ula == 6500 && positive == 3250 && negative == 3250,
                "exact frequency ratio or divide-by-two count failed");
        dut.reset = 1;
        cycle(dut);
        require(!dut.ce_6m5 && !dut.ce_cpu_p && !dut.ce_cpu_n && !dut.cpu_clock,
                "mid-run reset must clear all outputs");
        dut.reset = 0;
        for (int n = 1; n <= 9; ++n) {
            cycle(dut);
            require(bool(dut.ce_6m5) == (n == 9), "reset did not restart deterministic phase");
        }
        std::cout << "ZX81 clock: exact 6.5/3.25 MHz ratios, bounded jitter, edge alignment and reset passed\n";
    } catch (const std::exception &e) {
        std::cerr << e.what() << '\n';
        return 1;
    }
}
