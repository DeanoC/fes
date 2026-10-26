// SPDX-License-Identifier: MIT
// The pc386 OPL2 against Nuked-OPL3 in OPL2 mode, sample for sample
// (docs/pc386-machine-design.md, "OPL2"). Verilated top: pc386_opl2.
//
// Drives the wrapper's AdLib port as the guest would, one byte per isa8
// cycle (72 clocks at 100 MHz), and checks every output sample with
// sim/opl2_reference.h. Programs:
//   --random SEED SAMPLES   every channel keyed with a patch, then bursts of
//                           random writes to every register class, at random
//                           times, so some land while the operators compute
//   --log FILE              port writes captured from the machine bench:
//                           lines "<clock> <a0> <byte>" (hex byte)
// Prints PASS/FAIL lines and "OPL2 samples=<n> exact=<n> nonzero=<n> writes=<n>".
#include "Vpc386_opl2.h"
#include "Vpc386_opl2___024root.h"
#include "opl2_reference.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <random>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

constexpr uint64_t ISA8 = 72;

struct PortWrite {
    uint64_t clock;
    bool a0;
    uint8_t value;
};

class Bench {
public:
    Vpc386_opl2 top;
    Opl2Reference reference;
    uint64_t cycles = 0;

    Bench() {
        top.reset = 1;
        for (int n = 0; n < 4; ++n) clock();
        top.reset = 0;
    }
    void clock() {
        top.clk = 1; top.eval();
        top.clk = 0; top.eval();
        ++cycles;
        reference.clock(top.rootp->pc386_opl2__DOT__opl2_reg_wr, top.rootp->pc386_opl2__DOT__sample_tick,
                        top.sample_valid, int16_t(top.sample));
    }
    // One guest byte: access for a clock, then the rest of the isa8 cycle.
    void out(bool a0, uint8_t value) {
        top.access = 1; top.write = 1; top.a0 = a0; top.wdata = value;
        clock();
        top.access = 0; top.write = 0;
        for (uint64_t n = 1; n < ISA8; ++n) clock();
    }
    void run(const std::vector<PortWrite> &writes, uint64_t end) {
        for (const PortWrite &write : writes) {
            while (cycles < write.clock) clock();
            out(write.a0, write.value);
        }
        while (cycles < end) clock();
    }
};

// Every channel gets an audible patch and a key-on; then bursts of one to four
// writes to random registers of every class every 100-700 samples.
std::vector<PortWrite> random_program(unsigned seed, uint64_t samples) {
    std::mt19937 rng(seed);
    std::vector<std::pair<uint8_t, uint8_t>> start;
    static const int operator_offset[9] = {0, 1, 2, 8, 9, 10, 16, 17, 18};
    start.push_back({0x01, 0x20});
    for (int c = 0; c < 9; ++c) {
        for (int k = 0; k < 2; ++k) {
            int r = operator_offset[c] + 3 * k;
            start.push_back({uint8_t(0x20 + r), 0x21});
            start.push_back({uint8_t(0x40 + r), uint8_t(k ? 0x08 : 0x20)});
            start.push_back({uint8_t(0x60 + r), uint8_t(0xf4 - c)});
            start.push_back({uint8_t(0x80 + r), uint8_t(0x45 + c)});
        }
        start.push_back({uint8_t(0xc0 + c), uint8_t(0x02 + c)});
        start.push_back({uint8_t(0xa0 + c), uint8_t(0x40 + 17 * c)});
        start.push_back({uint8_t(0xb0 + c), uint8_t(0x31 + (c & 3) * 4)});
    }
    const uint64_t clocks_per_sample = 2011;
    std::vector<PortWrite> writes;
    uint64_t at = 1000;
    auto put = [&](uint8_t reg, uint8_t value) {
        writes.push_back({at, false, reg});
        at += ISA8 + rng() % 40;
        writes.push_back({at, true, value});
        at += ISA8 + rng() % 40;
    };
    for (auto [reg, value] : start) put(reg, value);
    static const uint8_t operator_bases[] = {0x20, 0x40, 0x60, 0x80, 0xe0};
    for (uint64_t sample = 200; sample < samples; sample += 100 + rng() % 600) {
        at = std::max<uint64_t>(at, sample * clocks_per_sample + rng() % clocks_per_sample);
        for (int n = 1 + rng() % 4; n > 0; --n) {
            int kind = rng() % 12, c = rng() % 9;
            uint8_t reg;
            if (kind < 5) reg = uint8_t(operator_bases[kind] + operator_offset[c] + 3 * (rng() & 1));
            else if (kind == 5) reg = uint8_t(0xa0 + c);
            else if (kind == 6) reg = uint8_t(0xb0 + c);
            else if (kind == 7) reg = uint8_t(0xc0 + c);
            else if (kind == 8) reg = 0xbd;
            else if (kind == 9) reg = 0x08;
            else if (kind == 10) reg = uint8_t(0x01 + rng() % 4);    // test, timers
            else reg = uint8_t(rng());                               // anything, unused ones too
            put(reg, uint8_t(rng()));
        }
    }
    return writes;
}

std::vector<PortWrite> read_log(const std::string &path) {
    std::ifstream input(path);
    if (!input) throw std::runtime_error("cannot read " + path);
    std::vector<PortWrite> writes;
    unsigned long long clock;
    unsigned a0, value;
    std::string line;
    while (std::getline(input, line)) {
        if (line.empty() || line[0] == '#') continue;
        if (std::sscanf(line.c_str(), "%llu %u %x", &clock, &a0, &value) != 3) throw std::runtime_error("bad log line: " + line);
        writes.push_back({clock, a0 != 0, uint8_t(value)});
    }
    return writes;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    try {
        std::vector<PortWrite> writes;
        uint64_t end = 0;
        std::string name;
        for (int n = 1; n < argc; ++n) {
            std::string flag = argv[n];
            if (flag == "--random" && n + 2 < argc) {
                unsigned seed = unsigned(std::stoul(argv[++n]));
                uint64_t samples = std::stoull(argv[++n]);
                writes = random_program(seed, samples);
                end = samples * 2011;
                name = "opl2-random-" + std::to_string(seed);
            } else if (flag == "--log" && n + 1 < argc) {
                writes = read_log(argv[++n]);
                end = (writes.empty() ? 0 : writes.back().clock) + 100000;
                name = "opl2-log";
            } else throw std::runtime_error("usage: opl2_tb --random SEED SAMPLES | --log FILE");
        }
        if (name.empty()) throw std::runtime_error("usage: opl2_tb --random SEED SAMPLES | --log FILE");
        Bench bench;
        bench.run(writes, std::max(end, bench.cycles));
        const Opl2Reference &r = bench.reference;
        std::printf("OPL2 samples=%llu exact=%llu nonzero=%llu writes=%llu\n", (unsigned long long)r.samples,
                    (unsigned long long)r.exact, (unsigned long long)r.nonzero, (unsigned long long)r.writes);
        bool ok = r.mismatches == 0 && r.samples > 0 && r.nonzero > 0;
        if (ok) std::printf("PASS %s\n", name.c_str());
        else std::printf("FAIL %s: %llu of %llu samples differ; first %s\n", name.c_str(),
                         (unsigned long long)r.mismatches, (unsigned long long)r.samples, r.first_mismatch.c_str());
        return ok ? 0 : 1;
    } catch (const std::exception &error) {
        std::fprintf(stderr, "opl2_tb: %s\n", error.what());
        return 2;
    }
}
