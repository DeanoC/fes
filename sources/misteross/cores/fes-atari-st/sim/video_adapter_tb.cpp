// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vst_video_adapter_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>

static constexpr unsigned Width = 1650, Height = 750, Frame = Width * Height;
static constexpr uint32_t DE = 1u << 24, HS = 1u << 25, VS = 1u << 26;
static constexpr uint32_t CE = 1u << 27, SOF = 1u << 28, EOL = 1u << 29, HOLD = 1u << 30;

struct Config {
    uint32_t base;
    unsigned mode, seed;
    std::array<uint16_t, 16> colors{};
};

class Simulation {
    Vst_video_adapter_sim_top dut;
    std::array<uint16_t, 512 * 1024 / 2> ram{};
    std::array<Config, 6> configurations{};
    uint64_t next_sys = 0, next_pixel = 9871, sys_cycles = 0, pixel_cycles = 0;
    unsigned position = 0, frame = 0;
    uint32_t registered_request = 0;
    bool odd_line = false, hold_meta = false, hold_sync = false;
    bool transfer_active = false, transfer_complete = false, forced_stall = false, crossing_stall = false;
    uint32_t transfer_addr = 0;
    unsigned remaining = 0, transfers = 0, stalled_picture_lines = 0, recovered_picture_lines = 0;
    unsigned crossing_black_lines = 0, crossing_recovered_lines = 0;
    bool line_muted = false;

    [[noreturn]] void fail(const char *what) const {
        std::cerr << "ST video adapter: " << what << " at frame " << frame
                  << " pixel " << position % Width << ',' << position / Width
                  << " sys/pixel cycles " << sys_cycles << '/' << pixel_cycles << '\n';
        std::exit(1);
    }

    static unsigned pixel(unsigned x, unsigned y, unsigned planes, unsigned seed) {
        return (x ^ (x / 7) ^ (y * 11) ^ (y / 5) ^ (seed * 13)) & ((1u << planes) - 1);
    }

    static unsigned expand(unsigned channel) {
        return (channel << 5) | (channel << 2) | (channel >> 1);
    }

    static uint32_t rgb(uint16_t color) {
        return (expand((color >> 6) & 7) << 16) |
               (expand((color >> 3) & 7) << 8) | expand(color & 7);
    }

    void populate(const Config& config) {
        if (config.mode == 3) return;
        const unsigned planes = config.mode == 0 ? 4 : config.mode == 1 ? 2 : 1;
        const unsigned width = config.mode == 0 ? 320 : 640;
        const unsigned height = config.mode == 2 ? 400 : 200;
        for (unsigned y = 0; y < height; ++y)
            for (unsigned group = 0; group < width / 16; ++group)
                for (unsigned plane = 0; plane < planes; ++plane) {
                    uint16_t word = 0;
                    for (unsigned bit = 0; bit < 16; ++bit)
                        if (pixel(group * 16 + bit, y, planes, config.seed) & (1u << plane))
                            word |= 1u << (15 - bit);
                    const unsigned address = config.base / 2 +
                        (y * (width / 16) + group) * planes + plane;
                    if (address < ram.size()) ram[address] = word;
                }
    }

    void configure(unsigned index) {
        const auto& config = configurations[index];
        dut.screen_base = config.base | 0x2f; // The ST ignores this low byte.
        dut.resolution = config.mode;
        for (unsigned i = 0; i < 5; ++i) dut.palette[i] = 0;
        for (unsigned entry = 0; entry < 16; ++entry)
            for (unsigned bit = 0; bit < 9; ++bit)
                if (config.colors[entry] & (1u << bit)) {
                    const unsigned packed = entry * 9 + bit;
                    dut.palette[packed / 32] |= 1u << (packed % 32);
                }
        dut.eval();
    }

    const Config& active_config() const {
        const unsigned index = frame == 1 ? 0 : frame == 2 ? 1 : frame == 3 ? 2 :
            frame == 4 ? 3 : frame == 5 ? 1 : frame == 6 ? 4 : frame == 7 ? 5 : 0;
        return configurations[index];
    }

    void check_config() {
        if (!frame) return;
        const auto& config = active_config();
        if (dut.active_base != config.base || dut.active_resolution != config.mode)
            fail("base/resolution snapshot changed outside its frame boundary");
        for (unsigned entry = 0; entry < 16; ++entry) {
            const unsigned offset = entry * 9;
            const uint64_t packed = uint64_t(dut.active_palette[offset / 32]) |
                (offset / 32 < 4 ? uint64_t(dut.active_palette[offset / 32 + 1]) << 32 : 0);
            if (((packed >> (offset % 32)) & 0x1ff) != config.colors[entry])
                fail("palette did not cross coherently with base/resolution");
        }
    }

    void memory_edge() {
        if (dut.reset_sys) {
            transfer_active = transfer_complete = false;
            dut.video_ready = 0;
        } else if (!dut.video_req) {
            if (transfer_active && !transfer_complete) fail("memory request abandoned before ready");
            transfer_active = transfer_complete = false;
            dut.video_ready = 0;
        } else {
            const uint32_t address = dut.video_addr;
            if (address >= ram.size()) fail("memory address escaped 512 KiB RAM");
            if (!transfer_active) {
                transfer_active = true;
                transfer_addr = address;
                // Fourteen-clock reads, occasional arbitration and refresh stalls.
                remaining = 12 + (address % 43 == 0 ? 60 : 0) + (transfers % 173 == 0 ? 40 : 0);
                const auto& config = configurations[3];
                if (frame == 4 && !forced_stall && address == config.base / 2 + 100 * 40) {
                    remaining += 20000;
                    forced_stall = true;
                }
                if (frame == 4 && !crossing_stall && address == config.base / 2 + 399 * 40) {
                    // Keep a previous frame's high-resolution bank fill in flight
                    // across the coherent switch to medium resolution at a new base.
                    remaining += 400000;
                    crossing_stall = true;
                }
                ++transfers;
                dut.video_ready = 0;
            } else {
                if (address != transfer_addr) fail("memory address changed before ready/rearm");
                if (remaining) {
                    --remaining;
                    dut.video_ready = 0;
                } else if (!transfer_complete) {
                    transfer_complete = true;
                    dut.video_rdata = ram[address];
                    dut.video_ready = 1;
                } else fail("memory request remained asserted after completion");
            }
        }
        dut.clk_sys = 1;
        dut.eval();
        dut.clk_sys = 0;
        dut.eval();
        ++sys_cycles;
    }

    void check_source(uint32_t request) {
        const unsigned x = position % Width, y = position / Width;
        uint32_t timing = CE;
        if (x < 1280 && y < 720) timing |= DE;
        if (x >= 1390 && x < 1430) timing |= HS;
        if (y >= 725 && y < 730) timing |= VS;
        if (!position) timing |= SOF;
        if (x == 1649) timing |= EOL;
        if ((request & ~(0xffffff | HOLD)) != timing) fail("raster timing/markers changed");
        if (dut.debug_frame != frame) fail("frame counter lost raster alignment");
        check_config();
        if (!frame) {
            if (!(request & HOLD) || (request & 0xffffff)) fail("unconfigured initial frame was visible");
            return;
        }
        const auto& config = active_config();
        const bool high = config.mode == 2;
        const unsigned top = high ? 160 : 60, height = high ? 400 : 600;
        const bool picture = (timing & DE) && config.mode != 3 && y >= top && y < top + height;
        const unsigned native_y = y >= top ? (y - top) / (high ? 1 : 3) : 0;
        const unsigned stride = high ? 40 : 80;
        const bool valid_line = config.base / 2 + (native_y + 1) * stride <= ram.size();
        const bool mute = request & HOLD;
        if (x == 0) line_muted = mute;
        const bool external_hold_line = frame == 2 && y >= 123 && y <= 125;
        if (picture && x < 1280 && mute != line_muted && !external_hold_line)
            fail("late cache completion exposed only part of a line");
        if (hold_sync && !mute) fail("synchronized HOLD did not mute");
        if (!hold_sync && picture && frame != 4 && frame != 5 && valid_line && mute)
            fail("unexpected line underflow at supported memory latency");
        if (!hold_sync && picture && !valid_line && !mute)
            fail("invalid line base was truncated into RAM");
        if (picture && x == 0 && frame == 4) {
            if (mute) ++stalled_picture_lines;
            else if (y > top + 140) ++recovered_picture_lines;
        }
        if (picture && x == 0 && frame == 5) {
            if (mute) ++crossing_black_lines;
            else if (y > 250) ++crossing_recovered_lines;
        }

        uint32_t expected = 0;
        if (!mute && (timing & DE) && config.mode != 3) {
            if (!picture) expected = high ? 0 : rgb(config.colors[0]);
            else {
                const unsigned native_x = x / (config.mode == 0 ? 4 : 2);
                const unsigned planes = config.mode == 0 ? 4 : config.mode == 1 ? 2 : 1;
                const unsigned index = pixel(native_x, native_y, planes, config.seed);
                expected = high ? ((index ^ (config.colors[0] & 1)) ? 0xffffff : 0) : rgb(config.colors[index]);
            }
        }
        if ((request & 0xffffff) != expected) fail("pixel/bitplane/palette/cache ownership mismatch");
    }

    void pixel_edge() {
        if (!dut.reset_pixel) {
            // Change the complete source configuration during an active frame.
            // The current frame must retain every field of its old snapshot.
            if (position == 100 * Width + 13) {
                if (frame == 1) configure(1);
                if (frame == 2) configure(2);
                if (frame == 3) configure(3);
                if (frame == 4) configure(1);
                if (frame == 5) configure(4);
                if (frame == 6) configure(5);
                if (frame == 7) configure(0);
            }
            if (frame == 2) {
                if (position == 123 * Width + 700) dut.hold = 1;
                if (position == 125 * Width + 713) dut.hold = 0;
                dut.eval();
            }
        }
        const uint32_t current = dut.video_request;
        if (!dut.reset_pixel) check_source(current);
        const bool valid = registered_request & CE;
        const uint32_t timing = valid ? registered_request & (DE | HS | VS | CE) : 0;
        const uint32_t picture = valid && (registered_request & DE) && !(registered_request & HOLD)
            ? registered_request & 0xffffff : 0;
        const uint32_t shade = odd_line && !(registered_request & SOF)
            ? ((picture & 0xfefefe) >> 1) : picture;
        dut.clk_pixel = 1;
        dut.eval();
        if (dut.reset_pixel) {
            registered_request = 0;
            hold_meta = hold_sync = false;
            position = frame = 0;
            // The part's SOF-qualified state is restored by the first request.
        } else {
            if (dut.direct_response != (timing | picture)) fail("direct part/two-boundary mismatch");
            if (dut.scanlines_response != (timing | shade)) fail("scanline part/two-boundary mismatch");
            if (valid) {
                if (registered_request & SOF) odd_line = registered_request & EOL;
                else if (registered_request & EOL) odd_line = !odd_line;
            }
            registered_request = current;
            hold_sync = hold_meta;
            hold_meta = dut.hold;
            if (++position == Frame) {
                position = 0;
                ++frame;
                std::cout << "ST video adapter: frame " << frame << " completed, underruns "
                          << dut.debug_underruns << '\n';
            }
        }
        dut.clk_pixel = 0;
        dut.eval();
        ++pixel_cycles;
    }

    void event() {
        if (next_sys < next_pixel) {
            memory_edge();
            next_sys += 74250;
        } else {
            pixel_edge();
            next_pixel += 52224;
        }
    }

public:
    Simulation() {
        ram.fill(0xa55a);
        configurations[0] = {0x010000, 0, 1};
        configurations[1] = {0x020000, 1, 2};
        configurations[2] = {0x030000, 2, 3};
        configurations[3] = {0x040000, 2, 4};
        configurations[4] = {0x07ff00, 0, 5};
        configurations[5] = {0x010000, 3, 6};
        for (auto& config : configurations) {
            for (unsigned i = 0; i < 16; ++i)
                config.colors[i] = (((i + config.seed) & 7) << 6) |
                    (((i * 5 + config.seed * 3) & 7) << 3) | ((i * 3 + config.seed) & 7);
            if (config.mode == 2)
                config.colors[0] = (config.colors[0] & ~1u) | unsigned(config.seed == 4);
            populate(config);
        }
        dut.clk_sys = dut.clk_pixel = 0;
        dut.reset_sys = dut.reset_pixel = 1;
        dut.hold = 0;
        dut.video_ready = 0;
        dut.video_rdata = 0;
        configure(0);
        for (unsigned i = 0; i < 200; ++i) event();
        dut.reset_sys = dut.reset_pixel = 0;
        dut.eval();
    }

    void run() {
        while (frame < 9) event();
        if (!forced_stall || !stalled_picture_lines || !recovered_picture_lines)
            fail("forced underflow did not produce black lines followed by recovery");
        if (!crossing_stall || !crossing_black_lines || !crossing_recovered_lines)
            fail("in-flight previous-frame fill did not mute and recover under a new configuration");
        if (!dut.debug_underruns) fail("underflow counter did not record missing lines");
        std::cout << "ST video adapter: " << pixel_cycles << " independent pixel clocks, "
                  << sys_cycles << " system clocks, " << transfers << " stable reads; "
                  << stalled_picture_lines << " forced black lines, " << recovered_picture_lines
                  << " recovered lines; crossing-frame " << crossing_black_lines << " black/"
                  << crossing_recovered_lines << " recovered lines passed\n";
        dut.final();
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Simulation simulation;
    simulation.run();
    return 0;
}
