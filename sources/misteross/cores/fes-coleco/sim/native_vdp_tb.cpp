// SPDX-License-Identifier: GPL-2.0-or-later
// Literal CPU-programmed VRAM fixtures through the real registered VDP.
#include "Vnative_vdp_top.h"
#include "verilated.h"
#include <algorithm>
#include <cstdint>
#include <cstdlib>
#include <deque>
#include <iostream>
#include <limits>

static void require(bool ok, const char *message) {
    if (!ok) { std::cerr << "Coleco native VDP: " << message << '\n'; std::exit(1); }
}

// These values describe the fixture, independently of the VDP's RAM/data path.
static unsigned expected_pixel(unsigned x, unsigned y) {
    const unsigned page = y / 64, row = y % 8;
    const unsigned pattern = x / 8 == 1 ? 0x81 : page == 0 ? (row & 1 ? 0x55 : 0xaa) :
                             page == 1 ? (row & 1 ? 0x01 : 0x80) : (row & 1 ? 0x0f : 0xf0);
    const bool foreground = pattern & (0x80 >> (x % 8));
    unsigned color = x / 8 == 1 ? (foreground ? 13 : 8) :
                     (foreground ? 2 + page * 2 : 1 + page * 2);
    if (y >= 16 && y < 24) {
        if (x >= 39 && x < 47) color = 2;
        if (x >= 40 && x < 48 && ((row & 1 ? 0x42 : 0x81) & (0x80 >> (x - 40)))) color = 14;
        if (x >= 100 && x < 108) color = 3; // preceding color-zero sprite is transparent
    }
    if (y >= 32 && y < 40 && x >= 253) color = 15; // clipped right edge
    if (y >= 48 && y < 56 && x < 4) color = 12; // early-clock sprite at x=-4
    return color;
}

static uint32_t palette(unsigned color) {
    static const uint32_t colors[] = {0, 0, 0x21c842, 0x5edc78, 0x5455ed, 0x7d76fc,
        0xd4524d, 0x42ebf5, 0xfc5554, 0xff7978, 0xd4c154, 0xe6ce80,
        0x21b03b, 0xc95bba, 0xcccccc, 0xffffff};
    return colors[color];
}

struct Bench {
    Vnative_vdp_top d;
    uint64_t next_sys = 0, next_pixel = 17381, sys_edges = 0, pixel_edges = 0;
    std::deque<uint32_t> outstanding;
    unsigned frame_pixels = 0, source_frames = 0, source_checked = 0;
    unsigned hdmi_frames_checked = 0, hdmi_frame_pixels = 0, crossed_tokens = 0;
    unsigned coordinate_age = 0, previous_x = 0, previous_y = 0;
    bool previous_blank = true, collecting = false, checking_hdmi = false;
    bool held_at_source = true, held_at_sink = true;
    unsigned min_age = std::numeric_limits<unsigned>::max(), max_age = 0;
    unsigned early_wrong_pixels = 0, latest_early_wrong_age = 0;
    unsigned source_spacing_min = 1000, source_spacing_max = 0;
    uint64_t previous_pixel_edge = 0;
    bool hold_recovery = false;
    unsigned invalidations = 0, explicitly_dropped_prefix_pixels = 0;

    Bench() {
        d.clk_sys = d.pixel_clk = 0; d.reset = d.hold = 1; d.run_raster = 0;
        d.cpu_ce = 0; d.cpu_iorq_n = d.cpu_rd_n = d.cpu_wr_n = 1;
        d.cpu_a = d.cpu_din = 0; d.eval(); cycles(8); d.reset = 0;
    }

    void source() {
        ++sys_edges;
        const bool changed = previous_x != d.logical_x || previous_y != d.logical_y ||
                             previous_blank != bool(d.logical_blank);
        coordinate_age = changed ? 0 : coordinate_age + 1;
        previous_x = d.logical_x; previous_y = d.logical_y; previous_blank = d.logical_blank;
        if (source_frames >= 1 && !d.logical_blank && !d.hold && coordinate_age < 4 &&
            d.logical_pixel != expected_pixel(d.logical_x, d.logical_y)) {
            ++early_wrong_pixels; latest_early_wrong_age = std::max(latest_early_wrong_age, coordinate_age);
        }
        const uint32_t word = d.source_request;
        if (!(word & (1u << 24))) return;
        outstanding.push_back(word);
        require(!(word & 0xc0fffff0u), "source reserved bits or non-indexed payload set");
        if (word & (1u << 29)) {
            held_at_source = word & (1u << 28);
            require(word == (held_at_source ? 0x31000000u : 0x21000000u), "malformed source HOLD control");
            collecting = false; frame_pixels = 0; return;
        }
        require(!held_at_source && !d.logical_blank, "pixel emitted during HOLD or logical blank");
        if (word & (1u << 25)) {
            require(d.logical_x == 0 && d.logical_y == 0, "SOF coordinate mismatch");
            require(!collecting, "duplicate SOF before EOF");
            collecting = true; frame_pixels = 0;
        }
        if (!collecting) return; // release midway through raster: await the next SOF
        const unsigned x = frame_pixels % 256, y = frame_pixels / 256;
        require(d.logical_x == x && d.logical_y == y, "source coordinate skipped, duplicated or reordered");
        require(bool(word & (1u << 25)) == (frame_pixels == 0), "SOF not exactly at first pixel");
        require(bool(word & (1u << 26)) == (x == 255), "EOL not exactly at each final column");
        require(bool(word & (1u << 27)) == (frame_pixels == 256 * 192 - 1), "EOF not exactly at final pixel");
        require(!(word & (1u << 28)), "pixel token has HOLD set");
        // Three waiting edges follow the edge that detects the changed VDP
        // coordinate. The coordinate itself appeared one edge before detection.
        require(coordinate_age == 4, "adapter did not wait three clocks after coordinate detection");
        min_age = std::min(min_age, coordinate_age); max_age = std::max(max_age, coordinate_age);
        if (frame_pixels && previous_pixel_edge) {
            const unsigned spacing = unsigned(sys_edges - previous_pixel_edge);
            require(spacing == 12 || spacing == 13, "source did not retain fractional production cadence");
            source_spacing_min = std::min(source_spacing_min, spacing);
            source_spacing_max = std::max(source_spacing_max, spacing);
        }
        previous_pixel_edge = sys_edges;
        if (source_frames >= 1) {
            if ((word & 15) != expected_pixel(x, y)) {
                std::cerr << "x=" << x << " y=" << y << " got=" << (word & 15)
                          << " expected=" << expected_pixel(x, y) << '\n';
                require(false, "registered VDP tile/sprite pixel was not settled at token emission");
            }
            ++source_checked;
        }
        if (++frame_pixels == 256 * 192) { ++source_frames; collecting = false; }
    }

    void pixel() {
        const unsigned h = unsigned(pixel_edges % 1650), v = unsigned((pixel_edges / 1650) % 750);
        const unsigned output_frame = unsigned(pixel_edges / (1650 * 750));
        ++pixel_edges;
        const uint32_t word = d.crossed_request;
        if (word & (1u << 24)) {
            if (word == 0x41000000u) {
                // HOLD can collide with an outstanding pixel handshake. Its
                // control is retained, and the CDC explicitly invalidates
                // that partial frame. Release can similarly discard the
                // first pre-SOF pixel while the control is still in flight.
                require(hold_recovery, "CDC overrun at normal native pixel cadence");
                ++invalidations;
                if (!outstanding.empty() && !(outstanding.front() & (1u << 29))) {
                    require(!(outstanding.front() & (1u << 25)) && !collecting,
                            "HOLD exception discarded a pixel of a complete source frame");
                    outstanding.pop_front(); ++explicitly_dropped_prefix_pixels;
                }
            } else {
                require(!outstanding.empty() && outstanding.front() == word,
                        "CDC lost, duplicated or changed real VDP token without explicit invalidation");
                outstanding.pop_front(); ++crossed_tokens;
                if (word & (1u << 29)) held_at_sink = word & (1u << 28);
                if (word & (1u << 25)) hold_recovery = false;
            }
        }
        const uint32_t direct = d.direct_response, scanlines = d.scanlines_response;
        const uint32_t sync = (1u << 27) | ((v >= 725 && v < 730) ? 1u << 26 : 0) |
                              ((h >= 1390 && h < 1430) ? 1u << 25 : 0) |
                              ((h < 1280 && v < 720) ? 1u << 24 : 0);
        require((direct & 0x0f000000u) == sync && (scanlines & 0x0f000000u) == sync,
                "native scanout stopped or changed fixed HDMI timing");
        if (held_at_sink) require(!(direct & 0x00ffffffu) && !(scanlines & 0x00ffffffu), "HOLD did not black both native outputs");
        if (h == 0 && v == 0) {
            checking_hdmi = output_frame >= 4 && source_frames >= 2 && !held_at_sink;
            hdmi_frame_pixels = 0;
        }
        if (held_at_sink) checking_hdmi = false;
        if (checking_hdmi) {
            uint32_t expected = 0;
            if (h >= 384 && h < 896 && v >= 168 && v < 552)
                expected = palette(expected_pixel((h - 384) / 2, (v - 168) / 2));
            const uint32_t dim = v & 1 ? ((expected >> 1) & 0x7f7f7fu) : expected;
            if ((direct & 0x00ffffffu) != expected || (scanlines & 0x00ffffffu) != dim) {
                std::cerr << "HDMI " << h << ',' << v << " direct=" << std::hex << (direct & 0xffffffu)
                          << " expected=" << expected << " scanlines=" << (scanlines & 0xffffffu) << std::dec << '\n';
                require(false, "real VDP native frame scanout differs from literal full-frame oracle");
            }
            ++hdmi_frame_pixels;
            if (h == 1649 && v == 749) {
                require(hdmi_frame_pixels == 1650 * 750, "partial HDMI frame comparison");
                ++hdmi_frames_checked;
            }
        }
    }

    void event() {
        const uint64_t time = std::min(next_sys, next_pixel);
        const bool sys = next_sys == time, pix = next_pixel == time;
        if (sys) { d.clk_sys = !d.clk_sys; next_sys += 74250; }
        if (pix) { d.pixel_clk = !d.pixel_clk; next_pixel += 52224; }
        d.eval();
        if (sys && d.clk_sys) source();
        if (pix && d.pixel_clk) pixel();
    }
    void cycles(unsigned count) { const uint64_t end = sys_edges + count; while (sys_edges < end) event(); }
    void out(unsigned port, unsigned value) {
        d.cpu_a = port; d.cpu_din = value; d.cpu_ce = 1; d.cpu_iorq_n = d.cpu_wr_n = 0;
        cycles(1); d.cpu_ce = 0; d.cpu_iorq_n = d.cpu_wr_n = 1; cycles(4);
    }
    void reg(unsigned r, unsigned value) { out(0xbf, value); out(0xbf, 0x80 | r); }
    void address(unsigned a) { out(0xbf, a & 255); out(0xbf, 0x40 | (a >> 8)); }
    void byte(unsigned a, unsigned value) { address(a); out(0xbe, value); }
    void fixture() {
        address(0); for (unsigned a = 0; a < 16384; ++a) out(0xbe, 0);
        reg(0, 2); reg(1, 0x40); reg(2, 6); reg(3, 0x7f); reg(4, 7); reg(5, 0x36); reg(6, 7); reg(7, 13);
        for (unsigned y = 0; y < 24; ++y) byte(0x1800 + y * 32 + 1, 8);
        for (unsigned row = 0; row < 8; ++row) {
            byte(0x2000 + row, row & 1 ? 0x55 : 0xaa); byte(0x2800 + row, row & 1 ? 0x01 : 0x80);
            byte(0x3000 + row, row & 1 ? 0x0f : 0xf0);
            for (unsigned page = 0; page < 3; ++page) {
                byte(page * 0x800 + row, ((2 + 2 * page) << 4) | (1 + 2 * page));
                byte(0x2040 + page * 0x800 + row, 0x81); byte(0x0040 + page * 0x800 + row, 0x08);
            }
            byte(0x3800 + row, row & 1 ? 0x42 : 0x81);
            for (unsigned pattern : {4u, 8u, 12u, 16u}) byte(0x3800 + pattern * 8 + row, 0xff);
        }
        const unsigned sprites[][4] = {{15, 40, 0, 14}, {15, 39, 4, 2}, {15, 100, 8, 0},
                                       {15, 100, 8, 3}, {31, 253, 12, 15}, {47, 28, 16, 0x8c}};
        for (unsigned n = 0; n < 6; ++n) for (unsigned i = 0; i < 4; ++i) byte(0x1b00 + n * 4 + i, sprites[n][i]);
        byte(0x1b18, 0xd0);
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv); Bench b; b.fixture();
    b.d.hold = 0; b.d.run_raster = 1;
    while (b.hdmi_frames_checked < 2 && b.sys_edges < 8'000'000) b.event();
    require(b.hdmi_frames_checked == 2 && b.source_checked >= 2 * 256 * 192, "complete native frames did not reach HDMI");
    require(b.early_wrong_pixels && b.latest_early_wrong_age < 4, "fixture did not exercise actual registered lookup settling");
    require(b.source_spacing_min == 12 && b.source_spacing_max == 13, "fractional raster cadence not covered");
    // Interrupt an actual active source line, rather than coincident output
    // blanking. The adapter must abandon that incomplete source frame.
    while ((b.d.logical_blank || b.d.logical_y != 64 || b.d.logical_x < 96 || b.d.logical_x > 100) &&
           b.sys_edges < 9'000'000) b.event();
    require(b.collecting && b.frame_pixels > 0 && b.frame_pixels < 256 * 192,
            "HOLD scenario did not interrupt an active partial source frame");
    b.hold_recovery = true; b.d.hold = 1; b.cycles(512);
    require(b.held_at_source && b.held_at_sink && b.outstanding.empty(), "HOLD did not cross/drain with clocks running");
    b.d.hold = 0;
    const unsigned prior_frames = b.source_frames;
    while ((b.source_frames < prior_frames + 2 || b.hdmi_frames_checked < 3) && b.sys_edges < 12'000'000) b.event();
    require(b.source_frames >= prior_frames + 2 && b.hdmi_frames_checked >= 3, "native VDP did not resume complete frames after HOLD");
    require(!b.hold_recovery && b.invalidations && b.explicitly_dropped_prefix_pixels,
            "HOLD collision did not exercise explicit invalidation followed by complete-frame resynchronization");
    std::cout << "Registered Coleco VDP native frames passed: source_frames=" << b.source_frames
              << " checked_indexed_pixels=" << b.source_checked << " full_HDMI_frames=" << b.hdmi_frames_checked
              << " CDC_tokens=" << b.crossed_tokens << " coordinate_age=" << b.min_age << ':' << b.max_age
              << " early_unsettled_samples=" << b.early_wrong_pixels << " latest_unsettled_age=" << b.latest_early_wrong_age
              << " fractional_spacing=" << b.source_spacing_min << ':' << b.source_spacing_max
              << " HOLD_invalidations=" << b.invalidations << " dropped_pre_SOF_pixels=" << b.explicitly_dropped_prefix_pixels << '\n';
}
