// SPDX-License-Identifier: GPL-2.0-or-later
// Host simulation of the Spectrum machine: diagnostic firmware, Kempston,
// keyboard matrix, probe card, tape pilot edges and the centred HDMI picture.
#include "Vspectrum_sim_top.h"
#include "verilated.h"

#include <cstdint>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "fes.spectrum machine: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

struct Machine {
    Vspectrum_sim_top dut;
    explicit Machine() {
        dut.clk_sys = 0;
        dut.pixel_clk = 0;
        dut.reset = 1;
        dut.matrix = 0xffffffffffULL & ~(1ULL << 5);  // A held
        dut.kempston = 0x11;                          // right + fire
        dut.unit_state = 3;
        dut.unit_size = 3;
        dut.eval();
    }
    void sys() {
        dut.clk_sys = 1;
        dut.eval();
        dut.clk_sys = 0;
        dut.eval();
    }
    void pixel() {
        dut.pixel_clk = 1;
        dut.eval();
        dut.pixel_clk = 0;
        dut.eval();
    }
};

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Machine machine;
    for (int i = 0; i < 32; ++i) machine.sys();
    machine.dut.reset = 0;

    int edges = 0;
    int matched = 0;
    uint32_t since = 0;
    bool seen_edge = false;
    bool cpu_ok = false;
    for (int i = 0; i < 2000000 && (!cpu_ok || matched < 3); ++i) {
        machine.sys();
        if (machine.dut.cpu_cycle) {
            if (seen_edge) since++;
        }
        const int level = machine.dut.ear;
        static int last = 0;
        if (level != last) {
            if (seen_edge && since == 2168) matched++;
            else if (seen_edge) matched = 0;
            seen_edge = true;
            since = 0;
            edges++;
            last = level;
        }
        if (machine.dut.sig8000 == 0xa5 && machine.dut.sig8001 == 0xbe &&
            machine.dut.sig8002 == 0x11 && machine.dut.sig8003 == 0xf5 &&
            machine.dut.sig8004 == 0x5a && machine.dut.border == 2)
            cpu_ok = true;
    }
    require(cpu_ok, "diagnostic signature was not reached");
    require(matched >= 3, "tape pilot edges were not 2168 T-states apart (edges " +
                              std::to_string(edges) + ")");

    bool border_ok = false;
    bool ink_ok = false;
    int border_seen = 0;
    int ink_seen = 0;
    // Two CTA-770 frames. The first origin sample can precede the attribute
    // prefetch; the border synchronizer settles during the first line.
    for (int i = 0; i < 1650 * 750 * 2 + 2000; ++i) {
        machine.pixel();
        if (!machine.dut.de) continue;
        if (machine.dut.pixel_x == 0 && machine.dut.pixel_y == 0 && ++border_seen == 2) {
            require(machine.dut.red == 0xcd && machine.dut.green == 0 && machine.dut.blue == 0,
                    "border pixel is not red (" + std::to_string(machine.dut.red) + "," +
                        std::to_string(machine.dut.green) + "," + std::to_string(machine.dut.blue) + ")");
            border_ok = true;
        }
        if (machine.dut.pixel_x == 128 && machine.dut.pixel_y == 72 && ++ink_seen == 2) {
            require(machine.dut.red == 0 && machine.dut.green == 0 && machine.dut.blue == 0,
                    "origin pixel is not black ink (" + std::to_string(machine.dut.red) + "," +
                        std::to_string(machine.dut.green) + "," + std::to_string(machine.dut.blue) + ")");
            ink_ok = true;
        }
    }
    require(border_ok && ink_ok, "HDMI raster did not pass the border and origin samples");
    std::cout << "fes.spectrum machine: ok\n";
    return 0;
}
