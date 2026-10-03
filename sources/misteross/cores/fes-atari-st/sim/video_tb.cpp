// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vst_video_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>

static constexpr uint32_t DE = 1u << 24, HS = 1u << 25, VS = 1u << 26;
static constexpr uint32_t CE = 1u << 27, SOF = 1u << 28, EOL = 1u << 29;
static constexpr uint32_t HOLD = 1u << 30;
static constexpr unsigned Width = 1650, Height = 750, Frame = Width * Height;

class Simulation {
    Vst_video_sim_top dut;
    std::array<uint16_t, 512 * 1024 / 2> ram{};
    std::array<uint16_t, 16> colors{};
    uint32_t registered_request = 0;
    bool odd_line = false;
    unsigned position = 0;
    uint64_t cycles = 0;

    [[noreturn]] void fail(const char *what, uint32_t got, uint32_t expected) const {
        std::cerr << what << " at mode " << unsigned(dut.resolution)
                  << " pixel " << position % Width << ',' << position / Width
                  << " cycle " << cycles << ": got 0x" << std::hex << got
                  << " expected 0x" << expected << std::dec << '\n';
        std::exit(1);
    }

    static unsigned pixel(unsigned x, unsigned y, unsigned planes) {
        // Native image varies every pixel and crosses word/line boundaries.
        return (x ^ (x / 7) ^ (y * 11) ^ (y / 5)) & ((1u << planes) - 1);
    }

    static unsigned expand(unsigned channel) {
        return (channel << 5) | (channel << 2) | (channel >> 1);
    }

    static uint32_t rgb(uint16_t color) {
        return (expand((color >> 6) & 7) << 16) |
               (expand((color >> 3) & 7) << 8) | expand(color & 7);
    }

    uint32_t expected_source() const {
        const unsigned x = position % Width, y = position / Width;
        uint32_t request = CE;
        if (x < 1280 && y < 720) request |= DE;
        if (x >= 1390 && x < 1430) request |= HS;
        if (y >= 725 && y < 730) request |= VS;
        if (position == 0) request |= SOF;
        if (x == 1649) request |= EOL;
        if (dut.hold || dut.reset) request |= HOLD;
        if (!(request & DE) || dut.resolution == 3) return request;

        const bool high = dut.resolution == 2;
        const unsigned top = high ? 160 : 60, height = high ? 400 : 600;
        if (y < top || y >= top + height) return request | (high ? 0 : rgb(colors[0]));
        const unsigned native_x = x / (dut.resolution == 0 ? 4 : 2);
        const unsigned native_y = (y - top) / (high ? 1 : 3);
        const unsigned planes = dut.resolution == 0 ? 4 : high ? 1 : 2;
        const unsigned word = ((dut.screen_base & 0xffff00) / 2) +
            native_y * (high ? 40 : 80) + (native_x / 16) * planes;
        if (word + planes > ram.size()) return request;
        const unsigned index = pixel(native_x, native_y, planes);
        return request | (high ? ((index ^ (colors[0] & 1)) ? 0xffffff : 0) : rgb(colors[index]));
    }

    void step(bool verify_source = true) {
        dut.clk = 0;
        dut.eval();
        dut.mem_data = ram[dut.mem_addr];
        dut.eval();
        const uint32_t current = dut.source_request;
        if (verify_source && current != expected_source())
            fail("source raster/pixel mismatch", current, expected_source());

        // The response at this edge belongs to the request captured on the
        // previous edge: both boundaries carry the entire timed raster word.
        const bool valid = registered_request & CE;
        const uint32_t timing = valid ? registered_request & (DE | HS | VS | CE) : 0;
        const uint32_t picture = valid && (registered_request & DE) && !(registered_request & HOLD)
            ? registered_request & 0xffffff : 0;
        const uint32_t shade = odd_line && !(registered_request & SOF)
            ? ((picture & 0xfefefe) >> 1) : picture;
        dut.clk = 1;
        dut.eval();
        if (dut.direct_response != (timing | picture))
            fail("direct part/boundary mismatch", dut.direct_response, timing | picture);
        if (dut.scanlines_response != (timing | shade))
            fail("scanlines part/boundary mismatch", dut.scanlines_response, timing | shade);
        if (valid) {
            if (registered_request & SOF) odd_line = registered_request & EOL;
            else if (registered_request & EOL) odd_line = !odd_line;
        }
        registered_request = current;
        position = dut.reset ? 0 : (position + 1) % Frame;
        ++cycles;
    }

public:
    Simulation() {
        dut.reset = 0;
        dut.hold = 0;
        dut.screen_base = 0x02012f; // Low byte must be ignored by the ST base registers.
        dut.resolution = 0;
        for (unsigned i = 0; i < 16; ++i)
            colors[i] = (((i + 3) & 7) << 6) | (((i * 5 + 2) & 7) << 3) | ((i * 3) & 7);
        apply_palette();
    }

    void apply_palette() {
        for (unsigned i = 0; i < 5; ++i) dut.palette[i] = 0;
        for (unsigned entry = 0; entry < 16; ++entry)
            for (unsigned bit = 0; bit < 9; ++bit)
                if (colors[entry] & (1u << bit)) {
                    const unsigned packed_bit = entry * 9 + bit;
                    dut.palette[packed_bit / 32] |= 1u << (packed_bit % 32);
                }
    }

    void frame(unsigned mode, uint32_t base, bool invert = false) {
        dut.reset = 1;
        step(false);
        dut.resolution = mode;
        dut.screen_base = base;
        colors[0] = (colors[0] & ~1u) | unsigned(invert);
        apply_palette();
        ram.fill(0xa55a); // Invalid reads must not alias a conveniently black word.
        const bool high = mode == 2;
        const unsigned width = mode == 0 ? 320 : 640, height = high ? 400 : 200;
        const unsigned planes = mode == 0 ? 4 : high ? 1 : 2;
        for (unsigned y = 0; y < height; ++y)
            for (unsigned group = 0; group < width / 16; ++group)
                for (unsigned plane = 0; plane < planes; ++plane) {
                    uint16_t word = 0;
                    for (unsigned bit = 0; bit < 16; ++bit)
                        if (pixel(group * 16 + bit, y, planes) & (1u << plane))
                            word |= 1u << (15 - bit);
                    const unsigned address = ((base & 0xffff00) / 2) +
                        (y * (width / 16) + group) * planes + plane;
                    if (address < ram.size()) ram[address] = word;
                }
        dut.reset = 0;

        unsigned active = 0, hs = 0, vs = 0, sof = 0, eol = 0;
        for (unsigned i = 0; i < Frame; ++i) {
            const unsigned x = i % Width, y = i / Width;
            // Assert and release in the picture, at EOL, and across SOF.
            // No raster/fetch pause is allowed and odd-line phase must survive.
            dut.hold = (y == 0 && x < 5) || (y >= 123 && y <= 125 && x >= 713) ||
                (y == 126 && x < 51) || (y == 749 && x >= 1647);
            step();
            const uint32_t request = registered_request;
            active += bool(request & DE);
            hs += bool(request & HS);
            vs += bool(request & VS);
            sof += bool(request & SOF);
            eol += bool(request & EOL);
        }
        if (active != 1280 * 720 || hs != 40 * 750 || vs != 5 * 1650 || sof != 1 || eol != 750)
            fail("frame timing totals", active, 1280 * 720);
        dut.hold = 0;
        std::cout << "ST video mode " << mode << ", base 0x" << std::hex << base
                  << std::dec << ", mono invert " << invert << ": full frame passed\n";
    }

    void finish() {
        // Complete outstanding socket responses and check raster wrap/SOF.
        step();
        step();
        std::cout << "ST video: " << cycles << " cycles passed\n";
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Simulation sim;
    sim.frame(0, 0x02012f);
    sim.frame(1, 0x030000);
    sim.frame(2, 0x040000);
    sim.frame(2, 0x040000, true);
    sim.frame(0, 0x07ff00); // Last RAM page: guard later groups/lines.
    sim.frame(1, 0xffff00); // No truncation or wrap into physical RAM.
    sim.frame(3, 0x020000); // Unsupported shift mode still carries valid timing.
    sim.finish();
    return 0;
}
