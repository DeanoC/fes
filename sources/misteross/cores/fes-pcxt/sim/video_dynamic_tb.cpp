// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vvideo_dynamic_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdio>
#include <deque>
#include <stdexcept>
#include <string>
#include <vector>

static void check(bool b, const char* w) {
    if (!b) throw std::runtime_error(w);
}

static uint32_t cga_rgb(unsigned c) {
    static const uint32_t p[] = {
        0x000000, 0x0000aa, 0x00aa00, 0x00aaaa, 0xaa0000, 0xaa00aa, 0xaa5500, 0xaaaaaa,
        0x555555, 0x5555ff, 0x55ff55, 0x55ffff, 0xff5555, 0xff55ff, 0xffff55, 0xffffff
    };
    return p[c & 15];
}

static uint32_t mono_rgb(unsigned c, unsigned tint) {
    if ((c & 15) == 0) return 0x000000;
    bool bright = (c & 8) != 0;
    if (tint == 1) return bright ? 0x55ff55 : 0x00aa00; // Green P39
    if (tint == 2) return bright ? 0xffbb33 : 0xcc7700; // Amber P3
    return bright ? 0xffffff : 0xaaaaaa;                // White
}

struct ModeSpec {
    unsigned source_mode;
    unsigned width, height, totalw, totalh, dot_hz;
};

static const ModeSpec SPECS[3] = {
    {0, 640, 200, 912, 262, 14318180},
    {1, 720, 350, 882, 370, 16257000},
    {2, 720, 348, 864, 370, 16257000},
};

static unsigned encode_pixel(unsigned source_mode, unsigned id, unsigned x, unsigned y) {
    // Encode source_mode and frame id into the top-left 128 pixels so the
    // 720p consumer can verify there is zero tearing across dynamic transitions!
    unsigned c = ((x / 17) ^ (y / 7) ^ id) & 15;
    if (y == 0 && x < 16) {
        if (source_mode == 0) return (id & 15) ? (id & 15) : 1;
        // For mono modes, encode bits of (id[3:0] | (source_mode<<4)) as bright vs normal vs black in x=0..11!
        if (x < 6) {
            unsigned tag = (source_mode << 4) | (id & 15);
            return ((tag >> x) & 1) ? 15 : 7;
        }
        return 0;
    }
    if (source_mode != 0) return (c % 3 == 0) ? 0 : (c % 3 == 1) ? 7 : 15;
    return c;
}

static uint32_t expected_rgb(unsigned source_mode, unsigned tint, unsigned id, unsigned x, unsigned y) {
    unsigned p = encode_pixel(source_mode, id, x, y);
    return source_mode == 0 ? cga_rgb(p) : mono_rgb(p, tint);
}

static void run_sequence(unsigned rate, bool switch_modes) {
    Vvideo_dynamic_top t;
    t.clk_sys = t.clk_pixel = 0;
    t.reset_sys = t.reset_pixel = 1;
    t.hold = 0;

    // Schedule: MDA -> HGC -> MDA -> CGA (with tint cycling) in normal mode,
    // or fixed HGC in doubled-rate bank-queue stress mode.
    unsigned cur_mode = switch_modes ? 1 : 2;
    unsigned cur_tint = 1; // start Green P39
    t.source_mode = cur_mode;
    t.mono_tint = cur_tint;

    uint64_t time = 0, nextsys = 10, nextpixel = 13, phase = 0;
    unsigned sx = 0, sy = 0, source_frame = 1, output_frames = 0, ox = 0, oy = 0;
    bool aligned = false, pause = false, expect_black = true, did_reset = false;
    uint64_t reset_until = 300;
    std::deque<uint32_t> pipeline;
    unsigned checked = 0, visible_frames = 0, previous_id = 0, pixel_phase = 0, part_y = 0;
    unsigned mode_mask_seen = 0, tint_mask_seen = 0;
    unsigned active_mode = 0, active_tint = 0, active_id = 0;
    uint64_t line0_candidates = 0;
    std::vector<unsigned> shown;

    const unsigned max_frames = switch_modes ? 36u : 20u;
    const uint64_t limit = 2200000000ULL;

    while (time < limit && output_frames < max_frames) {
        time = nextsys < nextpixel ? nextsys : nextpixel;
        bool sysedge = time == nextsys, pixedge = time == nextpixel;
        if (sysedge) { t.clk_sys = !t.clk_sys; nextsys += 10; }
        if (pixedge) {
            t.clk_pixel = !t.clk_pixel;
            pixel_phase += 468;
            nextpixel += 13 + (pixel_phase >= 1000);
            if (pixel_phase >= 1000) pixel_phase -= 1000;
        }
        if (switch_modes && output_frames == 28 && !did_reset) {
            did_reset = true;
            reset_until = time + 1000;
            aligned = false;
            pipeline.clear();
            expect_black = true;
        }
        t.reset_sys = t.reset_pixel = time <= reset_until;

        if (sysedge && t.clk_sys) {
            t.pixel_ce = 0;
            const auto& sp = SPECS[cur_mode];
            phase += uint64_t(sp.dot_hz) * rate;
            if (phase >= 100000000ULL) {
                phase -= 100000000ULL;
                if (!pause) {
                    if (sx == 0 && sy == 0 && switch_modes) {
                        if (source_frame <= 4) { cur_mode = 1; cur_tint = 1; }
                        else if (source_frame <= 8) { cur_mode = 2; cur_tint = 2; }
                        else if (source_frame <= 12) { cur_mode = 1; cur_tint = 0; }
                        else { cur_mode = 0; cur_tint = 0; }
                        t.source_mode = cur_mode;
                        t.mono_tint = cur_tint;
                    }
                    const auto& active_sp = SPECS[cur_mode];
                    t.pixel_ce = 1;
                    t.source_sof = (sx == 0 && sy == 0);
                    t.source_eol = (sx == active_sp.totalw - 1);
                    t.source_de = (sx < active_sp.width && sy < active_sp.height);
                    // Frame 3 loses an active pixel -> must not publish!
                    if (switch_modes && source_frame == 3 && sx == 17 && sy == 10) t.source_de = 0;
                    unsigned id = ((source_frame - 1) % 15) + 1;
                    t.rgbi = encode_pixel(cur_mode, id, sx, sy);
                    if (++sx == active_sp.totalw) {
                        sx = 0;
                        if (++sy == active_sp.totalh) {
                            sy = 0;
                            ++source_frame;
                        }
                    }
                }
            }
        }
        t.eval();
        if (!pixedge || !t.clk_pixel || t.reset_pixel) continue;
        uint32_t request = t.video_request;
        if (request & (1u << 28)) {
            if (aligned) { check(ox == 0 && oy == 750, "720p total geometry"); ++output_frames; }
            aligned = true;
            ox = oy = 0;
            if (switch_modes) {
                pause = (output_frames >= 20 && output_frames < 23);
                t.hold = (output_frames >= 24 && output_frames < 26);
            }
        }
        if (!aligned) continue;
        bool de = ox < 1280 && oy < 720;
        bool hs = ox >= 1390 && ox < 1430;
        bool vs = oy >= 725 && oy < 730;
        check(bool(request & (1u << 24)) == de, "720p DE");
        check(bool(request & (1u << 25)) == hs, "720p HS");
        check(bool(request & (1u << 26)) == vs, "720p VS");
        check(bool(request & (1u << 29)) == (ox == 1649), "720p EOL");
        uint32_t c = request & 0xffffffu;

        // Identify active (mode, tint, frame_id) from line 0 header pixels x=0..15 in 960x720 viewport.
        if (oy == 0 && ox == 160) {
            line0_candidates = ~0ULL; // 48 combinations: mode(0..2) * 16 + id(1..15), plus tint check
            expect_black = true;
        }
        if (oy == 0 && ox >= 160 && ox < 182) {
            if (c != 0) expect_black = false;
            for (unsigned m = 0; m < 3; ++m) {
                unsigned sx_m = (ox - 160) * SPECS[m].width / 960;
                for (unsigned id = 1; id < 16; ++id) {
                    unsigned bit_idx = m * 16 + id;
                    if (!(line0_candidates & (1ULL << bit_idx))) continue;
                    bool match = false;
                    for (unsigned tt = 0; tt < (m == 0 ? 1u : 3u); ++tt) {
                        if (c == expected_rgb(m, tt, id, sx_m, 0)) match = true;
                    }
                    if (!match) line0_candidates &= ~(1ULL << bit_idx);
                }
            }
            if (ox == 181 && !expect_black) {
                bool found = false;
                for (unsigned m = 0; m < 3 && !found; ++m) {
                    for (unsigned id = 1; id < 16 && !found; ++id) {
                        if (line0_candidates & (1ULL << (m * 16 + id))) {
                            active_mode = m;
                            active_id = id;
                            found = true;
                        }
                    }
                }
                check(found, "could not identify active dynamic frame header");
                if (active_mode == 0) active_tint = 0;
                else {
                    // Determine tint from any non-black pixel in ox=160..181
                    active_tint = 0;
                    for (unsigned tt = 0; tt < 3; ++tt) {
                        // Checked against full body pixels below too!
                        if (c == mono_rgb(7, tt) || c == mono_rgb(15, tt)) { active_tint = tt; break; }
                    }
                }
                if (switch_modes) check(active_id != 3 || output_frames >= 10, "malformed source frame published");
                mode_mask_seen |= (1u << active_mode);
                tint_mask_seen |= (1u << active_tint);
                if (active_id != previous_id) ++visible_frames;
                previous_id = active_id;
                if (!switch_modes && (shown.empty() || shown.back() != active_id)) shown.push_back(active_id);
            }
        }
        if ((request >> 30) & 1) {
            check(c == 0, "HOLD did not mute");
        } else if (!expect_black && ox >= 160 && ox < 1120 && oy < 720) {
            if (oy > 0) {
                unsigned sx_a = (ox - 160) * SPECS[active_mode].width / 960;
                unsigned sy_a = oy * SPECS[active_mode].height / 720;
                uint32_t want = expected_rgb(active_mode, active_tint, active_id, sx_a, sy_a);
                if (c != want) {
                    // Re-resolve tint on first non-black mono pixel in case ox==181 was black.
                    if (active_mode != 0 && oy == 1 && active_tint == 0) {
                        for (unsigned tt = 1; tt < 3; ++tt) {
                            if (c == expected_rgb(active_mode, tt, active_id, sx_a, sy_a)) {
                                active_tint = tt;
                                tint_mask_seen |= (1u << active_tint);
                                want = c;
                                break;
                            }
                        }
                    }
                    if (c != want) {
                        throw std::runtime_error("dynamic video pixel mismatch mode=" + std::to_string(active_mode) +
                                                 " tint=" + std::to_string(active_tint) +
                                                 " x=" + std::to_string(ox) + " y=" + std::to_string(oy));
                    }
                }
                ++checked;
            }
        } else if (!de || ox < 160 || ox >= 1120) {
            check(c == 0, "sidebar/blanking not black");
        }

        pipeline.push_back(request);
        if (pipeline.size() > 2) {
            uint32_t old = pipeline.front();
            pipeline.pop_front();
            uint32_t want = old & 0x0fffffffu;
            if ((old >> 30) & 1) want &= 0x0f000000u;
            if (!(old & (1u << 24))) want &= 0x0f000000u;
            check(t.direct_response == want, "Direct response alignment");
            if (old & (1u << 28)) part_y = 0;
            uint32_t dim = want;
            if (part_y & 1) dim = (want & 0x0f000000u) | ((want & 0xfefefeu) >> 1);
            check(t.scanline_response == dim, "Scanlines response alignment");
            if (old & (1u << 29)) ++part_y;
        }
        if (++ox == 1650) { ox = 0; ++oy; }
    }

    if (switch_modes) {
        check(output_frames == max_frames && checked > 5000000, "dynamic video frame coverage");
        check(mode_mask_seen == 0x7, "all 3 video modes (CGA, MDA, HGC) must be observed cleanly");
        check(tint_mask_seen == 0x7, "all 3 monochrome tints (White, Green, Amber) must be observed");
        std::printf("PASS PCXT dynamic video: %u frames, %u checked pixels, modes=CGA+MDA+HGC, tints=White+Green+Amber\n",
                    output_frames, checked);
    } else {
        check(output_frames == max_frames && checked > 2000000 && shown.size() >= 6, "doubled-rate HGC coverage");
        std::printf("PASS PCXT dynamic video doubled-rate HGC: %u frames, %u checked pixels, %zu IDs\n",
                    output_frames, checked, shown.size());
    }
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        run_sequence(1, true);
        run_sequence(2, false);
    } catch (const std::exception& e) {
        std::fprintf(stderr, "video-dynamic: %s\n", e.what());
        return 1;
    }
    return 0;
}
