// SPDX-License-Identifier: GPL-2.0-or-later
// Independent AY-3-8912 output checks. GI's AY-3-8910/8912 data manual,
// sections Tone Generator, Noise Generator, Mixer and Envelope Generator:
// https://computers.baffa.tec.br/pages/datasheet/AY-3-8910-data_manual.pdf
// Period checks use published frequency equations; envelope expectations are
// explicit shape tables, rather than a copy of the RTL counter machinery.
#include "Vzonx_ay.h"
#include "verilated.h"
#include <array>
#include <cmath>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

// GI's Amplitude Control figure gives nominal logarithmic 3 dB steps.
// Quantize that relationship independently of the RTL's lookup table,
// reserving 85 units per channel for the unsigned 8-bit mono transport.
static const std::array<int, 16> levels = [] {
    std::array<int, 16> result{};
    for (int level = 1; level < 16; ++level)
        result[level] = std::lround(85 * std::pow(10, (level - 15) * 3 / 20.0));
    return result;
}();

struct Test {
    Vzonx_ay dut;
    void require(bool ok, const std::string &message) {
        if (!ok) { std::cerr << "AY: " << message << '\n'; std::exit(1); }
    }
    void tick(bool ce = false) {
        dut.chip_ce = ce;
        dut.clk = 0; dut.eval(); dut.clk = 1; dut.eval();
    }
    void reset() {
        dut.address_write = 0; dut.data_write = 0; dut.data = 0;
        dut.reset_n = 0; tick(); dut.reset_n = 1; tick();
        require(dut.pcm == 0, "reset must mute all channels");
    }
    void select(int reg) {
        dut.data = reg; dut.address_write = 1; tick();
        dut.address_write = 0; tick();
    }
    void data(int value) {
        dut.data = value; dut.data_write = 1; tick();
        dut.data_write = 0; tick();
    }
    void write(int reg, int value) { select(reg); data(value); }
    void cycles(int n) { for (int i = 0; i < n; ++i) tick(true); }
    void constant_channel(int channel, int volume) {
        write(7, 63); write(8 + channel, volume);
    }
};

static void registers_and_levels(Test &t) {
    const std::array<int, 16> mask =
        {255,15,255,15,255,15,31,255,31,31,31,255,255,15,255,0};
    t.reset();
    for (int reg = 0; reg < 16; ++reg) {
        t.write(reg, 255);
        t.require(t.dut.read_data == mask[reg], "register mask R" + std::to_string(reg));
    }
    t.write(0, 42); t.select(0x10); t.data(99); t.select(0);
    t.require(t.dut.read_data == 42, "invalid address must disable writes");
    t.data(71);
    t.require(t.dut.read_data == 71, "valid address must restore writes");
    for (int ch = 0; ch < 3; ++ch) {
        t.reset();
        for (int volume = 0; volume < 16; ++volume) {
            t.constant_channel(ch, volume);
            t.require(t.dut.pcm == levels[volume], "volume/channel table ch=" +
                      std::to_string(ch) + " level=" + std::to_string(volume) +
                      " expected=" + std::to_string(levels[volume]) +
                      " actual=" + std::to_string(t.dut.pcm));
        }
    }
    t.reset(); t.write(7, 63);
    t.write(8, 15); t.write(9, 14); t.write(10, 13);
    t.require(t.dut.pcm == levels[15] + levels[14] + levels[13], "independent three-channel sum");
    t.write(9, 15); t.write(10, 15);
    t.require(t.dut.pcm == 255, "full three-channel sum must not wrap");
}

static void tones(Test &t) {
    for (int ch = 0; ch < 3; ++ch) {
        for (int period : {0,1,2,17,255,256,257,2049,4095}) {
            t.reset(); t.write(ch * 2, period & 255);
            t.write(ch * 2 + 1, period >> 8); t.write(8 + ch, 15);
            t.write(7, 63 & ~(1 << ch));
            const int half = 8 * (period ? period : 1);
            int last = t.dut.pcm, previous_edge = -1, edges = 0;
            for (int cycle = 1; cycle <= half * 5; ++cycle) {
                t.tick(true);
                int value = t.dut.pcm;
                t.require(value == 0 || value == 85, "tone output level");
                if (value != last) {
                    if (previous_edge >= 0)
                        t.require(cycle - previous_edge == half,
                                  "tone half-period ch=" + std::to_string(ch) +
                                  " period=" + std::to_string(period));
                    previous_edge = cycle; ++edges; last = value;
                }
            }
            t.require(edges >= 4, "tone must oscillate");
            const int held = t.dut.pcm;
            for (int i = 0; i < half * 2; ++i) t.tick(false);
            t.require(t.dut.pcm == held, "chip_ce must gate generator clocks");
        }
    }
    // Different periods produce a recognisable sum, not a shared oscillator.
    t.reset(); t.write(0, 1); t.write(2, 3); t.write(4, 7);
    t.write(8, 15); t.write(9, 14); t.write(10, 13); t.write(7, 56);
    std::array<bool, 256> seen{};
    for (int i = 0; i < 8 * 2 * 3 * 7; ++i) { t.tick(true); seen[t.dut.pcm] = true; }
    for (int mask = 0; mask < 8; ++mask) {
        int value = 0;
        for (int ch = 0; ch < 3; ++ch)
            if (mask & (1 << ch)) value += levels[15-ch];
        t.require(seen[value], "independent mixed tones missing " + std::to_string(value));
    }
}

static int envelope_level(int shape, int step) {
    // GI figure 17: 0..3 descend then zero; 4..7 ascend then zero.
    if (shape < 4) return step < 16 ? 15 - step : 0;
    if (shape < 8) return step < 16 ? step : 0;
    switch (shape) {
    case 8: return 15 - step % 16;
    case 9: return step < 16 ? 15 - step : 0;
    case 10: return (step / 16) % 2 ? step % 16 : 15 - step % 16;
    case 11: return step < 16 ? 15 - step : 15;
    case 12: return step % 16;
    case 13: return step < 16 ? step : 15;
    case 14: return (step / 16) % 2 ? 15 - step % 16 : step % 16;
    default: return step < 16 ? step : 0;
    }
}

static std::vector<int> mixer_trace(Test &t, int mixer, int period) {
    t.reset(); t.write(0,3); t.write(6,period); t.write(7,mixer); t.write(8,15);
    std::vector<int> samples;
    for (int i = 0; i < 16 * 31 * 256; ++i) {
        t.tick(true); samples.push_back(t.dut.pcm);
    }
    return samples;
}

static void noise_and_mixer(Test &t) {
    // Polynomial x^17+x^14+1, expressed as a right shift with taps 0 and 3.
    // Reset seed 1 follows MAME's ay8910_device::ay8910_reset_ym():
    // https://github.com/mamedev/mame/blob/master/src/devices/sound/ay8910.cpp
    // This pins the reference model's reset phase, not a measured chip phase.
    // Checking complete sample windows catches a mistaken /8 divider, a
    // shortened five-bit period, a different polynomial and stuck noise.
    for (int period : {0,1,2,17,31}) {
        t.reset(); t.write(6,period); t.write(7,55); t.write(8,15);
        unsigned sequence = 1;
        int interval = 16 * (period ? period : 1);
        for (int step = 0; step < 512; ++step) {
            int expected = (sequence & 1) ? 85 : 0;
            t.require(t.dut.pcm == expected, "noise sequence step=" + std::to_string(step));
            t.cycles(interval - 1);
            t.require(t.dut.pcm == expected, "noise advanced before period elapsed");
            t.cycles(1);
            sequence = (sequence >> 1) | (((sequence ^ (sequence >> 3)) & 1) << 16);
        }
    }
    auto tone = mixer_trace(t,62,17);
    auto noise = mixer_trace(t,55,17);
    auto combined = mixer_trace(t,54,17);
    for (size_t i = 0; i < tone.size(); ++i)
        t.require(combined[i] == ((tone[i] && noise[i]) ? 85 : 0), "tone/noise mixer AND");
    t.reset(); t.write(6,1); t.write(7,7);
    t.write(8,15); t.write(9,14); t.write(10,13);
    const int sum = levels[15] + levels[14] + levels[13];
    bool low = false, high = false;
    for (int i = 0; i < 16 * 512; ++i) {
        t.tick(true);
        t.require(t.dut.pcm == 0 || t.dut.pcm == sum, "noise source must be shared across channels");
        low |= t.dut.pcm == 0; high |= t.dut.pcm == sum;
    }
    t.require(low && high, "noise must produce both levels");
}

static void envelopes(Test &t) {
    for (int period : {0,1,2,257}) {
        const int interval = period ? period * 16 : 8;
        for (int shape = 0; shape < 16; ++shape) {
            t.reset(); t.write(7,63); t.write(8,16);
            t.write(11, period & 255); t.write(12, period >> 8);
            t.write(13, shape);
            for (int step = 0; step < 65; ++step) {
                const int expected = levels[envelope_level(shape,step)];
                t.require(t.dut.pcm == expected, "envelope shape=" + std::to_string(shape) +
                          " step=" + std::to_string(step) + " period=" + std::to_string(period));
                t.cycles(interval - 1);
                t.require(t.dut.pcm == expected, "envelope advanced before period elapsed");
                t.cycles(1);
            }
            t.write(13,shape);
            t.require(t.dut.pcm == levels[envelope_level(shape,0)], "R13 repeat write restarts envelope");
            t.cycles(interval-1);
            t.require(t.dut.pcm == levels[envelope_level(shape,0)], "R13 restart resets envelope phase");
        }
    }
    // Exercise bit 15 without replaying every shape for a million-cycle step.
    for (int period : {32769,65535}) {
        t.reset(); t.write(7,63); t.write(8,16);
        t.write(11,period & 255); t.write(12,period >> 8); t.write(13,12);
        t.cycles(period * 16 - 1);
        t.require(t.dut.pcm == 0, "full-width envelope period advanced early");
        t.cycles(1);
        t.require(t.dut.pcm == 1, "full-width envelope period did not advance");
    }
    // The one envelope supplies B and C while A keeps its fixed volume.
    t.reset(); t.write(7,63); t.write(8,15); t.write(9,16); t.write(10,16);
    t.write(11,1); t.write(13,12);
    for (int step = 0; step < 33; ++step) {
        t.require(t.dut.pcm == 85 + 2 * levels[step % 16], "shared envelope and fixed-volume mix");
        t.cycles(16);
    }
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc,argv);
    Test t; registers_and_levels(t); tones(t); noise_and_mixer(t); envelopes(t);
    std::cout << "AY reference: registers, volumes, tones, noise, mixer and all envelope shapes passed\n";
}
