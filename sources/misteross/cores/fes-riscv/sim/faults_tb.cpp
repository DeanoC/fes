// SPDX-License-Identifier: GPL-2.0-or-later
// fes_riscv_system bus-fault contract: only RAM, the framebuffer and the ten
// I/O words respond; every other access is an access fault. The program is
// cores/fes-riscv/sim/fault_test.S, assembled by the firmware assembler.
#include "Vfes_riscv_system.h"
#include "Vfes_riscv_system___024root.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <iostream>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_riscv_system dut;
    dut.pixel_clk = 0; dut.exec_reset = 1; dut.buttons = 0;
    dut.eval();
    auto tick = [&]() { dut.pixel_clk = 1; dut.eval(); dut.pixel_clk = 0; dut.eval(); };
    for (int i = 0; i < 8; ++i) tick();
    dut.exec_reset = 0;
    uint64_t cycles = 0;
    auto &root = *dut.rootp;
    while ((root.fes_riscv_system__DOT__mtimecmp & 0xffffffffull) == 0xffffffffull) {
        tick();
        if (++cycles > 200000) { std::cerr << "fault test did not finish\n"; return EXIT_FAILURE; }
    }
    for (int i = 0; i < 8; ++i) tick();
    uint64_t result = root.fes_riscv_system__DOT__mtimecmp;
    uint32_t faults = uint32_t(result), id = uint32_t(result >> 32);
    if (faults != 6 || id != 0x52495343u) {
        std::fprintf(stderr, "faults taken %u (expected 6), identification 0x%08x (expected 0x52495343)\n", faults, id);
        return EXIT_FAILURE;
    }
    std::cout << "fes_riscv_system: unmapped I/O, framebuffer overrun and wild addresses fault; "
                 "mapped words do not (" << cycles << " cycles)\n";
    return 0;
}
