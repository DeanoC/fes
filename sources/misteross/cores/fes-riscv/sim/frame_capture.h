// SPDX-License-Identifier: GPL-2.0-or-later
// Rebuilds the 160x120 framebuffer view from the shell's HDMI pixel stream:
// each framebuffer pixel is sampled once at the centre of its 6x6 block.
#pragma once
#include <array>
#include <cstdint>

struct FrameCapture {
    static constexpr int WIDTH = 160, HEIGHT = 120;
    std::array<uint32_t, WIDTH * HEIGHT> fb{};
    int x = 0, y = 0;
    bool prev_de = false, prev_vs = false;
    unsigned frames = 0;             // complete frames seen so far
    unsigned bar_violations = 0;     // non-black pixels in the side bars
    unsigned active_pixels = 0;

    void sample(bool de, bool vs, uint32_t rgb) {
        if (vs && !prev_vs) {
            if (y == 720) ++frames;
            y = 0;
        }
        if (de) {
            if (!prev_de) x = 0;
            if (x < 160 || x >= 1120) {
                if (rgb) ++bar_violations;
            } else if ((x - 160) % 6 == 3 && y % 6 == 3 && y / 6 < HEIGHT) {
                fb[(y / 6) * WIDTH + (x - 160) / 6] = rgb;
            }
            ++active_pixels;
            ++x;
        } else if (prev_de) {
            ++y;
        }
        prev_de = de;
        prev_vs = vs;
    }

    uint32_t at(int fx, int fy) const { return fb[fy * WIDTH + fx]; }

    // RGB332 to the shell's RGB888 expansion.
    static uint32_t expand(uint8_t p) {
        uint32_t r = p >> 5, g = (p >> 2) & 7, b = p & 3;
        uint32_t r8 = (r << 5) | (r << 2) | (r >> 1);
        uint32_t g8 = (g << 5) | (g << 2) | (g >> 1);
        uint32_t b8 = (b << 6) | (b << 4) | (b << 2) | b;
        return (r8 << 16) | (g8 << 8) | b8;
    }
    static uint8_t background(int fx, int fy) { return uint8_t(((fx >> 5) << 5) | ((fy >> 4) << 2) | 1); }

    // Leftmost x of a horizontal run of `length` pixels of `colour` in row fy, or -1.
    int find_run(int fy, uint32_t colour, int length) const {
        int run = 0;
        for (int fx = 0; fx < WIDTH; ++fx) {
            run = at(fx, fy) == colour ? run + 1 : 0;
            if (run == length) return fx - length + 1;
        }
        return -1;
    }
};
