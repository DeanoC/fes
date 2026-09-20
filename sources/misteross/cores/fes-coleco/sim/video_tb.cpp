// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_video_720p.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES Coleco video: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vcoleco_video_720p dut;
    dut.clk_sys = 0;
    dut.pixel_clk = 0;
    dut.raster_ce = 0;
    dut.logical_x = 0;
    dut.logical_y = 0;
    dut.logical_pixel = 1;
    dut.logical_blank = 0;
    dut.eval();

    unsigned hs_rises = 0;
    unsigned vs_rises = 0;
    unsigned de_count = 0;
    unsigned frame_ticks = 0;
    unsigned last_gap = 0;
    unsigned gap = 0;
    bool old_hs = false;
    bool old_vs = false;
    const unsigned cycles = 1650 * 751;
    for (unsigned i = 0; i != cycles; ++i) {
        dut.logical_x = uint8_t(i & 0xff);
        dut.logical_y = uint8_t((i >> 8) % 192);
        dut.clk_sys = 1;
        dut.eval();
        dut.clk_sys = 0;
        dut.eval();

        if (dut.hsync && !old_hs) {
            if (hs_rises != 0) last_gap = gap;
            gap = 0;
            ++hs_rises;
        }
        if (dut.vsync && !old_vs) ++vs_rises;
        if (dut.de) ++de_count;
        if (dut.frame_tick) ++frame_ticks;
        old_hs = dut.hsync;
        old_vs = dut.vsync;
        dut.pixel_clk = 1;
        dut.eval();
        dut.pixel_clk = 0;
        dut.eval();
        ++gap;
    }
    require(hs_rises >= 750, "not enough 720p lines");
    require(vs_rises >= 1, "missing 720p vsync");
    require(last_gap == 1650, "horizontal period is not 1650");
    require(de_count >= 1280u * 720u, "720p active area is too small");
    require(frame_ticks >= 1, "missing frame tick");
    std::cout << "FES Coleco video timing 1650x750 passed\n";
    return EXIT_SUCCESS;
}
