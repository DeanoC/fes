// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vpong_video.h"
#include <cstdlib>
#include <iostream>

static void require(bool condition, const char* message) {
    if (!condition) { std::cerr << message << '\n'; std::exit(1); }
}

int main() {
    Vpong_video video;
    unsigned pixels = 0, active = 0, frames = 0, hsync = 0, vsync = 0;
    unsigned previous_x = 0, previous_y = 0;
    bool first = true;
    for (unsigned cycle = 0; cycle < 3 * 424 * 262 * 2; ++cycle) {
        video.clk = 0; video.eval();
        if (video.ce_pixel) {
            if (!first) {
                require(video.x == (previous_x == 423 ? 0 : previous_x + 1), "pixel sequence");
                require(video.y == (previous_x == 423 ? (previous_y == 261 ? 0 : previous_y + 1) : previous_y), "line sequence");
            }
            first = false; previous_x = video.x; previous_y = video.y;
            ++pixels;
            if (video.active) ++active;
            if (video.hsync) ++hsync;
            if (video.vsync) ++vsync;
            if (video.frame_tick) ++frames;
            require(bool(video.active) == (video.x < 320 && video.y < 240), "active area");
        } else require(!video.frame_tick, "frame tick must be one system clock");
        video.clk = 1; video.eval();
    }
    require(pixels == 2 * 424 * 262, "pixel divider");
    require(active == 2 * 320 * 240, "visible pixel count");
    require(hsync == 2 * 32 * 262, "horizontal pulse width");
    require(vsync == 2 * 3 * 424, "vertical pulse width");
    require(frames == 2, "one tick per frame");
    std::cout << "Pong raster: two complete frames passed\n";
}
