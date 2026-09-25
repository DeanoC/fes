// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsgm_audio_path.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

static void require(bool ok, const char *why) {
    if (!ok) { std::cerr << why << '\n'; std::exit(EXIT_FAILURE); }
}
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vsgm_audio_path d;
    d.source_clk = 0; d.audio_clk = 0; d.locked = 0; d.hold = 0;
    d.sn_sample = 0; d.ay_sample = 0; d.eval();
    for (auto pair : {std::pair<int,int>{0,0}, {1234,0}, {-1234,0},
                      {32000,2000}, {-32000,-2000}, {1000,-2000}}) {
        d.sn_sample = uint16_t(pair.first); d.ay_sample = uint16_t(pair.second); d.eval();
        int expected = pair.first + pair.second;
        if (expected > 32767) expected = 32767;
        if (expected < -32768) expected = -32768;
        require(int16_t(d.mixed_sample) == expected, "SGM audio mix/saturation");
    }
    d.locked = 1; d.ay_sample = 0;
    unsigned frames = 0;
    bool old_sclk = 0, old_lrclk = 0;
    for (unsigned t = 0; t < 300000; ++t) {
        if (t % 3 == 0) {
            d.source_clk = !d.source_clk;
            if (d.source_clk) d.sn_sample += 977;
        }
        if (t % 13 == 0) d.audio_clk = !d.audio_clk;
        d.eval();
        require(d.sclk == d.reference_sclk && d.lrclk == d.reference_lrclk,
                "SGM mix changed I2S framing");
        require(d.mixed_data == d.reference_data,
                "zero SGM sample changed serialized I2S bytes");
        if (!old_sclk && d.sclk) {
            if (d.lrclk != old_lrclk) ++frames;
            old_lrclk = d.lrclk;
        }
        old_sclk = d.sclk;
    }
    require(frames > 20, "I2S comparison did not cover full frames");
    d.ay_sample = 5000;
    unsigned changed_bits = 0;
    for (unsigned t = 0; t < 100000; ++t) {
        if (t % 3 == 0) d.source_clk = !d.source_clk;
        if (t % 13 == 0) d.audio_clk = !d.audio_clk;
        d.eval();
        if (d.mixed_data != d.reference_data) ++changed_bits;
    }
    require(changed_bits > 100, "AY plus SN did not reach serialized I2S");
    std::cout << "Coleco SGM saturated mix and vacant I2S equivalence passed\n";
}
