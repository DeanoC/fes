// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_audio_i2s.h"
#include <iostream>
int main() {
    Vfes_audio_i2s d;
    d.clk = 0; d.reset = 1; d.mute = 0;
    d.left_sample = 0x1234; d.right_sample = 0x5678; d.eval();
    d.clk = 1; d.eval(); d.clk = 0; d.eval(); d.reset = 0; d.eval();
    bool old_bit = d.sclk, old_word = d.lrclk;
    for (unsigned cycle = 0; cycle < 1024; ++cycle) {
        // SDC numbers both edges: the first rising source edge is 1.
        d.clk = 1; d.eval();
        if (d.sclk != old_bit)
            std::cout << "bit_clock " << (2 * cycle + 1) << ' ' << unsigned(d.sclk) << '\n';
        if (d.lrclk != old_word) {
            if (d.sclk || !old_bit) return 2;
            std::cout << "word_clock " << (2 * cycle + 1) << ' ' << unsigned(d.lrclk) << '\n';
        }
        old_bit = d.sclk; old_word = d.lrclk; d.clk = 0; d.eval();
    }
}
