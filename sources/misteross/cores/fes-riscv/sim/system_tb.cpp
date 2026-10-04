// SPDX-License-Identifier: GPL-2.0-or-later
// fes_riscv_system with the real firmware: gradient, border, banner, the
// gamepad-driven box, the timer interrupt colour cycle and execution hold.
#include "Vfes_riscv_system.h"
#include "verilated.h"
#include "frame_capture.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "fes_riscv_system: " << message << '\n';
    std::exit(EXIT_FAILURE);
}
void require(bool ok, const std::string &message) { if (!ok) fail(message); }
std::string hex(uint32_t v) { char b[16]; std::snprintf(b, sizeof b, "0x%06x", v); return b; }

constexpr uint8_t PALETTE[8] = {0xe0, 0xfc, 0x1c, 0x1f, 0x03, 0xe3, 0xff, 0x92};
constexpr int BOX = 12, BOX_X0 = (160 - BOX) / 2, BOX_Y0 = (120 - BOX) / 2;
constexpr uint64_t TICK_CYCLES = 18562500;   // the firmware's timer period

struct System {
    Vfes_riscv_system dut;
    FrameCapture capture;
    uint64_t cycles = 0, steps = 0;

    System() {
        dut.pixel_clk = 0; dut.exec_reset = 1; dut.buttons = 0;
        dut.eval();
    }
    void tick() {
        dut.pixel_clk = 1; dut.eval();
        dut.pixel_clk = 0; dut.eval();
        ++cycles;
        steps += dut.cpu_step;
        capture.sample(dut.hdmi_de, dut.hdmi_vs, dut.hdmi_rgb);
    }
    uint64_t reset_cycle = 0;
    void reset(unsigned cycles_held = 16) {
        dut.exec_reset = 1;
        for (unsigned i = 0; i < cycles_held; ++i) tick();
        dut.exec_reset = 0;
        reset_cycle = cycles;
    }
    void frames(unsigned count) {
        unsigned target = capture.frames + count;
        uint64_t limit = cycles + uint64_t(count + 2) * 1650 * 750;
        while (capture.frames < target) {
            require(cycles < limit, "raster did not complete the requested frames");
            tick();
        }
    }
    void expect_pixel(int fx, int fy, uint8_t colour, const std::string &what) {
        uint32_t got = capture.at(fx, fy), want = FrameCapture::expand(colour);
        require(got == want, what + " at (" + std::to_string(fx) + "," + std::to_string(fy) + ") = " +
                hex(got) + " expected " + hex(want));
    }
    // The box colour follows the timer tick at the moment the firmware drew
    // the frame, which is up to one frame before the capture completed.
    bool colour_plausible(uint8_t colour) const {
        uint64_t tick = (cycles - reset_cycle) / TICK_CYCLES;
        return colour == PALETTE[tick & 7] || colour == PALETTE[(tick - 1) & 7];
    }
    uint8_t expect_box(int bx, int by) {
        int row = by + BOX / 2, found = -1;
        uint8_t colour = 0;
        for (uint8_t candidate : PALETTE) {
            found = capture.find_run(row, FrameCapture::expand(candidate), BOX);
            if (found >= 0) { colour = candidate; break; }
        }
        require(found == bx, "box run in row " + std::to_string(row) + " starts at " +
                std::to_string(found) + " expected " + std::to_string(bx));
        require(colour_plausible(colour), "box colour " + hex(colour) + " does not follow the timer tick");
        for (int dy = 0; dy < BOX; ++dy) {
            expect_pixel(bx, by + dy, colour, "box left edge");
            expect_pixel(bx + BOX - 1, by + dy, colour, "box right edge");
        }
        expect_pixel(bx - 1, by + BOX / 2, FrameCapture::background(bx - 1, by + BOX / 2), "left of box");
        expect_pixel(bx + BOX, by + BOX / 2, FrameCapture::background(bx + BOX, by + BOX / 2), "right of box");
        expect_pixel(bx + BOX / 2, by - 1, FrameCapture::background(bx + BOX / 2, by - 1), "above box");
        expect_pixel(bx + BOX / 2, by + BOX, FrameCapture::background(bx + BOX / 2, by + BOX), "below box");
        return colour;
    }
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    System s;
    s.reset();
    s.frames(3);
    require(s.steps > 100000, "the CPU did not execute the firmware");
    require(s.capture.bar_violations == 0, "pixels outside the 960-wide playfield must be black");

    // Border and gradient.
    for (int fx = 0; fx < 160; ++fx) {
        s.expect_pixel(fx, 0, 0xff, "top border");
        s.expect_pixel(fx, 119, 0xff, "bottom border");
    }
    for (int fy = 0; fy < 120; ++fy) {
        s.expect_pixel(0, fy, 0xff, "left border");
        s.expect_pixel(159, fy, 0xff, "right border");
    }
    for (int fy = 30; fy < 50; fy += 3)
        for (int fx = 1; fx < 159; fx += 7)
            s.expect_pixel(fx, fy, FrameCapture::background(fx, fy), "gradient");
    s.expect_pixel(100, 100, 0x79, "gradient reference value");

    // Banner: 'F' at (26, 8) scaled 2x, column 0 solid, row 1 open after the stem.
    for (int dy = 0; dy < 14; ++dy) s.expect_pixel(26, 8 + dy, 0xff, "F stem");
    s.expect_pixel(27, 9, 0xff, "F stem second column");
    s.expect_pixel(34, 8, 0xff, "F top bar end");
    s.expect_pixel(28, 10, FrameCapture::background(28, 10), "gap below the F bar");
    s.expect_pixel(34, 20, FrameCapture::background(34, 20), "F lower right is open");
    // 'I' is the ninth glyph: x = 26 + 8 * 12 = 122; its centre column is solid.
    for (int dy = 0; dy < 14; ++dy) s.expect_pixel(122 + 4, 8 + dy, 0xff, "I centre column");
    s.expect_pixel(122, 12, FrameCapture::background(122, 12), "I left column is open mid-glyph");

    // The box starts centred in the first palette colour.
    require(s.expect_box(BOX_X0, BOX_Y0) == PALETTE[0], "initial box colour");

    // Right for ten frame ticks moves it ten pixels; a release stops it.
    s.dut.buttons = 8;
    s.frames(10);
    s.dut.buttons = 0;
    s.frames(2);
    s.expect_box(BOX_X0 + 10, BOX_Y0);
    s.dut.buttons = 1 | 4;      // up and left together
    s.frames(4);
    s.dut.buttons = 0;
    s.frames(2);
    s.expect_box(BOX_X0 + 6, BOX_Y0 - 4);

    // The timer interrupt advances the palette every TICK_CYCLES.
    while ((s.cycles - s.reset_cycle) / TICK_CYCLES < 2) s.frames(1);
    s.frames(2);
    uint8_t colour = s.expect_box(BOX_X0 + 6, BOX_Y0 - 4);
    require(colour != PALETTE[0], "the box colour must have cycled by now");

    // Holding the box against the border clamps it inside the frame.
    s.dut.buttons = 2;          // down
    s.frames(70);
    s.dut.buttons = 0;
    s.frames(2);
    {
        int row = 120 - BOX - 1 + BOX / 2;
        int found = -1;
        for (uint8_t p : PALETTE)
            if ((found = s.capture.find_run(row, FrameCapture::expand(p), BOX)) >= 0) break;
        require(found == BOX_X0 + 6, "clamped box column");
        s.expect_pixel(BOX_X0 + 6, 119, 0xff, "border below the clamped box");
    }

    // Execution hold restarts the firmware: centred box, first colour.
    s.reset(100);
    s.frames(3);
    require(s.expect_box(BOX_X0, BOX_Y0) == PALETTE[0], "restarted box colour");
    require(s.capture.bar_violations == 0, "side bars stayed black");
    std::cout << "fes_riscv_system: firmware drew the scene, moved the box, cycled colour on the timer "
                 "interrupt and restarted on hold (" << s.cycles << " pixel cycles, " << s.steps
              << " instructions).\n";
    return 0;
}
