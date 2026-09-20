// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsms_hdmi_i2s.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

namespace {

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES SMS HDMI I2S: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

void tick(Vsms_hdmi_i2s &dut) {
    dut.pixel_clk = 1;
    dut.eval();
    dut.pixel_clk = 0;
    dut.eval();
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vsms_hdmi_i2s dut;
    dut.pixel_clk = 0;
    dut.sample = 0x1234;
    dut.eval();

    int prev_sclk = dut.sclk;
    int prev_lrclk = dut.lrclk;
    std::vector<int> left_bits;
    std::vector<int> right_bits;
    bool in_left = false;
    unsigned frames = 0;
    unsigned period_sum = 0;
    unsigned period_cycles = 0;
    bool counting = false;

    for (unsigned cycle = 0; cycle != 40000; ++cycle) {
        tick(dut);
        const bool sclk_fall = prev_sclk && !dut.sclk;
        const bool lrclk_fall = prev_lrclk && !dut.lrclk;
        const bool lrclk_rise = !prev_lrclk && dut.lrclk;
        if (counting)
            ++period_cycles;
        if (lrclk_fall) {
            if (counting) {
                period_sum += period_cycles;
                ++frames;
                period_cycles = 0;
            } else {
                counting = true;
                period_cycles = 0;
            }
            in_left = true;
            left_bits.clear();
        }
        if (lrclk_rise) {
            in_left = false;
            right_bits.clear();
        }
        if (sclk_fall) {
            if (in_left)
                left_bits.push_back(dut.i2s & 1);
            else
                right_bits.push_back(dut.i2s & 1);
        }
        prev_sclk = dut.sclk;
        prev_lrclk = dut.lrclk;
        if (frames >= 8 && left_bits.size() >= 17 && right_bits.size() >= 17)
            break;
    }

    require(left_bits.size() >= 17, "left I2S slot");
    require(right_bits.size() >= 17, "right I2S slot");
    uint16_t left = 0;
    uint16_t right = 0;
    for (int bit = 1; bit <= 16; ++bit) {
        left = uint16_t((left << 1) | left_bits[size_t(bit)]);
        right = uint16_t((right << 1) | right_bits[size_t(bit)]);
    }
    require(left == 0x1234, "left I2S word");
    require(right == 0x1234, "right I2S word");
    require(frames >= 8, "not enough LRCLK periods");
    // 8 LRCLK periods at 74.25 MHz / 48 kHz = 12375 pixel clocks.
    require(period_sum > 12300 && period_sum < 12450, "48 kHz LRCLK from pixel clock");

    unsigned mclk_flips = 0;
    int previous_mclk = dut.mclk;
    for (unsigned cycle = 0; cycle != 200; ++cycle) {
        tick(dut);
        if (dut.mclk != previous_mclk) {
            ++mclk_flips;
            previous_mclk = dut.mclk;
        }
    }
    require(mclk_flips >= 4, "MCLK must run");

    std::cout << "FES SMS HDMI I2S checks passed\n";
    return 0;
}
