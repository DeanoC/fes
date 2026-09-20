// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsms_psg.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES SMS PSG: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

void tick(Vsms_psg &dut) {
    dut.clk = 1;
    dut.eval();
    dut.clk = 0;
    dut.eval();
}

void io_write(Vsms_psg &dut, uint8_t port, uint8_t value) {
    dut.cpu_ce = 1;
    dut.cpu_iorq_n = 0;
    dut.cpu_wr_n = 0;
    dut.cpu_a = port;
    dut.cpu_din = value;
    tick(dut);
    dut.cpu_ce = 0;
    dut.cpu_iorq_n = 1;
    dut.cpu_wr_n = 1;
    for (unsigned cycle = 0; cycle != 4; ++cycle) tick(dut);
}

void chip_cycles(Vsms_psg &dut, unsigned count) {
    dut.ce = 1;
    for (unsigned cycle = 0; cycle != count; ++cycle) tick(dut);
    dut.ce = 0;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vsms_psg dut;
    dut.clk = 0;
    dut.reset = 1;
    dut.ce = 0;
    dut.cpu_ce = 0;
    dut.cpu_iorq_n = 1;
    dut.cpu_wr_n = 1;
    dut.cpu_a = 0;
    dut.cpu_din = 0;
    dut.eval();
    for (unsigned cycle = 0; cycle != 8; ++cycle) tick(dut);
    dut.reset = 0;

    require(dut.tone0_atten == 0xF, "reset tone0 is not silent");
    require(dut.tone1_atten == 0xF, "reset tone1 is not silent");
    require(dut.tone2_atten == 0xF, "reset tone2 is not silent");
    require(dut.noise_atten == 0xF, "reset noise is not silent");
    require(dut.sample == 0, "reset mix must be zero");

    io_write(dut, 0x7F, 0x80);  // latch tone0 freq, low nibble 0
    io_write(dut, 0x7E, 0x10);  // high 6 bits => period 256
    io_write(dut, 0x7F, 0x90);  // tone0 atten 0
    io_write(dut, 0x7F, 0xBF);
    io_write(dut, 0x7F, 0xDF);
    io_write(dut, 0x7F, 0xFF);

    require(dut.tone0_period == 256, "tone0 period 256");
    require(dut.tone0_atten == 0, "tone0 max volume");
    require(dut.tone1_atten == 0xF, "tone1 silent");
    require(dut.tone2_atten == 0xF, "tone2 silent");
    require(dut.noise_atten == 0xF, "noise silent");

    chip_cycles(dut, 16 * 256 * 8);
    require(int16_t(dut.sample) == (dut.tone0 ? 8191 : -8191),
            "tone0 sample amplitude");
    require(int16_t(dut.sample) != 0, "audible mix after programming");

    dut.reset = 1;
    tick(dut);
    dut.reset = 0;
    io_write(dut, 0x7F, 0x80);
    io_write(dut, 0x7E, 0x10);
    io_write(dut, 0x7F, 0x90);
    io_write(dut, 0x7F, 0xBF);
    io_write(dut, 0x7F, 0xDF);
    io_write(dut, 0x7F, 0xFF);
    unsigned flips = 0;
    int previous = dut.tone0;
    for (unsigned cycle = 0; cycle != 16 * 256 * 6; ++cycle) {
        dut.ce = 1;
        tick(dut);
        if (dut.tone0 != previous) {
            ++flips;
            previous = dut.tone0;
        }
    }
    dut.ce = 0;
    require(flips >= 4 && flips <= 8, "tone0 square-wave period");

    io_write(dut, 0x7E, 0x9F);  // latch tone0 silent via 0x7E
    require(dut.tone0_atten == 0xF, "0x7E write must update volume");
    require(int16_t(dut.sample) == 0, "silent after atten 15");

    io_write(dut, 0x7F, 0x80);  // period 0: Sega reload is 1024, not truncated 0
    io_write(dut, 0x7F, 0x00);
    require(dut.tone0_period == 0, "tone0 period 0");
    unsigned zero_flips = 0;
    previous = dut.tone0;
    for (unsigned cycle = 0; cycle != 16 * 200; ++cycle) {
        dut.ce = 1;
        tick(dut);
        if (dut.tone0 != previous) {
            ++zero_flips;
            previous = dut.tone0;
        }
    }
    dut.ce = 0;
    require(zero_flips <= 2, "period 0 must count 1024, not toggle every prescaler");

    io_write(dut, 0x7F, 0x81);  // tone0 period low nibble 1, then data 0 => 1
    io_write(dut, 0x7F, 0x00);
    require(dut.tone0_period == 1, "tone0 period 1");

    std::cout << "FES SMS PSG checks passed\n";
    return 0;
}
