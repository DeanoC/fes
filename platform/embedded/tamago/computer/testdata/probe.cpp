// SPDX-License-Identifier: GPL-3.0-or-later
// Host-only bus/observation adapter. The selected computer RTL is unmodified.
#include "Vfes_de25_computer.h"
#include "Vfes_de25_computer___024root.h"
#include "verilated.h"
#include <cstdint>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>

static Vfes_de25_computer top;
static uint64_t transitions;
static uint16_t previous_address;
static void tick(unsigned count = 1) {
    while (count--) {
        top.clk = 0; top.pixel_clk = 0; top.eval();
        top.clk = 1; top.pixel_clk = 1; top.eval();
        if (top.rootp->fes_de25_computer__DOT__cpu_addr != previous_address) ++transitions;
        previous_address = top.rootp->fes_de25_computer__DOT__cpu_addr;
    }
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    top.reset_n = 0;
    top.read = top.write = top.address = 0;
    top.byteenable = 15;
    top.expansion_running = 0;
    top.diagnostic_address = 0;
    top.expansion_response = 0;
    tick(4); top.reset_n = 1; tick(10);
    for (std::string line; std::getline(std::cin, line);) {
        std::istringstream in(line);
        std::string command;
        uint32_t address = 0, value = 0;
        in >> command >> std::hex;
        if (command == "read" || command == "write") {
            in >> address;
            if (command == "write") in >> value;
            if (!in || address > 31 || address % 4) return 2;
            top.address = address;
            top.writedata = value;
            top.read = command == "read";
            top.write = command == "write";
            tick();
            bool valid = !top.waitrequest && (top.read ? top.readdatavalid : top.writeresponsevalid);
            uint32_t result = top.read ? top.readdata : 0;
            uint32_t response = top.response;
            top.read = top.write = 0; tick();
            std::cout << "R " << (valid && response == 0 ? "1 " : "0 ") << std::hex << result << std::endl;
        } else if (command == "advance") {
            in >> value;
            if (!in || value > 3000000) return 2;
            tick(value);
            std::cout << "R 1 0" << std::endl;
        } else if (command == "reset") {
            top.reset_n = 0; tick(2); top.reset_n = 1; tick(10);
            std::cout << "R 1 0" << std::endl;
        } else if (command == "inspect") {
            auto *r = top.rootp;
            std::cout << "R 1 " << std::hex
                << unsigned(r->fes_de25_computer__DOT__exec_reset) << ' '
                << transitions << ' '
                << (r->fes_de25_computer__DOT__keyboard_rows[0] & 65535) << ' '
                << (r->fes_de25_computer__DOT__keyboard_rows[4] & 65535) << ' '
                << unsigned(r->fes_de25_computer__DOT__unit0_state) << std::endl;
        } else if (command == "byte") {
            in >> address;
            if (!in || address >= 16384) return 2;
            std::cout << "R 1 " << std::hex << unsigned(top.rootp->fes_de25_computer__DOT__tape__DOT__image[address]) << std::endl;
        } else {
            return 2;
        }
        if (Verilated::gotFinish()) return 3;
    }
    top.final();
}
