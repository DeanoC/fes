// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vvideo_pair.h"
#include <cstdint>
#include <cstdio>
#include <cstdlib>

static Vvideo_pair dut;
static uint64_t now, writes, publications, acknowledgements, boundaries;
static unsigned previous_events;
static uint32_t random_state = 0x91a25b07;
static uint32_t random_word() {
    random_state ^= random_state << 13;
    random_state ^= random_state >> 17;
    random_state ^= random_state << 5;
    return random_state;
}
static void evaluate() {
    dut.eval();
    if (dut.mismatch) {
        std::fprintf(stderr, "video pair differs at tick %llu\n", (unsigned long long)now);
        std::exit(1);
    }
    unsigned events = dut.events;
    if (dut.reset_n) {
        writes += !!((events & 2) && !(previous_events & 2));
        publications += !!((events ^ previous_events) & 4);
        acknowledgements += !!((events ^ previous_events) & 8);
        boundaries += !!((events & 1) && !(previous_events & 1));
    }
    previous_events = events;
}
static void step(bool sys, bool pixel) {
    dut.clk_sys = sys;
    dut.pixel_clk = pixel;
    evaluate();
    ++now;
}
static void reset(bool stopped) {
    dut.reset_n = 0;
    dut.held = 1;
    for (unsigned i = 0; i < 32; ++i)
        step(!stopped && (i & 1), !stopped && ((i / 2) & 1));
    dut.reset_n = 1;
    for (unsigned i = 0; i < 32; ++i)
        step(!stopped && (i & 1), !stopped && ((i / 2) & 1));
}
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    dut.ce_6m5 = 1;
    dut.hblank = 0;
    dut.vblank = 0;
    reset(false);
    // Releasing Hold in active video must not start a partial first frame.
    dut.held = 0;
    const uint64_t before = writes;
    for (unsigned i = 0; i < 4096; ++i) step(i & 1, (i % 7) < 3);
    if (writes != before) return 2;
    // Exercise arbitrary CE/blank/Hold input histories, then cleanly reset.
    for (unsigned i = 0; i < 200000; ++i) {
        if (!(i & 1)) {
            uint32_t r = random_word();
            dut.held = !(r & 255);
            dut.ce_6m5 = !!(r & 256);
            dut.hblank = !!(r & 512);
            dut.vblank = !!(r & 1024);
            dut.zx_pixel = !!(r & 2048);
        }
        step(i & 1, (i % 7) < 3);
    }
    reset(true); // asynchronous assertion while both clocks are stopped
    dut.held = 0;
    dut.ce_6m5 = 1;
    uint64_t source_cycle = 0;
    for (unsigned i = 0; i < 40000000; ++i) {
        // Both individual clock-loss cases, including an outstanding bank.
        bool sys = !(i >= 9000000 && i < 9100000) && (i & 1);
        bool pixel = !(i >= 3000000 && i < 3500000) && (i % 7) < 3;
        if (sys && !dut.clk_sys) {
            // Complete 328-pixel lines, including the maximum 270-row frame.
            uint64_t t = source_cycle++ % (32 + 270 * 392);
            dut.vblank = t < 32;
            dut.hblank = t < 32 || ((t - 32) % 392) >= 328;
            dut.zx_pixel = (source_cycle ^ (source_cycle >> 8)) & 1;
        }
        step(sys, pixel);
    }
    if (writes < 1000 || publications < 3 || acknowledgements < 2 || boundaries < 3) {
        std::fprintf(stderr, "insufficient full RTL coverage: %llu %llu %llu %llu\n",
            (unsigned long long)writes, (unsigned long long)publications,
            (unsigned long long)acknowledgements, (unsigned long long)boundaries);
        return 3;
    }
    std::printf("PASS dual-clock actual video pair: writes=%llu publications=%llu acknowledgements=%llu boundaries=%llu ticks=%llu\n",
        (unsigned long long)writes, (unsigned long long)publications,
        (unsigned long long)acknowledgements, (unsigned long long)boundaries,
        (unsigned long long)now);
}
