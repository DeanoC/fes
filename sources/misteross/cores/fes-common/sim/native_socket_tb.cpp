// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vnative_socket_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <iostream>
#include <stdexcept>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vnative_socket_top dut;
    constexpr unsigned frame = 1650 * 750;
    constexpr std::array<uint32_t, 16> palette = {
        0, 0, 0x21c842, 0x5edc78, 0x5455ed, 0x7d76fc, 0xd4524d, 0x42ebf5,
        0xfc5554, 0xff7978, 0xd4c154, 0xe6ce80, 0x21b03b, 0xc95bba, 0xcccccc, 0xffffff
    };
    for (unsigned edge = 0; edge < frame * 3; ++edge) {
        uint32_t request = 0;
        if (edge == 0) request = 0x21000000; // release HOLD
        if (edge >= 16 && edge < 16 + 256 * 192) {
            const unsigned n = edge - 16, x = n % 256, y = n / 256;
            request = 0x01000000 | ((x + y * 3) & 15) |
                      (n == 0 ? 1u << 25 : 0) | (x == 255 ? 1u << 26 : 0) |
                      (n == 256 * 192 - 1 ? 1u << 27 : 0);
        }
        dut.clock = 0; dut.request = request; dut.eval();
        dut.clock = 1; dut.eval();
        // Before the consumer's first registered raster sample, CE already
        // advertises its continuous pixel clock and all other output is black.
        uint32_t expected = 1u << 27;
        if (edge != 0) {
            // The consumer's registered scanout is followed by the actual
            // shell response FF. Source request FF latency must still allow
            // the complete first bank to appear at the next output SOF.
            const unsigned output_edge = edge - 1;
            const unsigned x = output_edge % 1650, y = output_edge / 1650 % 750;
            uint32_t rgb = 0;
            if (output_edge >= frame && x >= 384 && x < 896 && y >= 168 && y < 552) {
                rgb = palette[((x - 384) / 2 + 3 * ((y - 168) / 2)) & 15];
#ifdef FES_VIDEO_SCANLINES
                if (y & 1) rgb = (rgb & 0xfefefe) >> 1;
#endif
            }
            expected = rgb | (1u << 27) | (x < 1280 && y < 720 ? 1u << 24 : 0) |
                       (x >= 1390 && x < 1430 ? 1u << 25 : 0) |
                       (y >= 725 && y < 730 ? 1u << 26 : 0);
        }
        if (dut.response != expected) {
            std::cerr << "native socket edge " << edge << ": got " << std::hex
                      << dut.response << " expected " << expected << '\n';
            throw std::runtime_error("native socket/cart full-raster mismatch");
        }
    }
    std::cout << "native socket/cart: " << frame * 3 << " exact HDMI cycles passed\n";
}
