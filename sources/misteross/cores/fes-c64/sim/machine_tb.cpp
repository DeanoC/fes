// SPDX-License-Identifier: MIT
// Boots the open C64 diagnostic and requires the CPU, both cartridge sockets,
// the joystick, the keyboard, a SID sample, the D64 LOAD and both border and
// text pixels. The firmware also requires CIA1 timer B IRQ and CIA2 timer A
// NMI through the CPU vectors, with SEI still set during the NMI check.
// Success then captures the next full frame. Earlier white and red pixels are
// not the pass mark: the frame must show PASS, a black border, a black
// background and no stage digit.
#include "Vc64_sim_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <cstdio>
#include <vector>

static constexpr int kWidth = 1280;
static constexpr int kHeight = 720;
static constexpr int kFrameCycles = 1650 * 750;

// Open font, bit 7 on the left. P, A and S are screen codes 16, 1 and 19.
static constexpr uint8_t kGlyphP[8] = {0x7C, 0x66, 0x66, 0x7C, 0x60, 0x60, 0x60, 0x00};
static constexpr uint8_t kGlyphA[8] = {0x3C, 0x66, 0x66, 0x7E, 0x66, 0x66, 0x66, 0x00};
static constexpr uint8_t kGlyphS[8] = {0x3C, 0x66, 0x60, 0x3C, 0x06, 0x66, 0x3C, 0x00};

static bool black_at(const std::vector<uint8_t> &rgb, int x, int y) {
    const uint8_t *p = &rgb[(y * kWidth + x) * 3];
    return p[0] == 0 && p[1] == 0 && p[2] == 0;
}

static bool white_at(const std::vector<uint8_t> &rgb, int x, int y) {
    const uint8_t *p = &rgb[(y * kWidth + x) * 3];
    return p[0] == 0xFF && p[1] == 0xFF && p[2] == 0xFF;
}

// PASS is painted on character rows 11..13. The sample x is the last pixel of
// each 4x-scaled glyph bit, which stays on that bit after the VIC's two-cycle
// character-code delay and one-cycle bit-select delay. y is the middle line of
// the 3x vertical scale.
static int check_pass_frame(const std::vector<uint8_t> &rgb) {
    if (rgb.size() != static_cast<size_t>(kWidth * kHeight * 3)) {
        std::printf("c64 PASS frame has %zu pixels, want %d\n",
                    rgb.size() / 3, kWidth * kHeight);
        return 1;
    }
    // $0408 is row 0, column 8. A leftover stage digit is white there.
    for (int y = 60; y < 84; ++y) {
        for (int x = 8 * 32; x < 9 * 32; ++x) {
            if (!black_at(rgb, x, y)) {
                std::printf("c64 PASS frame still shows the stage digit at %d,%d\n", x, y);
                return 1;
            }
        }
    }
    constexpr int pass_top = 60 + 11 * 24;
    constexpr int pass_bottom = 60 + 14 * 24;
    for (int y = 0; y < kHeight; ++y) {
        if (y >= pass_top && y < pass_bottom)
            continue;
        const bool border = y < 60 || y >= 660;
        for (int x = 0; x < kWidth; ++x) {
            if (!black_at(rgb, x, y)) {
                const uint8_t *p = &rgb[(y * kWidth + x) * 3];
                std::printf("c64 PASS frame %s pixel %d,%d is %02x%02x%02x\n",
                            border ? "border" : "background", x, y, p[0], p[1], p[2]);
                return 1;
            }
        }
    }
    for (int row = 11; row <= 13; ++row) {
        for (int col = 0; col < 40; ++col) {
            const int which = col % 5;
            const uint8_t *glyph = nullptr;
            if (which == 0)
                glyph = kGlyphP;
            else if (which == 1)
                glyph = kGlyphA;
            else if (which != 4)
                glyph = kGlyphS;
            for (int by = 0; by < 8; ++by) {
                for (int bx = 0; bx < 8; ++bx) {
                    const bool on = glyph && (glyph[by] & (0x80u >> bx)) != 0;
                    const int x = col * 32 + bx * 4 + 3;
                    const int y = 60 + row * 24 + by * 3 + 1;
                    if (white_at(rgb, x, y) != on) {
                        std::printf("c64 PASS text mismatch row %d col %d bit %d,%d\n",
                                    row, col, bx, by);
                        return 1;
                    }
                }
            }
        }
    }
    return 0;
}

static int run(bool delayed) {
    Vc64_sim_top top;
    top.clk_sys = 0;
    top.pixel_clk = 0;
    top.reset = 1;
    top.keyboard_rows[0] = delayed ? 0 : 1u << 4;
    top.keyboard_rows[1] = 0;
    top.keyboard_rows[2] = 0;
    top.keyboard_rows[3] = 0;
    top.keyboard_rows[4] = 0;
    top.controller_buttons = delayed ? 0 : 1;
    top.disk_ready = !delayed;

    uint64_t sys_cycles = 0, cpu_cycles = 0, sprite_stalls = 0;
    auto tick = [&]() {
        top.clk_sys = 0;
        top.pixel_clk = 0;
        top.eval();
        cpu_cycles += top.cpu_enable;
        if(top.cpu_enable&&top.sprite_bus_owned&&!top.cpu_run_enable)++sprite_stalls;
        ++sys_cycles;
        if (cpu_cycles != sys_cycles * 985248 / 52224000) {
            std::printf("c64 PAL Phi2 count mismatch at system cycle %llu\n",
                        static_cast<unsigned long long>(sys_cycles));
            std::exit(1);
        }
        top.clk_sys = 1;
        top.pixel_clk = 1;
        top.eval();
    };
    for (int i = 0; i < 400; ++i) tick();
    top.reset = 0;

    bool white = false;
    bool red = false;
    bool sound = false;
    int joy_wait = 0;
    int key_wait = 0;
    // 0: diagnostic still running. 1: success seen, wait out this frame.
    // 2: wait for the next vsync. 3: record that frame's active pixels.
    int phase = 0;
    int pass_seen = -1;
    bool prev_vsync = false;
    std::vector<uint8_t> frame;
    int budget = 3000000;
    for (int i = 0; i < budget; ++i) {
        tick();
        // Model a library mount arriving after CPU release. Do not send the
        // operator's LOAD trigger until the media is ready, then delay A too.
        if (delayed && top.debug_stage == 7 && ++joy_wait == 100000) {
            top.disk_ready = 1;
            top.controller_buttons = 1;
        }
        if (delayed && top.debug_stage == 8 && ++key_wait == 100000)
            top.keyboard_rows[0] = 1u << 4;
        if (top.audio_sample != 0) sound = true;
        if (top.de) {
            if (top.red == 0xFF && top.green == 0xFF && top.blue == 0xFF) white = true;
            if (top.red == 0xCC && top.green == 0x00 && top.blue == 0x00) red = true;
        }
        if (pass_seen < 0 && top.debug_stage == 0xFF) {
            pass_seen = i;
            budget = i + 6 * kFrameCycles;
        }
        // Native frame publication has bounded PAL + HDMI presentation latency.
        // Allow two PAL frames to finish after the CPU paints PASS.
        if (phase == 0 && top.debug_stage == 0xFF && white && red && sound &&
            pass_seen >= 0 && i-pass_seen >= 2 * 19656ull * 52224000 / 985248) {
            if (delayed && (joy_wait < 100000 || key_wait < 100000)) {
                std::printf("c64 diagnostic skipped delayed input gate\n");
                return 1;
            }
            // $C000 is written after the PASS paint, but this frame may already
            // be partly drawn. Capture the next frame that starts afterwards.
            phase = 1;
            budget = i + 3 * kFrameCycles;
        }
        if (phase == 1) {
            if (!top.vsync)
                phase = 2;
        } else if (phase == 2) {
            if (top.vsync && !prev_vsync) {
                phase = 3;
                frame.clear();
                frame.reserve(static_cast<size_t>(kWidth * kHeight * 3));
            }
        } else if (phase == 3) {
            if (top.de) {
                frame.push_back(top.red);
                frame.push_back(top.green);
                frame.push_back(top.blue);
            }
            if (top.vsync && !prev_vsync && !frame.empty()) {
                if (check_pass_frame(frame))
                    return 1;
                if(!sprite_stalls){std::puts("c64 diagnostic never stalled a CPU read for sprite ownership");return 1;}
                std::printf("c64 sprite ownership stalled %llu CPU reads\n",static_cast<unsigned long long>(sprite_stalls));
                std::printf("c64 diagnostic passed after %d cycles (delayed=%d)\n", i, delayed);
                return 0;
            }
        }
        if (top.debug_stage != 0 && top.debug_stage != 0xFF && top.debug_error == 1) {
            std::printf("c64 diagnostic failed stage %u iec %u pc %04x at cycle %d\n",
                        top.debug_stage, top.debug_iec, top.debug_pc, i);
            return 1;
        }
        prev_vsync = top.vsync;
    }
    std::printf("c64 diagnostic timeout stage %u error %u iec %u white %d red %d sound %d capture %d\n",
                top.debug_stage, top.debug_error, top.debug_iec, white, red, sound, phase);
    return 1;
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (run(false)) return 1;
    return run(true);
}
