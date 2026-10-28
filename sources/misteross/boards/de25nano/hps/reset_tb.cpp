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

    void assert_reset(const char* phase) {
        dut.async_reset_n = 0;
        expect(false, phase);
    }

    void release_two_edges() {
        // Drop the asynchronous source with no edge: release must wait.
        dut.async_reset_n = 1;
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
    bench.dut.async_reset_n = 1;
    bench.dut.eval();
    bench.assert_reset("startup assertion without clock");
    for (int edge = 0; edge < 6; ++edge) {
        bench.clock(edge & 1);
        bench.expect(false, "startup reset held across clocks");
    }
    bench.release_two_edges();

    // PLL unlock/configuration reset must assert even when the clock stops
    // high or low. A lock recovery without edges cannot release the reset.
    for (bool stopped_high : {false, true}) {
        bench.clock(stopped_high);
        bench.assert_reset("asynchronous assertion with stopped clock");
        for (int observation = 0; observation < 4; ++observation)
            bench.expect(false, "held with stopped clock");
        bench.release_two_edges();
    }

    // A short loss pulse between edges invalidates a partially completed
    // release; two fresh edges are needed after every relock.
    bench.clock(false);
    bench.assert_reset("short loss pulse");
    bench.dut.async_reset_n = 1;
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
    std::cout << "DE25 top reset: startup, stopped-clock assertion, two-edge release and relock passed\n";
    return 0;
}
