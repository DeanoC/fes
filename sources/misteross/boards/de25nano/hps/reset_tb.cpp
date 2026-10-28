#include "Vfes_de25_top_reset.h"
#include "verilated.h"

#include <cstdlib>
#include <iostream>

class ResetBench {
public:
    Vfes_de25_top_reset dut;

    void expect(bool released, const char* phase) {
        dut.eval();
        if (bool(dut.reset_n) != released) {
            std::cerr << phase << ": reset_n=" << unsigned(dut.reset_n)
                      << ", expected " << released << '\n';
            std::exit(1);
        }
    }

    void clock(bool high) {
        dut.clk = high;
        dut.eval();
    }

    void sources(bool configuration_reset, bool locked) {
        dut.ninit_done = configuration_reset;
        dut.pll_locked = locked;
        dut.eval();
    }

    void assert_reset(const char* phase, bool configuration_reset = true, bool locked = true) {
        sources(configuration_reset, locked);
        expect(false, phase);
    }

    void release_two_edges() {
        // Both real source inputs must permit release, then two edges qualify it.
        sources(false, true);
        expect(false, "release without clock");
        clock(false);
        expect(false, "release on falling edge");
        clock(true);
        expect(false, "first rising edge still holds reset");
        clock(false);
        expect(false, "between release stages");
        clock(true);
        expect(true, "second rising edge releases reset");
        clock(false);
        expect(true, "released between edges");
        clock(true);
        expect(true, "stays released");
    }
};

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    ResetBench bench;
    bench.clock(false);
    // Force a real assertion event before any clock; do not rely on the
    // simulator's initial register value to establish the startup state.
    bench.sources(false, true);
    bench.assert_reset("startup assertion without clock", true, false);
    for (int edge = 0; edge < 6; ++edge) {
        bench.clock(edge & 1);
        bench.expect(false, "startup reset held across clocks");
    }
    bench.sources(false, false);
    for (int edge = 0; edge < 6; ++edge) {
        bench.clock(edge & 1);
        bench.expect(false, "configuration finished but PLL not locked");
    }
    bench.release_two_edges();

    // Check every source combination from a released state. Either reset
    // cause independently holds the common output, including across clocks.
    for (bool configuration_reset : {false, true}) {
        for (bool locked : {false, true}) {
            bench.sources(configuration_reset, locked);
            const bool can_run = !configuration_reset && locked;
            bench.expect(can_run, "source truth table assertion");
            for (int edge = 0; edge < 6; ++edge) {
                bench.clock(edge & 1);
                bench.expect(can_run, "source truth table hold");
            }
            if (!can_run)
                bench.release_two_edges();
        }
    }

    // PLL unlock/configuration reset must assert even when the clock stops
    // high or low. A lock recovery without edges cannot release the reset.
    for (bool stopped_high : {false, true}) {
        for (bool configuration_reset : {false, true}) {
            bench.clock(stopped_high);
            bench.assert_reset("asynchronous assertion with stopped clock",
                               configuration_reset, configuration_reset);
            for (int observation = 0; observation < 4; ++observation)
                bench.expect(false, "held with stopped clock");
            bench.release_two_edges();
        }
    }

    // A short loss pulse between edges invalidates a partially completed
    // release; two fresh edges are needed after every relock.
    bench.clock(false);
    bench.assert_reset("short loss pulse", false, false);
    bench.sources(false, true);
    bench.expect(false, "pulse ended without edge");
    bench.clock(true);
    bench.expect(false, "partial release");
    bench.assert_reset("new loss interrupts first release stage");
    bench.release_two_edges();

    // Assertion has priority when a rising edge occurs under reset.
    bench.clock(false);
    bench.assert_reset("reset before rising edge");
    bench.clock(true);
    bench.expect(false, "assertion priority");
    bench.release_two_edges();
    std::cout << "DE25 common reset: source truth table, startup, stopped-clock assertion, two-edge release and relock passed\n";
    return 0;
}
