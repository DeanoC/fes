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
    // Fill all sixteen palette codes through the real dual-clock framebuffer.
    dut.raster_ce = 1;
    for (unsigned y=0; y<192; ++y) for (unsigned x=0; x<256; ++x) {
        dut.logical_x=x; dut.logical_y=y; dut.logical_pixel=x/16;
        dut.clk_sys=1; dut.eval(); dut.clk_sys=0; dut.eval();
    }
    dut.raster_ce = 0;
    const uint32_t palette[] = {0,0,0x21c842,0x5edc78,0x5455ed,0x7d76fc,0xd4524d,0x42ebf5,
                               0xfc5554,0xff7978,0xd4c154,0xe6ce80,0x21b03b,0xc95bba,0xcccccc,0xffffff};

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
        const unsigned x=i%1650, y=(i/1650)%750;
        if (y>=168 && y<552 && x>=385 && x<896) {
            const uint32_t rgb=(uint32_t(dut.red)<<16)|(uint32_t(dut.green)<<8)|dut.blue;
            require(rgb==palette[((x-385)/2)/16], "full TMS palette did not survive framebuffer/HDMI");
        }
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
