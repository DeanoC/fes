// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_splash_core.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "fes-splash video: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool ok, const std::string &message) {
    if (!ok) fail(message);
}

void write_ppm(const char *path, const uint32_t *pixels) {
    std::ofstream out(path, std::ios::binary);
    require(bool(out), std::string("cannot write ") + path);
    out << "P6\n1280 720\n255\n";
    for (unsigned i = 0; i < 1280 * 720; ++i) {
        const uint32_t rgb = pixels[i];
        const char bytes[3] = {char((rgb >> 16) & 0xff), char((rgb >> 8) & 0xff),
                               char(rgb & 0xff)};
        out.write(bytes, 3);
    }
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_splash_core d;
    d.pixel_clk = 0;
    d.eval();

    const char *frame0_path = argc > 1 ? argv[1] : nullptr;
    const char *frame1_path = argc > 2 ? argv[2] : nullptr;
    static uint32_t frame0[1280 * 720];
    static uint32_t frame1[1280 * 720];
    uint64_t hashes[3] = {};

    for (unsigned frame = 0; frame < 3; ++frame) {
        unsigned active = 0, hs = 0, vs = 0, lit = 0;
        unsigned letters = 0, ring = 0, disc = 0, motes = 0;
        for (unsigned y = 0; y < 750; ++y) {
            for (unsigned x = 0; x < 1650; ++x) {
                require(bool(d.hdmi_de) == (x < 1280 && y < 720), "data enable timing");
                require(bool(d.hdmi_hs) == (x >= 1390 && x < 1430), "hsync timing");
                require(bool(d.hdmi_vs) == (y >= 725 && y < 730), "vsync timing");
                if (x >= 1280 || y >= 720) require(d.hdmi_rgb == 0, "blanking is not black");
                active += d.hdmi_de;
                hs += d.hdmi_hs;
                vs += d.hdmi_vs;
                lit += d.hdmi_rgb != 0;
                hashes[frame] = hashes[frame] * 33 + d.hdmi_rgb;
                if (x < 1280 && y < 720) {
                    if (frame == 0) frame0[y * 1280 + x] = d.hdmi_rgb;
                    if (frame == 1) frame1[y * 1280 + x] = d.hdmi_rgb;
                }
                if (d.hdmi_rgb == 0xECF4F8u) ++letters;
                if (d.hdmi_rgb == 0x40BAD2u) ++ring;
                if (d.hdmi_rgb == 0x143048u) ++disc;
                if (d.hdmi_rgb == 0xFFFFFFu) ++motes;
                d.pixel_clk = 1;
                d.eval();
                d.pixel_clk = 0;
                d.eval();
            }
        }
        require(active == 1280 * 720 && hs == 40 * 750 && vs == 5 * 1650, "frame dimensions");
        require(lit > 0, "active image is black");
        require(letters > 1000, "FES letters are missing");
        require(ring > 1000, "fog ring is missing");
        require(disc > 1000, "fog disc is missing");
        require(motes > 0, "orbiting mote is missing");
        if (frame == 0) {
            require(frame0[318 * 1280 + 620] == 0xECF4F8u, "F glyph origin");
            require(frame0[360 * 1280 + 540] == 0x143048u, "disc sample");
            require(frame0[360 * 1280 + 570] == 0x40BAD2u, "ring sample");
            require(frame0[360 * 1280 + 592] == 0xFFFFFFu, "phase-0 mote");
            require(d.phase == 1, "phase did not advance after frame 0");
        }
    }
    require(hashes[0] != hashes[1] && hashes[1] != hashes[2],
            "autonomous splash motion did not advance");
    if (frame0_path) write_ppm(frame0_path, frame0);
    if (frame1_path) write_ppm(frame1_path, frame1);
    std::cout << "splash video passed 720p timing, FES mark and autonomous motion\n";
}
