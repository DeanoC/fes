// SPDX-License-Identifier: GPL-2.0-or-later
// Frame-bank video: 50 Hz frames written on the 52.224 MHz system clock and
// shown 3x3 in 720p60 on an unrelated 74.25 MHz clock. Chunks are
// {bitmap, ink, paper} words. Source frame n has paper colour n mod 15 + 1
// everywhere except a geometry mark chunk whose first pixel is paper 0 and
// whose other seven are bright white ink. Every HDMI frame
// must show one whole source frame (no tear) with the mark at its scaled
// position and black side bars, and the shown frame must keep advancing.
#include "Vspectrum_video.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "fes.spectrum video: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

unsigned colour_of(unsigned rgb) {
    const unsigned r = rgb >> 16 & 0xff, g = rgb >> 8 & 0xff, b = rgb & 0xff;
    const unsigned level = r | g | b;
    if (!level) return 0;
    return (level == 0xff ? 8 : 0) | (g ? 4 : 0) | (r ? 2 : 0) | (b ? 1 : 0);
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vspectrum_video dut;
    constexpr uint64_t kSysHalf = 9574, kPixelHalf = 6734;  // ps
    // One 48K frame of 69,888 T states is 1,042,816 system clocks.
    constexpr uint64_t kFrameClocks = 1042816;
    uint64_t next_sys = 0, next_pixel = 0, sys_clocks = 0;
    unsigned source = 0, row = 0, chunk = 0;
    int hdmi_frames = 0, last_shown = -1, distinct = 0;
    unsigned h = 0, v = 0, frame_colour = 0;
    bool frame_mixed = false, last_de = false, last_vs = false, mark_ok = false;
    dut.clk_sys = 0;
    dut.pixel_clk = 0;
    dut.eval();
    while (hdmi_frames < 40) {
        if (next_sys <= next_pixel) {
            next_sys += kSysHalf;
            dut.clk_sys = !dut.clk_sys;
            if (dut.clk_sys) {
                ++sys_clocks;
                // Stream 9,600 words through most of each frame period, one per
                // 100 clocks, leaving a blank gap before the next frame as the ULA does.
                const uint64_t phase = sys_clocks % kFrameClocks;
                dut.pixel_we = 0;
                dut.frame_done = 0;
                if (phase >= 1000 && phase < 1000 + 9600 * 100 && phase % 100 == 0) {
                    const unsigned colour = source % 15 + 1;
                    const bool mark = row == 100 && chunk == 20;
                    dut.pixel_we = 1;
                    dut.pixel_row = row;
                    dut.pixel_chunk = chunk;
                    dut.pixel_data = mark ? 0x7ff0u : colour;
                    dut.frame_done = row == 239 && chunk == 39;
                    if (++chunk == 40) { chunk = 0; if (++row == 240) { row = 0; ++source; } }
                }
            }
        } else {
            next_pixel += kPixelHalf;
            dut.pixel_clk = !dut.pixel_clk;
            if (dut.pixel_clk) {
                if (dut.vsync && !last_vs) {
                    if (hdmi_frames > 2) {
                        if (frame_mixed) fail("HDMI frame " + std::to_string(hdmi_frames) + " mixes source frames");
                        if (!mark_ok) fail("geometry mark not at its 3x3 position");
                        const int shown = int(frame_colour);
                        if (shown != last_shown) ++distinct;
                        last_shown = shown;
                    }
                    ++hdmi_frames;
                    frame_colour = 0;
                    frame_mixed = false;
                    mark_ok = false;
                    v = 0;
                }
                if (dut.de) {
                    if (!last_de) h = 0;
                    const unsigned c = colour_of(dut.red << 16 | dut.green << 8 | dut.blue);
                    const bool picture = h >= 160 && h < 1120;
                    const unsigned sx = (h - 160) / 3, sy = v / 3;
                    if (!picture && c) fail("side bar is not black");
                    if (picture && sy == 100 && sx >= 160 && sx < 168) {
                        // Mark word: pixel 0 colour 0, pixels 1-7 bright white.
                        if (c != (sx == 160 ? 0u : 15u)) mark_ok = false;
                        else if (sx == 167 && v % 3 == 2) mark_ok = true;
                    } else if (picture) {
                        if (!frame_colour) frame_colour = c;
                        else if (c != frame_colour) frame_mixed = true;
                    }
                    ++h;
                } else if (last_de) {
                    ++v;
                }
                last_de = dut.de;
                last_vs = dut.vsync;
            }
        }
        dut.eval();
    }
    // 40 HDMI frames are 0.67 s: about 33 source frames, each shown once or twice.
    if (distinct < 25) fail("only " + std::to_string(distinct) + " distinct frames shown");
    std::cout << "fes.spectrum video: ok (" << distinct << " source frames in " << hdmi_frames
              << " HDMI frames, no tearing)\n";
    return 0;
}
