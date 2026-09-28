// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_computer_gp.h"
#include "verilated.h"

#include <cstdlib>
#include <iostream>

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_computer_gp dut;
    dut.clk = 0;
    dut.gpo = 0;
    dut.media_addr = 0;
    dut.eval();
    if (!dut.exec_reset) return EXIT_FAILURE;
    // Identity opcode 1, capability index 7, toggled request bit.
    dut.gpo = 0x81070000u;
    bool answered = false;
    for (unsigned i = 0; i < 12; ++i) {
        dut.clk = 1; dut.eval();
        dut.clk = 0; dut.eval();
        if ((uint32_t(dut.gpi) & 0x00800000u) != 0) {
            answered = true;
            break;
        }
    }
    if (!answered || (uint32_t(dut.gpi) & 0xffffu) != 0x13u) {
        std::cerr << "SG-1000 GP must advertise keyboard/video/audio only: 0x"
                  << std::hex << uint32_t(dut.gpi) << '\n';
        return EXIT_FAILURE;
    }
    std::cout << "SG-1000 ROM-linked GP identity mask 0x13 passed\n";
    return EXIT_SUCCESS;
}
