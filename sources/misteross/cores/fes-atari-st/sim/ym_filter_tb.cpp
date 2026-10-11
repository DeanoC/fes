// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vst_ym_filter.h"
#include "verilated.h"
#include "st_ym_filter_reference.hpp"
#include <algorithm>
#include <cstdint>
#include <cstdlib>
#include <deque>
#include <functional>
#include <iostream>
#include <string>

// Keep these definitions equal to the Verilator -G parameters. The default
// scaled fixture retains the production rate relationship but runs faster.
#ifndef ST_YM_FILTER_TEST_CHIP_HZ
#define ST_YM_FILTER_TEST_CHIP_HZ 500000u
#endif
#ifndef ST_YM_FILTER_TEST_SYS_HZ
#define ST_YM_FILTER_TEST_SYS_HZ 8000000u
#endif
#ifndef ST_YM_FILTER_TEST_SAMPLE_HZ
#define ST_YM_FILTER_TEST_SAMPLE_HZ 48000u
#endif

struct Test {
    static constexpr uint64_t chip = ST_YM_FILTER_TEST_CHIP_HZ;
    static constexpr uint64_t fabric = ST_YM_FILTER_TEST_SYS_HZ;
    static constexpr uint64_t sample = ST_YM_FILTER_TEST_SAMPLE_HZ;
    Vst_ym_filter d;
    st_ym_filter_reference::Reference reference{unsigned(chip), unsigned(sample)};
    uint64_t cycle = 0, checks = 0, outputs = 0, maximum_busy = 0;
    bool job = false;
    uint64_t accepted_at = 0;
    std::string scenario;
    struct Expected { int16_t value; uint64_t requested_at; };
    std::deque<Expected> expected;

    void require(bool ok, const char* message) {
        ++checks;
        if (!ok) {
            std::cerr << "YM filter " << scenario << " at edge " << cycle << ": " << message << '\n';
            std::exit(1);
        }
    }
    void edge() { d.clk = 0; d.eval(); d.clk = 1; d.eval(); ++cycle; }
    void reset() {
        d.reset = 1; d.input_valid = 0; d.sample_ce = 0;
        edge(); edge();
        reference.reset(); expected.clear(); job = false;
        require(!d.busy && !d.sample_valid && !d.fault && d.pcm == 0, "reset clears work, PCM and fault");
        d.reset = 0;
    }
    void tick(bool input, unsigned raw, bool pop) {
        d.input_valid = input; d.input_pcm = raw; d.sample_ce = pop;
        if (input) {
            require(!d.busy && !job, "counter input arrives while idle");
            reference.input(raw); job = true; accepted_at = cycle + 1;
        }
        if (pop) {
            require(reference.pending() != 0, "scheduled PCM pop has a completed reference interval");
            expected.push_back({reference.output(), cycle + 1});
        }
        edge();
        require(!d.fault, "regular cadence has no overflow, underrun or job collision");
        if (job) {
            const auto age = cycle - accepted_at;
            require(age <= 48, "counter processing finishes within 48 edges");
            if (!d.busy) {
                maximum_busy = std::max(maximum_busy, age);
                job = false;
            }
        }
        if (d.sample_valid) {
            require(!expected.empty(), "PCM valid belongs to a requested pop");
            require(cycle == expected.front().requested_at, "PCM completion occurs on the sample enable edge");
            const int16_t actual = int16_t(uint16_t(d.pcm));
            if (actual != expected.front().value)
                std::cerr << "expected " << expected.front().value << ", actual " << actual << '\n';
            require(actual == expected.front().value, "exact STF low-pass/integration/DC oracle PCM");
            expected.pop_front(); ++outputs;
        }
        if (!expected.empty())
            require(cycle == expected.front().requested_at, "PCM pop must produce a valid sample");
    }
    void stream(const char* name, unsigned input_count, const std::function<unsigned(unsigned)>& signal) {
        scenario = name; reset();
        uint64_t source_phase = 0, output_phase = 0;
        unsigned count = 0;
        const uint64_t start = cycle;
        while (count < input_count) {
            source_phase += chip;
            bool input = false;
            unsigned raw = 0;
            if (source_phase >= fabric * 8) {
                source_phase -= fabric * 8; input = true; raw = signal(count++);
            }
            bool pop = false;
            if (cycle - start >= 320) {
                output_phase += sample;
                if (output_phase >= fabric) { output_phase -= fabric; pop = true; }
            }
            tick(input, raw, pop);
        }
        // Finish a possible input pipeline without asking for another interval.
        for (unsigned i = 0; i < 50; ++i) tick(false, 0, false);
        require(!d.busy && expected.empty(), "stream drains in-flight work");
    }
    void reset_jobs() {
        scenario = "reset in flight";
        // Stage-specific cancellation, including split interval multiplication
        // and long division. A subsequent regular stream detects stale FIFO data.
        for (unsigned phase : {0u, 1u, 2u, 4u, 8u, 16u, 24u, 35u, 45u}) {
            reset();
            // Advance to the first target boundary at either clock rate.
            const unsigned until_boundary = unsigned((chip + 8 * sample - 1) / (8 * sample)) - 1;
            for (unsigned j = 1; j < until_boundary; ++j) {
                tick(true, 12345, false);
                for (unsigned i = 0; i < 48; ++i) tick(false, 0, false);
            }
            tick(true, 32767, false);
            for (unsigned i = 0; i < phase; ++i) tick(false, 0, false);
            reset();
            for (unsigned i = 0; i < 50; ++i) tick(false, 0, false);
            stream("fresh after cancellation", 64, [](unsigned n) { return n & 1 ? 12345u : 0u; });
        }
    }
    void faults() {
        scenario = "fault contract";
        reset(); d.input_valid = 0; d.sample_ce = 1; edge();
        require(d.fault && d.sample_valid && d.pcm == 0, "empty FIFO pop sets fault and emits silence");
        d.sample_ce = 0; edge(); require(d.fault, "underrun fault is sticky");
        reset(); d.input_pcm = 12345; d.input_valid = 1; edge();
        require(d.busy, "accepted input enters busy pipeline");
        edge(); require(d.fault, "input collision sets fault");
        d.input_valid = 0; edge(); require(d.fault, "collision fault is sticky");
        reset();
        // With no output pops, enough inputs must overflow the four-entry FIFO.
        for (unsigned j = 0; j < 40; ++j) {
            d.input_pcm = j & 1 ? 32767 : 12345;
            d.input_valid = 1; edge(); d.input_valid = 0;
            for (unsigned i = 0; i < 48; ++i) edge();
        }
        require(d.fault, "unconsumed average FIFO overflow sets fault");
        edge(); require(d.fault, "overflow fault is sticky"); reset();
    }
    void oracle_sanity() {
        // Manually derived C10 filter values distinguish the former PWM path.
        st_ym_filter_reference::Reference r(500000, 48000);
        r.input(32767);
        require(r.low_pass() == 12287 && r.pending() == 1, "rising STF edge is filtered and seeds first boundary");
        require(r.output() == int16_t(uint64_t(12287) * 116000 / 500000), "first weighted impulse with zero seed");
        r.input(0);
        require(r.low_pass() == 13311, "falling STF edge uses input history and sixfold output history");
        const int32_t first = int32_t(uint64_t(12287) * 116000 / 500000);
        const int32_t second = int32_t((uint64_t(12287) * 268000 + uint64_t(13311) * 232000) / 500000);
        require(r.output() == second - (first * 64 + 32767) / 32768, "DC state rounds negative correction downward");
        r.reset(); require(r.pending() == 0 && r.low_pass() == 0, "oracle reset clears all histories");
    }
    void run() {
        d.clk = 0; scenario = "reference sanity"; oracle_sanity();
        stream("silence", 256, [](unsigned) { return 0u; });
        stream("isolated impulses", 1024, [](unsigned n) { return n % 127 == 0 ? 32767u : 0u; });
        stream("alternating extrema", 2048, [](unsigned n) { return n & 1 ? 32767u : 0u; });
        stream("DC then silence", 24000, [](unsigned n) { return n < 16000 ? 32767u : 0u; });
        stream("high frequency saw", 4096, [](unsigned n) { return (n * 7919u) & 32767u; });
        uint32_t random = 0x2149abcd;
        stream("deterministic random", 4096, [&random](unsigned) {
            random ^= random << 13; random ^= random >> 17; random ^= random << 5;
            return random & 32767u;
        });
        reset_jobs(); faults();
        std::cout << "YM filter: " << outputs << " exact PCM samples, " << checks
                  << " checks; maximum input processing " << maximum_busy
                  << " edges; chip=" << chip << ", fabric=" << fabric
                  << "; STF low-pass/rational integration/DC/reset passed\n";
        d.final();
    }
};
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Test test;
    test.run();
}
