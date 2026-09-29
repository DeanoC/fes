// SPDX-License-Identifier: MIT
// Boots the open C64 diagnostic and requires the CPU, both cartridge sockets,
// the joystick, the keyboard, a SID sample, the D64 LOAD and both border and
// text pixels.
#include "Vc64_sim_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>

int main() {
    Vc64_sim_top top;
    top.clk_sys = 0;
    top.pixel_clk = 0;
    top.reset = 1;
    top.keyboard_rows[0] = 1u << 4;
    top.keyboard_rows[1] = 0;
    top.keyboard_rows[2] = 0;
    top.keyboard_rows[3] = 0;
    top.keyboard_rows[4] = 0;
    top.controller_buttons = 1;

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
    constexpr int limit = 3000000;
    for (int i = 0; i < limit; ++i) {
        tick();
        if (top.audio_sample != 0) sound = true;
        if (top.de) {
            if (top.red == 0xFF && top.green == 0xFF && top.blue == 0xFF) white = true;
            if (top.red == 0xCC && top.green == 0x00 && top.blue == 0x00) red = true;
        }
        if (top.debug_stage == 0xFF && white && red && sound) {
            std::printf("c64 diagnostic passed after %d cycles\n", i);
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
