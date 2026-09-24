// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vdiagnostic_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

static void require(bool ok, const char* reason) {
    if (!ok) { std::cerr << reason << '\n'; std::exit(EXIT_FAILURE); }
}

static void tick(Vdiagnostic_machine& dut, const std::vector<uint8_t>& rom,
                 uint8_t& registered_data, unsigned& wait_ticks) {
    const uint32_t request = dut.bus_request;
    if ((request & 0xffff) == 0x2000 && !(request & (1u << 24)) &&
        !(request & (1u << 26)) && (dut.bus_response & (1u << 9)))
        ++wait_ticks;
    const uint16_t address = dut.media_addr;
    dut.media_data = registered_data;
    dut.clk_sys = 1; dut.eval();
    dut.clk_sys = 0; dut.eval();
    registered_data = address < rom.size() ? rom[address] : 0xff;
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    // LD A,5; LD (2001),A; LD A,(2000); LD (6000),A; HALT.
    const std::vector<uint8_t> program = {
        0x3e, 0x05, 0x32, 0x01, 0x20,
        0x3a, 0x00, 0x20, 0x32, 0x00, 0x60, 0x76
    };
    Vdiagnostic_machine dut;
    dut.clk_sys = 0; dut.reset = 1;
    dut.media_ready = 1; dut.media_size = program.size();
    dut.media_data = 0; dut.peek_addr = 0;
    dut.eval();
    uint8_t registered_data = 0;
    unsigned wait_ticks = 0;
    for (unsigned i = 0; i < program.size() + 32; ++i)
        tick(dut, program, registered_data, wait_ticks);
    dut.reset = 0;
    unsigned cycles = 0;
    for (; cycles < 200000 && dut.cpu_halt_n; ++cycles)
        tick(dut, program, registered_data, wait_ticks);
    require(cycles < 200000, "diagnostic CPU program did not HALT");
    dut.peek_addr = 0x6000;
    tick(dut, program, registered_data, wait_ticks);
    require(dut.peek_data == 0x5a, "CPU did not read diagnostic through socket");
    require(wait_ticks >= 16, "CPU read did not experience diagnostic WAIT");
    std::cout << "FES Coleco diagnostic CPU/socket probe passed (WAIT "
              << wait_ticks << " ticks)\n";
}
