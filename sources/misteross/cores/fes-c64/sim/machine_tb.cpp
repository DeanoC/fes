// SPDX-License-Identifier: MIT
// Boots the open C64 diagnostic and requires the CPU, both cartridge sockets,
// the joystick, the keyboard, a SID sample, the D64 LOAD and both border and
// text pixels. The firmware also requires CIA1 timer B IRQ and CIA2 timer A
// NMI through the CPU vectors, with SEI still set during the NMI check.
#include "Vc64_sim_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>

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

    auto tick = [&]() {
        top.clk_sys = 0;
        top.pixel_clk = 0;
        top.eval();
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
    constexpr int limit = 3000000;
    for (int i = 0; i < limit; ++i) {
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
        if (top.debug_stage == 0xFF && white && red && sound) {
            if (delayed && (joy_wait < 100000 || key_wait < 100000)) {
                std::printf("c64 diagnostic skipped delayed input gate\n");
                return 1;
            }
            std::printf("c64 diagnostic passed after %d cycles (delayed=%d)\n", i, delayed);
            return 0;
        }
        if (top.debug_stage != 0 && top.debug_stage != 0xFF && top.debug_error == 1) {
            std::printf("c64 diagnostic failed stage %u iec %u pc %04x at cycle %d\n",
                        top.debug_stage, top.debug_iec, top.debug_pc, i);
            return 1;
        }
    }
    std::printf("c64 diagnostic timeout stage %u error %u iec %u white %d red %d sound %d\n",
                top.debug_stage, top.debug_error, top.debug_iec, white, red, sound);
    return 1;
}

int main() {
    if (run(false)) return 1;
    return run(true);
}
