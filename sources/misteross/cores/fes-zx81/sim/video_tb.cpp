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

void tick_sys(Vzx81_video_720p &dut) {
    dut.ce_6m5 = 1;
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
}

// Matches zx81_video_720p: the captured raster is placed at (312, 90) and
// each source pixel is 2x2. The readout register delays the bit by one
// pixel clock, so source (20, 10) is visible at output (353, 111).
constexpr int kPictureLeft = 312;
constexpr int kPictureTop = 90;
constexpr int kInkX = 20;
constexpr int kInkY = 10;

void capture(Vzx81_video_720p &dut) {
    dut.hblank = 1;
    dut.vblank = 1;
    dut.zx_pixel = 0;
    tick_sys(dut);
    dut.vblank = 0;
    for (int y = 0; y != 270; ++y) {
        dut.hblank = 0;
        for (int x = 0; x != 328; ++x) {
            dut.zx_pixel = (y == kInkY && x == kInkX) ? 1 : 0;
            tick_sys(dut);
        }
        dut.hblank = 1;
        dut.zx_pixel = 0;
        tick_sys(dut);
    }
    dut.ce_6m5 = 0;
}

bool white(const Vzx81_video_720p &dut) {
    return dut.red == 0xff && dut.green == 0xff && dut.blue == 0xff;
}

bool black(const Vzx81_video_720p &dut) {
    return dut.red == 0 && dut.green == 0 && dut.blue == 0;
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
    capture(dut);

    unsigned hs_rises = 0, vs_rises = 0, de_count = 0;
    unsigned last_gap = 0, gap = 0;
    int prev_hs = 0, prev_vs = 0;
    bool saw_ink = false, saw_paper = false, saw_margin = false;
    const int cycles = 1650 * 751;
    for (int i = 0; i != cycles; ++i) {
        if (dut.raster_h == kPictureLeft + 2 * kInkX + 1 &&
            dut.raster_v == kPictureTop + 2 * kInkY + 1) {
            require(black(dut), "ink pixel is not black");
            saw_ink = true;
        }
        if (dut.raster_h == kPictureLeft + 2 * (kInkX + 1) + 1 &&
            dut.raster_v == kPictureTop + 2 * kInkY + 1) {
            require(white(dut), "paper pixel is not white");
            saw_paper = true;
        }
        if (dut.raster_h == 10 && dut.raster_v == 10) {
            require(black(dut), "720p margin is not black");
            saw_margin = true;
        }
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
    require(saw_ink, "missed the ink pixel");
    require(saw_paper, "missed the paper pixel");
    require(saw_margin, "missed the 720p margin");
    std::cout << "FES ZX81: 720p timing 1650x750, white paper, black ink\n";
    return 0;
}
