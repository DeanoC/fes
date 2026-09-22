// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vrom_init_window.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <string>
#include <vector>

namespace {
constexpr unsigned kBasicBytes = 8192;

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES ZX81 ROM: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

std::vector<uint8_t> load_hex(const std::string &path) {
    std::ifstream input(path);
    if (!input)
        fail("cannot open " + path);
    std::vector<uint8_t> bytes;
    std::string line;
    while (std::getline(input, line)) {
        if (line.empty())
            continue;
        bytes.push_back(static_cast<uint8_t>(std::stoul(line, nullptr, 16)));
    }
    return bytes;
}

void tick(Vrom_init_window &dut) {
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 2)
        fail("usage: Vrom_init_window zx8x.hex");
    const std::vector<uint8_t> image = load_hex(argv[1]);
    if (image.size() < kBasicBytes || image[0] != 0xd3 || image[1] != 0xfd)
        fail("BASIC image does not start with OUT (FD),A");

    Vrom_init_window window;
    window.clk_sys = 0;
    window.addr = 0;
    window.eval();
    tick(window);
    for (unsigned addr = 0; addr < kBasicBytes; ++addr) {
        window.addr = addr;
        window.eval();
        if (window.data != image[addr]) {
            std::cerr << "ROM[" << std::hex << addr << "] expected "
                      << static_cast<unsigned>(image[addr]) << " got "
                      << static_cast<unsigned>(window.data) << std::dec << '\n';
            return 1;
        }
    }
    std::cout << "PASS: ZX81 BASIC 8KiB through the machine ROM port\n";
    return 0;
}
