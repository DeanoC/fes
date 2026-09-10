// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vzx81_video_720p.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES ZX81 video: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vzx81_video_720p dut;
    dut.clk_sys = 0;
    dut.pixel_clk = 0;
    dut.ce_6m5 = 0;
    dut.zx_pixel = 0;
    dut.hblank = 1;
    dut.vblank = 1;
    dut.eval();

    unsigned hs_rises = 0, vs_rises = 0, de_count = 0;
    unsigned last_gap = 0, gap = 0;
    int prev_hs = 0, prev_vs = 0;
    const int cycles = 1650 * 751;
    for (int i = 0; i != cycles; ++i) {
        if (dut.hsync && !prev_hs) {
            if (hs_rises > 0)
                last_gap = gap;
            gap = 0;
            hs_rises++;
        }
        if (dut.vsync && !prev_vs)
            vs_rises++;
        if (dut.de)
            de_count++;
        prev_hs = dut.hsync;
        prev_vs = dut.vsync;
        dut.pixel_clk = 1;
        dut.eval();
        dut.pixel_clk = 0;
        dut.eval();
        gap++;
    }
    require(hs_rises >= 750, "not enough 720p lines");
    require(vs_rises >= 1, "missing 720p vsync");
    require(last_gap == 1650, "horizontal period is not 1650");
    require(de_count >= 1280u * 720u, "720p active area is too small");
    std::cout << "FES ZX81: 720p timing 1650x750\n";
    return 0;
}
