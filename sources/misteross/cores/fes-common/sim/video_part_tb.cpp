// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vvideo_part_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

static constexpr uint32_t DE = 1u << 24, HS = 1u << 25, VS = 1u << 26;
static constexpr uint32_t CE = 1u << 27, SOF = 1u << 28, EOL = 1u << 29;
static constexpr uint32_t HOLD = 1u << 30, RESERVED = 1u << 31;

class Simulation {
    Vvideo_part_top dut;
    uint32_t registered_request = 0;
    bool odd_line = false;
    uint64_t cycles = 0;
public:
    void step(uint32_t request) {
        const uint32_t previous = registered_request;
        const bool valid = (previous & CE) && !(previous & RESERVED);
        const bool sof = previous & SOF;
        const bool eol = previous & EOL;
        const uint32_t timing = valid ? previous & (DE | HS | VS | CE) : 0;
        const uint32_t picture = valid && (previous & DE) && !(previous & HOLD)
            ? previous & 0xffffff : 0;
        // Halve each channel independently, including odd channel values.
        const uint32_t shade = odd_line && !sof
            ? ((picture & 0xfefefe) >> 1) : picture;
        dut.clock = 0;
        dut.source_request = request;
        dut.eval();
        dut.clock = 1;
        dut.eval();
        if (dut.direct_response != (timing | picture) ||
            dut.scanlines_response != (timing | shade)) {
            std::cerr << "video part mismatch at cycle " << cycles
                      << " request=0x" << std::hex << previous
                      << " direct=0x" << dut.direct_response
                      << " want=0x" << (timing | picture)
                      << " scanlines=0x" << dut.scanlines_response
                      << " want=0x" << (timing | shade) << std::dec << '\n';
            std::exit(1);
        }
        if (valid) {
            if (sof) odd_line = eol;
            else if (eol) odd_line = !odd_line;
        }
        registered_request = request;
        ++cycles;
    }

    void finish() {
        step(0);
        step(0);
        std::cout << "video parts: " << cycles << " cycles passed\n";
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Simulation sim;
    // One-pixel full raster, invalid markers and a restart halfway through a
    // line exercise ordering that an even-height 720p stream alone cannot.
    sim.step(CE | DE | SOF | EOL | 0xff0183);
    sim.step(CE | DE | 0x030507);
    sim.step(SOF | EOL | DE | 0xffffff); // CE disabled: no parity transition
    sim.step(CE | DE | 0x030507);
    sim.step(CE | DE | EOL | HOLD | 0xffffff);
    sim.step(CE | DE | 0x030507); // HOLD did not lose the line boundary
    sim.step(CE | DE | EOL | RESERVED | 0xffffff); // invalid word: no transition
    sim.step(CE | DE | 0x030507);
    sim.step(CE | DE | SOF | 0xffffff); // SOF resets its own pixel to bright
    sim.step(CE | HS | VS | 0xffffff); // blanking blacks RGB, preserves sync

    uint64_t active_pixels = 0, horizontal_sync_pixels = 0, vertical_sync_pixels = 0;
    for (unsigned frame = 0; frame < 2; ++frame) {
        for (unsigned y = 0; y < 750; ++y) {
            for (unsigned x = 0; x < 1650; ++x) {
                const uint32_t rgb = (((x * 11 + y * 7 + frame * 3) & 255) << 16) |
                    (((x * 3 + y * 13 + frame * 5) & 255) << 8) |
                    ((x * 7 + y * 5 + frame * 11) & 255);
                uint32_t request = CE | rgb;
                if (x < 1280 && y < 720) { request |= DE; ++active_pixels; }
                if (x >= 1390 && x < 1430) { request |= HS; ++horizontal_sync_pixels; }
                if (y >= 725 && y < 730) { request |= VS; ++vertical_sync_pixels; }
                if (x == 0 && y == 0) request |= SOF;
                if (x == 1649) request |= EOL;
                if (frame == 1 && y >= 237 && y < 240 && x >= 713) request |= HOLD;
                sim.step(request);
            }
        }
    }
    if (active_pixels != 2 * 1280 * 720 || horizontal_sync_pixels != 2 * 40 * 750 ||
        vertical_sync_pixels != 2 * 5 * 1650) return 1;

    // Clock-enable gaps at SOF/EOL model a slower raster in an always-running
    // source clock. Neither disabled marker nor invalid word may alter parity.
    for (unsigned y = 0; y < 9; ++y) {
        for (unsigned x = 0; x < 7; ++x) {
            uint32_t markers = (x == 0 && y == 0 ? SOF : 0) | (x == 6 ? EOL : 0);
            sim.step(markers | DE | 0xffffff);
            sim.step(CE | markers | DE | 0xb153ff);
            sim.step(markers | DE | 0xffffff);
        }
    }
    sim.finish();
    return 0;
}
