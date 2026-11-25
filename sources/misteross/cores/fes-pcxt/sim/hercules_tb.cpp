// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vhercules_top.h"
#include "verilated.h"
#include <array>
#include <cstdio>
#include <random>
#include <stdexcept>
#include <string>

static Vhercules_top top;
static uint64_t ticks;
static std::array<uint8_t, 65536> hgc_vram{};
static std::array<uint8_t, 16384> cga_vram{};

static uint8_t font(unsigned a) {
    return (a & 15) == 13 ? 0xff : (a >> 4) == 0xc4 ? 0x81 : 0x80;
}

static void tick() {
    top.clk = 0; top.eval();
    top.cga_font_data = font(top.cga_font_address);
    top.mda_font_data = font(top.mda_font_address);
    top.eval();
    top.clk = 1; top.eval();
    ++ticks;
}

static void check(bool v, const char* why) {
    if (!v) throw std::runtime_error(why);
}

static unsigned bus(unsigned kind, unsigned addr, bool write = false, unsigned data = 0, bool aen = false) {
    check(top.cycle_ready, "bus busy");
    top.cycle_kind = kind;
    top.cycle_address = addr;
    top.cycle_write = write;
    top.cycle_wdata = data;
    top.cycle_aen = aen;
    top.cycle_valid = 1;
    tick();
    top.cycle_valid = 0;
    unsigned n = 0;
    while (!top.cycle_done && n++ < 200) tick();
    check(top.cycle_done && !top.cycle_fault, "ISA completion fault");
    return top.cycle_rdata;
}

static void out(unsigned a, unsigned d) { bus(1, a, true, d); }
static unsigned in(unsigned a) { return bus(1, a); }
static void reg(unsigned base, unsigned index, unsigned data) {
    out(base + 4, index);
    out(base + 5, data);
}

static void setup_mda_text(unsigned mode, unsigned start = 0) {
    out(0x3b8, 0x00);
    const std::array<unsigned, 16> values{
        97, 80, 82, 15, 25, 6, 25, 25, 2, 13, 0x20, 12, start >> 8, start & 255, 0, 0
    };
    for (unsigned i = 0; i < 16; ++i) reg(0x3b0, i, values[i]);
    out(0x3b8, mode);
}

static void setup_hgc_gfx(unsigned mode, unsigned start = 0) {
    out(0x3b8, 0x00);
    const std::array<unsigned, 16> values{
        0x35, 0x2d, 0x2e, 0x07, 0x5b, 0x02, 0x57, 0x57, 0x02, 0x03, 0x00, 0x00, start >> 8, start & 255, 0, 0
    };
    for (unsigned i = 0; i < 16; ++i) reg(0x3b0, i, values[i]);
    out(0x3b8, mode);
}

static unsigned expected_mda_text(unsigned mode, unsigned start, unsigned x, unsigned y, bool wrap_4k) {
    if (!(mode & 8)) return 0;
    if (x >= 720 || y >= 350) return 0;
    unsigned ma = (start + (y / 14) * 80 + x / 9) & (wrap_4k ? 0x07ffu : 0x3fffu);
    unsigned a = ma * 2;
    const unsigned ch = hgc_vram[a];
    const unsigned attr = hgc_vram[a + 1];
    unsigned dot = x % 9;
    unsigned glyph = font(ch * 16 + (y % 14));
    bool ink = dot < 8 ? ((glyph & (0x80u >> dot)) != 0) : (ch >= 0xc0 && ch <= 0xdf && (glyph & 1));
    if ((attr & 0x77) == 1 && (y % 14) == 13) ink = true;
    if ((attr & 0x77) == 0x70) return ink ? 0 : 7;
    if ((attr & 0x77) == 0) return 0;
    return ink ? ((attr & 8) ? 15 : 7) : 0;
}

static unsigned expected_hgc_gfx(unsigned mode, unsigned page, unsigned start, unsigned x, unsigned y) {
    if (!(mode & 8)) return 0;
    if (x >= 720 || y >= 348) return 0;
    unsigned ma = (start + (y / 4) * 45 + (x / 16)) & 0x0fffu;
    unsigned addr = (page * 0x8000u) + ((y & 3u) * 0x2000u) + (ma * 2u) + ((x & 8u) >> 3);
    uint8_t b = hgc_vram[addr & 0xffffu];
    return (b & (0x80u >> (x & 7u))) ? 7 : 0;
}

static void check_mda_frame(unsigned mode, unsigned start, bool wrap_4k) {
    const unsigned mask = 2u, width = 882u, height = 370u;
    bool began = false;
    unsigned x = 0, y = 0, active = 0, hs = 0, vs = 0;
    uint64_t first = 0;
    for (unsigned n = 0; n < 4500000; ++n) {
        tick();
        if (!(top.pixel_ce & mask)) continue;
        if (top.sof & mask) {
            if (began) {
                check(x == 0 && y == height, "MDA frame geometry");
                const uint64_t dots = uint64_t(width) * height;
                const uint64_t hz = 16257000;
                check((ticks - first) * hz >= dots * 100000000ULL - hz &&
                      (ticks - first) * hz <= dots * 100000000ULL + hz, "MDA cadence");
                check(active == 720u * 350u, "MDA active pixels");
                check(hs == 15u * 9u * height, "MDA HS width");
                check(vs == 16u * width, "MDA VS width");
                return;
            }
            began = true;
            first = ticks;
        }
        if (!began) continue;
        const unsigned got = (top.rgbi >> 4) & 15;
        const unsigned want = expected_mda_text(mode, start, x, y, wrap_4k);
        if (got != want) {
            throw std::runtime_error("MDA pixel mismatch x=" + std::to_string(x) +
                                     " y=" + std::to_string(y) + " got=" + std::to_string(got) +
                                     " want=" + std::to_string(want));
        }
        check(bool(top.eol & mask) == (x == width - 1), "MDA EOL");
        check(bool(top.de & mask) == (x < 720u && y < 350u), "MDA DE");
        active += bool(top.de & mask);
        hs += bool(top.hs & mask);
        vs += bool(top.vs & mask);
        if (++x == width) { x = 0; ++y; }
    }
    throw std::runtime_error("MDA frame timeout");
}

static void check_hgc_status_vsync() {
    bool saw_vs_high = false, saw_vs_low = false;
    for (unsigned n = 0; n < 200000 && !(saw_vs_high && saw_vs_low); ++n) {
        unsigned st = in(0x3ba);
        check((st & 0x70u) == 0x00u, "Hercules ID bits 6:4 must be 000");
        if (st & 0x80u) saw_vs_high = true;
        else saw_vs_low = true;
    }
    check(saw_vs_high && saw_vs_low, "Hercules 3BAh bit 7 did not toggle with VSync");
}

static void check_hgc_frame(unsigned mode, unsigned page, unsigned start, bool enabled = true) {
    const unsigned mask = 2u, width = 864u, height = 370u;
    bool began = false;
    unsigned x = 0, y = 0, active = 0, hs = 0, vs = 0;
    uint64_t first = 0;
    for (unsigned n = 0; n < 4500000; ++n) {
        tick();
        if (!(top.pixel_ce & mask)) continue;
        if (top.sof & mask) {
            if (began) {
                check(x == 0 && y == height, "HGC frame geometry");
                const uint64_t dots = uint64_t(width) * height;
                const uint64_t hz = 16257000;
                check((ticks - first) * hz >= dots * 100000000ULL - hz &&
                      (ticks - first) * hz <= dots * 100000000ULL + hz, "HGC cadence");
                check(active == 720u * 348u, "HGC active pixels");
                check(hs == 7u * 16u * height, "HGC HS width");
                check(vs == 16u * width, "HGC VS width");
                return;
            }
            began = true;
            first = ticks;
        }
        if (!began) continue;
        const unsigned got = (top.rgbi >> 4) & 15;
        const unsigned want = enabled ? expected_hgc_gfx(mode, page, start, x, y) : 0;
        if (got != want) {
            throw std::runtime_error("HGC pixel mismatch page=" + std::to_string(page) +
                                     " x=" + std::to_string(x) + " y=" + std::to_string(y) +
                                     " got=" + std::to_string(got) + " want=" + std::to_string(want));
        }
        check(bool(top.eol & mask) == (x == width - 1), "HGC EOL");
        check(bool(top.de & mask) == (x < 720u && y < 348u), "HGC DE");
        active += bool(top.de & mask);
        hs += bool(top.hs & mask);
        vs += bool(top.vs & mask);
        if (++x == width) { x = 0; ++y; }
    }
    throw std::runtime_error("HGC frame timeout");
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        // 1. Strict MDA mode (video_card = 1): CGA disabled, 3BFh ignored, 4K wrap.
        top.video_card = 1;
        top.reset = 1;
        for (unsigned n = 0; n < 10; ++n) tick();
        top.reset = 0;
        for (unsigned n = 0; n < 10; ++n) tick();

        bus(0, 0xb0000, true, 0xa5);
        check(bus(0, 0xb7000) == 0xa5, "strict MDA 4 KiB VRAM mirror");
        bus(0, 0xb8000, true, 0x5a);
        check(bus(0, 0xb8000) == 0xff, "CGA B8000 must be unclaimed in MDA mode");
        check(in(0x3da) == 0xff, "CGA 3DA must be unclaimed in MDA mode");

        out(0x3bf, 0x03); // must be ignored in strict MDA mode
        out(0x3b8, 0x0a);
        check(!top.hercules_graphics_mode, "strict MDA must ignore 3BFh/graphics mode");
        check((in(0x3ba) & 0xf0) == 0xf0, "strict MDA 3BAh high nibble must be 0xf");

        for (unsigned i = 0; i < 4096; i += 2) {
            hgc_vram[i] = (i & 2) ? 0xc4 : 0x41;
            hgc_vram[i + 1] = std::array<unsigned, 8>{0, 1, 7, 8, 9, 15, 0x70, 0x78}[(i / 2) % 8];
            bus(0, 0xb0000 + i, true, hgc_vram[i]);
            bus(0, 0xb0000 + i + 1, true, hgc_vram[i + 1]);
        }
        setup_mda_text(0x09, 0x7f0);
        check_mda_frame(0x09, 0x7f0, true);
        std::puts("PASS strict MDA mode (video_card=1): 4K mirror, 3BFh ignored, 720x350 text");

        // 2. Hercules mode (video_card = 2): Diag -> Half -> Full (Page 0 + Page 1) -> Text.
        top.video_card = 2;
        top.reset = 1;
        for (unsigned n = 0; n < 10; ++n) tick();
        top.reset = 0;
        for (unsigned n = 0; n < 10; ++n) tick();

        // Diag mode (after reset): graphics ignored, B8000 unclaimed.
        out(0x3b8, 0x0a);
        check(!top.hercules_graphics_mode, "Hercules reset Diag mode must block graphics");
        bus(0, 0xb8000, true, 0x33);
        check(bus(0, 0xb8000) == 0xff, "Hercules Diag mode must not claim B8000");

        // Half mode (3BFh = 1): Page 0 graphics allowed, B8000 still unclaimed, 3B8h[7] ignored.
        out(0x3bf, 0x01);
        bus(0, 0xb0000, true, 0x77);
        bus(0, 0xb8000, true, 0x99);
        check(bus(0, 0xb8000) == 0xff && bus(0, 0xb0000) == 0x77, "Half mode must leave B8000 unclaimed");

        std::mt19937 rng(0x484743u);
        for (unsigned i = 0; i < 32768; ++i) {
            hgc_vram[i] = uint8_t(rng());
            bus(0, 0xb0000 + i, true, hgc_vram[i]);
        }
        setup_hgc_gfx(0x8a); // bit 7 set, but Half mode forces Page 0
        check(top.hercules_graphics_mode, "Hercules Half mode graphics active");
        check_hgc_frame(0x8a, 0, 0, true);
        check_hgc_status_vsync();
        std::puts("PASS Hercules Half mode (3BFh=1): 720x348 Page 0, B8000 unclaimed, 3BAh vsync");

        // Full mode (3BFh = 3): B8000-BFFFF claimed as Page 1 without colliding with Page 0.
        out(0x3bf, 0x03);
        for (unsigned i = 0; i < 32768; ++i) {
            hgc_vram[0x8000 + i] = uint8_t(rng() ^ 0xa5u);
            bus(0, 0xb8000 + i, true, hgc_vram[0x8000 + i]);
        }
        // Verify Page 0 wasn't corrupted by Page 1 writes.
        for (unsigned i = 0; i < 512; ++i) {
            unsigned off = (rng() % 32768);
            check(bus(0, 0xb0000 + off) == hgc_vram[off], "Page 1 write corrupted Page 0");
            check(bus(0, 0xb8000 + off) == hgc_vram[0x8000 + off], "Page 1 readback mismatch");
        }
        // Page 1 active (3B8h = 8Ah).
        out(0x3b8, 0x8a);
        check_hgc_frame(0x8a, 1, 0, true);
        check_hgc_status_vsync();
        // Flip back to Page 0 (3B8h = 0Ah).
        out(0x3b8, 0x0a);
        check_hgc_frame(0x0a, 0, 0, true);
        // Video disable blanks raster.
        out(0x3b8, 0x02);
        check_hgc_frame(0x02, 0, 0, false);
        std::puts("PASS Hercules Full mode (3BFh=3): 64K Page 0/1 flips + blanking");

        // Return to text mode (3B8h = 28h) cleanly without reset.
        for (unsigned i = 0; i < 4096; i += 2) {
            hgc_vram[i] = (i & 2) ? 0xc4 : 0x41;
            hgc_vram[i + 1] = std::array<unsigned, 8>{0, 1, 7, 8, 9, 15, 0x70, 0x78}[(i / 2) % 8];
            bus(0, 0xb0000 + i, true, hgc_vram[i]);
            bus(0, 0xb0000 + i + 1, true, hgc_vram[i + 1]);
        }
        setup_mda_text(0x28, 0);
        check(!top.hercules_graphics_mode, "Return to text mode cleared graphics_mode");
        check_mda_frame(0x28, 0, false);
        std::puts("PASS Hercules -> Text transition: clean 720x350 frame");

        // 3. Dual CGA + MDA mode (video_card = 3): simultaneous decode with zero collision.
        top.video_card = 3;
        top.reset = 1;
        for (unsigned n = 0; n < 10; ++n) tick();
        top.reset = 0;
        for (unsigned n = 0; n < 10; ++n) tick();
        out(0x3bf, 0x03); // ignored when hercules_enable=0
        bus(0, 0xb0000, true, 0x12);
        bus(0, 0xb8000, true, 0x34);
        check(bus(0, 0xb0000) == 0x12 && bus(0, 0xb8000) == 0x34, "Dual CGA+MDA VRAM isolation");
        cga_vram[0] = 0x34;
        std::puts("PASS Dual CGA+MDA mode (video_card=3): zero collision");
    } catch (const std::exception& e) {
        std::fprintf(stderr, "hercules: %s\n", e.what());
        return 1;
    }
    return 0;
}
