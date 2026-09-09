// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_pong_core.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string &message, uint64_t cycle) {
    std::cerr << "FES 720p: " << message << " at cycle " << cycle << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message, uint64_t cycle) {
    if (!condition) fail(message, cycle);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_pong_core core;
    core.pixel_clk = 0;
    core.game_reset = 1;
    core.buttons = 0;
    core.eval();

    constexpr uint32_t kHTotal = 1650;
    constexpr uint32_t kVTotal = 750;
    constexpr uint64_t kFramePixels = uint64_t(kHTotal) * kVTotal;
    uint64_t active_count = 0;
    uint64_t hsync_count = 0;
    uint64_t vsync_count = 0;
    uint64_t frame_ticks = 0;

    for (uint64_t cycle = 0; cycle != 2 * kFramePixels; ++cycle) {
        const uint32_t x = cycle % kHTotal;
        const uint32_t y = (cycle / kHTotal) % kVTotal;
        const bool active = x < 1280 && y < 720;
        const bool hsync = x >= 1390 && x < 1430;
        const bool vsync = y >= 725 && y < 730;
        const bool frame_tick = x == 1649 && y == 749;

        core.pixel_clk = 0;
        core.eval();
        require(bool(core.hdmi_de) == active, "data-enable timing", cycle);
        require(bool(core.hdmi_hs) == hsync, "positive horizontal sync timing", cycle);
        require(bool(core.hdmi_vs) == vsync, "positive vertical sync timing", cycle);
        require(bool(core.frame_tick) == frame_tick, "one frame tick position", cycle);
        if (active) ++active_count;
        if (hsync) ++hsync_count;
        if (vsync) ++vsync_count;
        if (frame_tick) ++frame_ticks;

        if (active && (x < 160 || x >= 1120)) {
            require(core.hdmi_rgb == 0, "160-pixel side bar is not black", cycle);
            require(!core.playfield_active, "side bar marked as playfield", cycle);
        }
        if (active && x >= 160 && x < 1120) {
            require(core.playfield_active, "center image missing playfield enable", cycle);
            require(core.playfield_x == (x - 160) / 3,
                    "horizontal 3x playfield mapping", cycle);
            require(core.playfield_y == y / 3,
                    "vertical 3x playfield mapping", cycle);
        }
        if (x >= 196 && x < 208 && y >= 312 && y < 408) {
            require(core.hdmi_rgb == 0x00ffffffu,
                    "reset player paddle is not scaled 3x", cycle);
        }

        core.pixel_clk = 1;
        core.eval();
    }

    require(active_count == 2ull * 1280 * 720,
            "two-frame active pixel count", 2 * kFramePixels);
    require(hsync_count == 2ull * 40 * 750,
            "40-clock horizontal sync width", 2 * kFramePixels);
    require(vsync_count == 2ull * 5 * 1650,
            "five-line vertical sync width", 2 * kFramePixels);
    require(frame_ticks == 2, "one tick per complete frame", 2 * kFramePixels);
    require(core.game_playing == 0,
            "gameplay advanced while reset was held", 2 * kFramePixels);

    std::cout << "FES 720p: two continuous reset frames, exact raster/sync, bars and 3x Pong mapping passed\n";
    return EXIT_SUCCESS;
}
