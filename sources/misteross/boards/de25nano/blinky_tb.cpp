#include "Vde25nano_blinky.h"
#include "Vde25nano_blinky___024root.h"
#include "verilated.h"

#include <cstdint>
#include <iostream>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vde25nano_blinky dut;
    dut.CLOCK0_50 = 0;
    dut.eval();
    if (dut.LEDR != 0xff) {
        std::cerr << "initial active-low LEDs must all be off\n";
        return 1;
    }
    auto cycle_clock = [&dut](std::uint8_t expected) {
        dut.CLOCK0_50 = 1;
        dut.eval();
        if (dut.LEDR != expected)
            return false;
        dut.CLOCK0_50 = 0;
        dut.eval();
        return dut.LEDR == expected;
    };
    auto set_ninit_done = [&dut](bool held) {
        // Drive the simulation IP endpoint, never the application's state.
        dut.rootp->de25nano_blinky__DOT__u_reset_release__DOT__ninit_done = held;
        dut.eval();
    };

    // Initialization can take arbitrarily long. More than one tick's worth
    // of clock edges must leave the application held at zero.
    constexpr std::uint32_t tick_cycles = 25000000;
    for (std::uint32_t cycle = 0; cycle < tick_cycles + 17; ++cycle) {
        if (!cycle_clock(0xff)) {
            std::cerr << "counter advanced before device initialization\n";
            return 1;
        }
    }
    set_ninit_done(false);
    for (int edge = 0; edge < 2; ++edge) {
        if (!cycle_clock(0xff)) {
            std::cerr << "counter advanced during synchronous reset release\n";
            return 1;
        }
    }

    // Use the hardware divider unchanged through three half-second ticks,
    // including the binary carry into LED 1.
    for (std::uint32_t cycle = 1; cycle <= 3 * tick_cycles; ++cycle) {
        const auto expected = static_cast<std::uint8_t>(~(cycle / tick_cycles));
        if (!cycle_clock(expected)) {
            std::cerr << "LED cadence/polarity mismatch at cycle " << cycle << '\n';
            return 1;
        }
    }

    // Reset away from a tick boundary, so the restart test also detects an
    // uncleared divider rather than only a reset of the visible LED state.
    for (int edge = 0; edge < 17; ++edge) {
        if (!cycle_clock(0xfc)) {
            std::cerr << "LED state changed between ticks\n";
            return 1;
        }
    }

    // A new initialization/reset event must clear both the LED state and
    // divider even without a clock edge, then restart a full tick interval.
    set_ninit_done(true);
    if (dut.LEDR != 0xff) {
        std::cerr << "reset failed to assert asynchronously\n";
        return 1;
    }
    for (int edge = 0; edge < 7; ++edge) {
        if (!cycle_clock(0xff)) {
            std::cerr << "counter advanced while reset was reasserted\n";
            return 1;
        }
    }
    set_ninit_done(false);
    for (int edge = 0; edge < 2; ++edge) {
        if (!cycle_clock(0xff)) {
            std::cerr << "counter advanced during second reset release\n";
            return 1;
        }
    }
    for (std::uint32_t cycle = 1; cycle <= tick_cycles; ++cycle) {
        if (!cycle_clock(cycle == tick_cycles ? 0xfe : 0xff)) {
            std::cerr << "divider did not restart after reset\n";
            return 1;
        }
    }
    dut.final();
    std::cout << "DE25-Nano initialization hold, reset, active-low LEDs and 50 MHz cadence passed\n";
    return 0;
}
