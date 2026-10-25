// SPDX-License-Identifier: GPL-2.0-or-later
// lat_stats against a reference model of its bins, plus hand-checked
// windows: exact percentiles near the base, the base following the
// previous minimum per target, bin tops capped at the maximum, the
// overflow bin, and a cleared histogram between windows.
#include "Vlat_stats.h"
#include "verilated.h"
#include <algorithm>
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <random>
#include <vector>

static constexpr int SAMPLES = 100;  // built with -GSAMPLES=100

static void require(bool ok, const char* message) {
    if (!ok) {
        std::cerr << message << '\n';
        std::exit(1);
    }
}

struct Result {
    int min, med, p99, max;
};

// The RTL's bins: one cycle up to base+31, eight per doubling to
// base+1023, then one bin for the rest. A percentile is its bin's top,
// capped at the maximum.
struct Model {
    std::array<int, 4> base{};

    static int bin_of(int offset) {
        if (offset >= 1024) return 72;
        if (offset < 32) return offset;
        int octave = 5;
        while (offset >= (1 << (octave + 1))) ++octave;
        return 32 + (octave - 5) * 8 + ((offset >> (octave - 3)) & 7);
    }

    static int bin_top(int bin) {
        if (bin < 32) return bin;
        const int octave = 5 + (bin - 32) / 8;
        const int step = 1 << (octave - 3);
        return (1 << octave) + ((bin - 32) % 8 + 1) * step - 1;
    }

    Result window(int target, const std::vector<int>& values) {
        std::array<int, 73> counts{};
        int lo = 0x3fff, hi = 0;
        for (int value : values) {
            ++counts[bin_of(std::max(value - base[target], 0))];
            lo = std::min(lo, value);
            hi = std::max(hi, value);
        }
        auto quantile = [&](int rank) {
            int below = 0;
            for (int bin = 0; bin < 73; ++bin) {
                below += counts[bin];
                if (below >= rank)
                    return bin == 72 ? hi : std::min(base[target] + bin_top(bin), hi);
            }
            return 0;
        };
        Result result{lo, quantile(SAMPLES / 2), quantile(SAMPLES - SAMPLES / 100), hi};
        if (!values.empty())
            base[target] = lo;
        return result;
    }
};

struct Bench {
    Vlat_stats dut;
    uint64_t cycles = 0;

    Bench() {
        dut.clk = 0;
        dut.reset = 1;
        dut.sample = 0;
        dut.finish = 0;
        dut.value = 0;
        dut.target = 0;
        tick();
        tick();
        dut.reset = 0;
        tick();
    }

    void tick() {
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
        ++cycles;
    }

    void wait_idle() {
        for (int i = 0; i < 64 && !dut.idle; ++i) tick();
        require(dut.idle, "lat_stats did not return to idle");
    }

    Result window(int target, const std::vector<int>& values) {
        dut.target = target;
        for (int value : values) {
            wait_idle();
            dut.value = value;
            dut.sample = 1;
            tick();
            dut.sample = 0;
        }
        wait_idle();
        dut.finish = 1;
        tick();
        dut.finish = 0;
        for (int i = 0; i < 200 && !dut.done; ++i) tick();
        require(dut.done, "lat_stats did not finish the window");
        Result result{dut.min, dut.med, dut.p99, dut.max};
        tick();
        wait_idle();
        return result;
    }
};

static void expect(const Result& got, const Result& want, const char* what) {
    if (got.min != want.min || got.med != want.med || got.p99 != want.p99 || got.max != want.max) {
        std::cerr << what << ": got " << got.min << ' ' << got.med << ' ' << got.p99 << ' ' << got.max
                  << " want " << want.min << ' ' << want.med << ' ' << want.p99 << ' ' << want.max << '\n';
        std::exit(1);
    }
}

int main() {
    Bench bench;
    Model model;

    // 60 x 30, 30 x 31, 9 x 40, 1 x 500. With base 0 the 99th sample (40)
    // falls in bin 40-43; the next window has base 30 and is exact.
    std::vector<int> narrow;
    narrow.insert(narrow.end(), 60, 30);
    narrow.insert(narrow.end(), 30, 31);
    narrow.insert(narrow.end(), 9, 40);
    narrow.push_back(500);
    std::shuffle(narrow.begin(), narrow.end(), std::mt19937(7));
    expect(bench.window(1, narrow), Result{30, 30, 43, 500}, "first window on base 0");
    model.window(1, narrow);
    expect(bench.window(1, narrow), Result{30, 30, 40, 500}, "second window on base 30");
    model.window(1, narrow);

    // Target 2 still has base 0: 208 is in bin 208-223, and the cap holds
    // a percentile at the maximum.
    std::vector<int> line(100, 208);
    line[17] = 244;
    expect(bench.window(2, line), Result{208, 223, 223, 244}, "SDRAM-like line on base 0");
    model.window(2, line);
    expect(bench.window(2, line), Result{208, 208, 208, 244}, "SDRAM-like line on base 208");
    model.window(2, line);

    // Beyond base+1023 a percentile reports the maximum, until the base
    // catches up in the next window.
    std::vector<int> far(100, 3000);
    far[0] = 16383;
    expect(bench.window(3, far), Result{3000, 16383, 16383, 16383}, "overflow bin");
    model.window(3, far);
    expect(bench.window(3, far), Result{3000, 3000, 3000, 16383}, "overflow bin on base 3000");
    model.window(3, far);

    // Random windows against the model, per target, bases carried over.
    std::mt19937 random(12345);
    for (int round = 0; round < 200; ++round) {
        const int target = round % 4;
        const int floor = 10 + static_cast<int>(random() % 60);
        std::vector<int> values;
        for (int i = 0; i < SAMPLES; ++i) {
            int value = floor + static_cast<int>(random() % 8);
            if (random() % 10 == 0) value += static_cast<int>(random() % 200);
            if (random() % 50 == 0) value += static_cast<int>(random() % 3000);
            if (random() % 3 == 0 && round % 7 == 0) value -= static_cast<int>(random() % 9);
            values.push_back(std::max(value, 1));
        }
        expect(bench.window(target, values), model.window(target, values), "random window");
    }
    std::cout << "PASS: latency statistics, exact near the base, bin tops capped, overflow, 200 random windows\n";
    return 0;
}
