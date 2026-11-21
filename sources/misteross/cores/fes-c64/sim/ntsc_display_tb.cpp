// SPDX-License-Identifier: MIT
#include "Vc64_vic.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <cstdio>

// VICE data/C64/pepto-ntsc-sony.vpl, in VIC register order.
static constexpr std::array<unsigned, 16> ntsc_rgb = {
    0x000000, 0xffffff, 0x7c352b, 0x5aa6b1, 0x694185, 0x5d8643, 0x212e78, 0xcfbe6f,
    0x894a26, 0x5b3300, 0xaf6459, 0x434343, 0x6b6b6b, 0xa0cb84, 0x5665b3, 0x959595
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vc64_vic vic;
    vic.clk_sys = vic.pixel_clk = vic.phi = vic.cycle_clock = 0;
    vic.cs = vic.we = vic.reading = vic.read_sample = 0;
    vic.color_cs = vic.color_we = vic.color_addr = vic.color_din = 0;
    vic.addr = vic.din = vic.cpu_bus_data = vic.bank = vic.ram_data = 0;
    vic.reset = 1;
    vic.eval();
    vic.clk_sys = 1;
    vic.eval();
    vic.clk_sys = 0;
    vic.reset = 0;

    // Keep the whole frame in the border. Give each row a left/right
    // marker and each native picture a tag that persists across raster 0.
    unsigned phase = 0, cycle = 1, clock = 0, frame = 0;
    uint64_t sys_edge = 0, pixel_edge = 0;
    unsigned pixels = 0, checked_frames = 0, tag = 0, last_tag = 0;
    bool previous_vsync = false, aligned = false;
    for (; pixel_edge < 2ULL * 8 * 1650 * 750;) {
        if (sys_edge * 74250000 <= pixel_edge * 52224000) {
            const bool rising = !(sys_edge++ & 1);
            vic.clk_sys = rising;
            if (rising) {
                vic.phi = phase + 1022730 >= 52224000;
                vic.cycle_clock = clock;
                vic.cs = vic.we = 0;
                if (clock == 0) {
                    if (cycle == 1 && vic.raster == 28) ++frame;
                    if (cycle == 1 || cycle == 37) {
                        vic.addr = 0x20;
                        vic.din = (3 * vic.raster + frame + (cycle == 37 ? 8 : 0)) & 15;
                        vic.cs = vic.we = 1;
                    } else if (cycle == 2) {
                        vic.addr = 0x11;
                        vic.din = 0;
                        vic.cs = vic.we = 1;
                    }
                }
            }
            vic.eval();
            if (rising) {
                phase += 1022730;
                if (vic.phi) {
                    phase -= 52224000;
                    cycle = cycle == 65 ? 1 : cycle + 1;
                    clock = 0;
                } else ++clock;
            }
            continue;
        }
        const bool rising = !(pixel_edge++ & 1);
        vic.pixel_clk = rising;
        vic.eval();
        if (!rising) continue;
        if (vic.vsync && !previous_vsync) {
            if (aligned && pixels != 1280 * 720) return 1;
            if (aligned && tag) {
                if (tag < last_tag || tag > last_tag + 1) {
                    std::fprintf(stderr, "HDMI skipped or reordered pictures %u -> %u\n", last_tag, tag);
                    return 1;
                }
                last_tag = tag;
                ++checked_frames;
            }
            aligned = true;
            pixels = tag = 0;
        }
        previous_vsync = vic.vsync;
        if (!aligned || !vic.de) continue;
        const unsigned x = pixels % 1280, y = pixels / 1280;
        ++pixels;
        const unsigned rgb = (vic.red << 16) | (vic.green << 8) | vic.blue;
        if (x == 160 && y == 0) {
            // A reset page is all black. Visible pictures 1..8 have
            // nonblack first pixels with a distinct color for each tag.
            for (unsigned n = 1; n <= frame; ++n)
                if (rgb == ntsc_rgb[(28 * 3 + n) & 15]) tag = n;
        }
        unsigned expected = 0;
        if (tag && x >= 160 && x < 1120) {
            const unsigned raster = (28 + y * 247 / 720) % 263;
            const unsigned dot = (x - 160) * 384 / 960;
            // D020 written at the start of cycle 37 reaches the color
            // resolver three dots after source selection.
            const unsigned half = dot >= (37 - 13) * 8 - 3 ? 8 : 0;
            expected = ntsc_rgb[(3 * raster + tag + half) & 15];
        }
        if (rgb != expected) {
            std::fprintf(stderr, "HDMI (%u,%u) picture %u: got %06x expected %06x\n",
                         x, y, tag, rgb, expected);
            return 1;
        }
    }
    if (checked_frames < 4 || last_tag < 4) {
        std::fprintf(stderr, "Only checked %u HDMI frames through native picture %u\n", checked_frames, last_tag);
        return 1;
    }
    std::printf("NTSC HDMI: %u complete 720p pictures, 384x247 wrapped rasters and 16 colors passed\n",
                checked_frames);
}
