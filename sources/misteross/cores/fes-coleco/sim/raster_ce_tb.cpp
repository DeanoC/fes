// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vraster_ce_tb.h"
#include "verilated.h"

#include <algorithm>
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

constexpr uint64_t kRasterRateHz = 4'024'320;
constexpr uint64_t kSystemClockColecoHz = 52'224'000;
constexpr uint64_t kSystemClockComputerHz = 52'000'000;
constexpr uint64_t kCycles = 1'000'000;

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES TMS9918 raster: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

struct RateCheck {
    const char *name;
    uint64_t system_hz;
    uint64_t pulses = 0;
    uint64_t last_pulse = 0;
    uint64_t min_gap = UINT64_MAX;
    uint64_t max_gap = 0;

    void observe(bool pulse, uint64_t cycle) {
        if (!pulse) return;
        ++pulses;
        if (last_pulse != 0) {
            const uint64_t gap = cycle - last_pulse;
            min_gap = std::min(min_gap, gap);
            max_gap = std::max(max_gap, gap);
        }
        last_pulse = cycle;
    }

    void verify() const {
        const uint64_t expected = kCycles * kRasterRateHz / system_hz;
        const uint64_t error = pulses > expected ? pulses - expected : expected - pulses;
        require(error <= 1, name);
        require(min_gap == 12 && max_gap == 13,
                "fractional raster enable must distribute 12/13-clock intervals");
    }
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vraster_ce_tb dut;
    dut.clk = 0;
    dut.reset = 1;
    dut.eval();

    for (unsigned i = 0; i != 4; ++i) {
        dut.clk = 1;
        dut.eval();
        require(!dut.coleco_ce && !dut.computer_ce, "reset must suppress raster pulses");
        dut.clk = 0;
        dut.eval();
    }

    dut.reset = 0;
    RateCheck coleco{"Coleco 52.224 MHz rate is not about 60 frames/s", kSystemClockColecoHz};
    RateCheck computer{"SG-1000/SMS 52 MHz rate is not about 60 frames/s", kSystemClockComputerHz};
    for (uint64_t cycle = 1; cycle <= kCycles; ++cycle) {
        dut.clk = 1;
        dut.eval();
        coleco.observe(dut.coleco_ce, cycle);
        computer.observe(dut.computer_ce, cycle);
        dut.clk = 0;
        dut.eval();
    }

    coleco.verify();
    computer.verify();
    require(kRasterRateHz == 256 * 262 * 60,
            "raster rate no longer represents 256x262 samples at 60 Hz");

    std::cout << "FES TMS9918 raster: " << coleco.pulses << " / " << computer.pulses
              << " enables in " << kCycles << " system clocks; 60 Hz frame cadence\n";
    return EXIT_SUCCESS;
}
