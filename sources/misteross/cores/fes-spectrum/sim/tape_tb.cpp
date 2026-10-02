// SPDX-License-Identifier: GPL-2.0-or-later
// Decode the production cassette player's EAR edges in Z80 T-states.
#include "Vspectrum_tape.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

namespace {
void require(bool condition, const std::string& message) {
    if (!condition) {
        std::cerr << "fes.spectrum tape: " << message << '\n';
        std::exit(EXIT_FAILURE);
    }
}

struct Tape {
    Vspectrum_tape dut;
    Tape() {
        dut.clk = 0;
        dut.cen = 0;
        dut.reset = 1;
        dut.unit_state = 0;
        dut.unit_size = 0;
        dut.write_addr = 0;
        dut.write_data = 0;
        dut.write_enable = 0;
        clock();
    }
    void clock() {
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    }
    // Leave a system clock for the synchronous tape RAM between CPU enables.
    bool tick() {
        dut.cen = 1;
        clock();
        const bool level = dut.ear;
        dut.cen = 0;
        clock();
        return level;
    }
    void load(const std::vector<uint8_t>& image) {
        dut.unit_state = 0;
        clock();
        for (size_t i = 0; i < image.size(); ++i) {
            dut.write_addr = i;
            dut.write_data = image[i];
            dut.write_enable = 1;
            clock();
        }
        dut.write_enable = 0;
        dut.unit_size = image.size();
        dut.unit_state = 3;
        dut.reset = 0;
    }
    unsigned wait_start(unsigned limit) {
        for (unsigned t = 1; t <= limit; ++t)
            if (tick()) return t;
        require(false, "missing block start");
        return 0;
    }
    void pulse(unsigned width, const std::string& label) {
        const bool before = dut.ear;
        for (unsigned t = 1; t < width; ++t) {
            if (tick() != before)
                require(false, label + " ended early at " + std::to_string(t));
        }
        require(tick() != before, label + " did not end at " + std::to_string(width));
    }
    void block(const std::vector<uint8_t>& data) {
        const unsigned pilots = (data[0] & 0x80) ? 3223 : 8063;
        for (unsigned p = 0; p < pilots; ++p) pulse(2168, "pilot");
        pulse(667, "sync 1");
        pulse(735, "sync 2");
        for (size_t i = 0; i < data.size(); ++i) {
            for (int bit = 7; bit >= 0; --bit) {
                const unsigned width = (data[i] & (1 << bit)) ? 1710 : 855;
                for (unsigned half = 0; half < 2; ++half)
                    pulse(width, "byte " + std::to_string(i) + " bit " +
                                     std::to_string(bit) + " half " + std::to_string(half));
            }
        }
        require(!dut.ear, "pause did not settle low");
    }
};

std::vector<uint8_t> tap(const std::vector<uint8_t>& data) {
    std::vector<uint8_t> image{static_cast<uint8_t>(data.size()),
                               static_cast<uint8_t>(data.size() >> 8)};
    image.insert(image.end(), data.begin(), data.end());
    return image;
}
}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Tape tape;
    // Distinct adjacent bytes expose a stretched pulse or stale RAM prefetch.
    for (uint8_t flag : {0xff, 0x80, 0x7f, 0x00}) {
        const std::vector<uint8_t> data{flag, 0x81, 0x5a,
                                       static_cast<uint8_t>(flag ^ 0x81 ^ 0x5a)};
        tape.load(tap(data));
        tape.wait_start(16);
        tape.block(data);
    }

    // Replay a second block after the standard pause; reject a partial tail.
    const std::vector<uint8_t> data{0xff, 0x00, 0xff};
    auto image = tap(data);
    const auto second = tap(data);
    image.insert(image.end(), second.begin(), second.end());
    image.insert(image.end(), {0x02, 0x00, 0xff});
    tape.load(image);
    tape.wait_start(16);
    tape.block(data);
    const unsigned pause = tape.wait_start(3500010);
    require(pause >= 3500000, "inter-block pause was shorter than one second");
    tape.block(data);
    for (unsigned t = 0; t < 3500010; ++t)
        require(!tape.tick(), "partial final block was played");

    // Eject during a pilot, then replace it with a different complete block.
    tape.load(tap({0xff, 0xff}));
    tape.wait_start(16);
    tape.dut.unit_state = 0;
    require(!tape.tick(), "eject did not silence EAR");
    tape.load(tap(data));
    tape.wait_start(16);
    tape.block(data);
    std::cout << "fes.spectrum tape: ok\n";
}
