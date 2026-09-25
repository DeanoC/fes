// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsgm_ay.h"
#include "verilated.h"

#include <algorithm>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <set>

static void require(bool ok, const char *why) {
    if (!ok) { std::cerr << why << '\n'; std::exit(EXIT_FAILURE); }
}
struct Driver {
    Vsgm_ay dut;
    void tick() {
        dut.clk = 0; dut.eval();
        dut.clk = 1; dut.eval();
        dut.clk = 0; dut.eval();
    }
    void select(uint8_t address) {
        dut.write_data = address;
        dut.address_write = 1; tick(); dut.address_write = 0;
    }
    void write(uint8_t address, uint8_t value) {
        select(address);
        dut.write_data = value;
        dut.data_write = 1; tick(); dut.data_write = 0;
    }
    uint8_t read(uint8_t address) {
        select(address);
        dut.data_read = 1; dut.eval();
        const uint8_t result = dut.read_data;
        dut.data_read = 0;
        return result;
    }
    int16_t sample() { return static_cast<int16_t>(dut.sample_signed); }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Driver d;
    d.dut.clk = 0; d.dut.reset = 1;
    d.dut.address_write = 0; d.dut.data_write = 0;
    d.dut.data_read = 0; d.dut.write_data = 0;
    d.tick(); d.dut.reset = 0;
    require(d.sample() == 0, "AY reset sample is not silent");
    for (unsigned reg = 0; reg < 16; ++reg)
        require(d.read(reg) == 0, "AY register reset value");
    d.write(0, 2); d.write(1, 0); d.write(6, 1);
    d.write(7, 0x3e); d.write(8, 0x0f);
    d.write(11, 1); d.write(12, 0); d.write(13, 9);
    for (auto item : {std::pair<int,int>{0,2}, {1,0}, {6,1}, {7,0x3e},
                      {8,0x0f}, {11,1}, {12,0}, {13,9}})
        require(d.read(item.first) == item.second, "AY register readback");
    int transitions = 0;
    int last_sign = d.sample() > 0;
    for (unsigned i = 0; i < 6000; ++i) {
        d.tick();
        int sign = d.sample() > 0;
        if (sign != last_sign) { ++transitions; last_sign = sign; }
    }
    // At 1.7897725 MHz / 8, period 2 toggles every 16 chip clocks:
    // about 12-13 sign changes in 6000 system-clock ticks.
    if (transitions < 11 || transitions > 14)
        std::cerr << "AY tone transitions in 6000 system ticks: " << transitions << '\n';
    require(transitions >= 11 && transitions <= 14, "AY tone A frequency/phase");

    // With tone and noise gates both disabled the envelope alone controls A.
    d.write(7, 0x3f); d.write(8, 0x10); d.write(13, 9);
    d.tick(); // PCM is registered after the shape write.
    int16_t peak = d.sample();
    for (unsigned i = 0; i < 1000; ++i) d.tick();
    int16_t early = d.sample();
    for (unsigned i = 0; i < 8000; ++i) d.tick();
    int16_t held = d.sample();
    if (!(peak > early && early > held))
        std::cerr << "AY envelope samples peak=" << peak << " early=" << early
                  << " held=" << held << '\n';
    require(peak > early && early > held, "AY envelope did not descend");
    require(held == 0, "AY envelope hold did not reach zero");

    // The read port must not expose live sound state while counters advance.
    d.select(13); d.dut.data_read = 1;
    for (unsigned i = 0; i < 2000; ++i) {
        d.tick();
        require(d.dut.read_data == 9, "AY read changed with sound counters");
    }
    d.dut.data_read = 0;

    d.write(7, 0x37); d.write(8, 0x0f); d.write(6, 1);
    std::set<int16_t> noise_samples;
    for (unsigned i = 0; i < 55000; ++i) {
        d.tick(); noise_samples.insert(d.sample());
    }
    require(noise_samples.size() >= 2, "AY noise channel is static");
    d.dut.reset = 1; d.tick(); d.dut.reset = 0;
    require(d.sample() == 0 && d.read(8) == 0, "AY reset did not clear audio and registers");
    std::cout << "Coleco SGM AY register/tone/noise/envelope probe passed\n";
}
