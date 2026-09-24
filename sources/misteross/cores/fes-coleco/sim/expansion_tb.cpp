// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

static void require(bool ok, const char* reason) {
    if (!ok) { std::cerr << reason << '\n'; std::exit(EXIT_FAILURE); }
}

static void tick(Vcoleco_machine& dut, const std::vector<uint8_t>& rom,
                 uint8_t& registered_data, bool attached, unsigned& wait_cycles) {
    const unsigned request = dut.bus_request;
    const uint16_t address = request & 0xffff;
    const bool mem_read = !(request & (1u << 24)) && !(request & (1u << 26));
    const bool io_read = !(request & (1u << 25)) && !(request & (1u << 26));
    uint16_t response = 0;
    if (attached) {
        // Claim every read deliberately: the shell must mask console-owned
        // BIOS, RAM, cartridge, VDP and controller regions.
        if (mem_read || io_read) response = (1u << 8) | 0xee;
        if (mem_read && address == 0x2000) {
            response = (1u << 8) | 0x5a;
            if (wait_cycles < 32) { response |= 1u << 9; ++wait_cycles; }
        }
        if (io_read && (address & 0xff) == 0x40) response = (1u << 8) | 0xa6;
    }
    dut.bus_response = response;
    const uint16_t media_address = dut.media_addr;
#ifdef FES_COLECO_OSS
    dut.media_data = registered_data;
#else
    dut.media_data = media_address < rom.size() ? rom[media_address] : 0xff;
#endif
    dut.clk_sys = 1; dut.eval();
    dut.clk_sys = 0; dut.eval();
#ifdef FES_COLECO_OSS
    registered_data = media_address < rom.size() ? rom[media_address] : 0xff;
#endif
}

static uint8_t peek(Vcoleco_machine& dut, uint16_t address,
                    const std::vector<uint8_t>& rom, uint8_t& registered_data,
                    bool attached, unsigned& wait_cycles) {
    dut.peek_addr = address;
    tick(dut, rom, registered_data, attached, wait_cycles);
    return dut.peek_data;
}

static void run(bool attached) {
    // JP 8000 is the open reset shim. The program tests a vacant expansion
    // memory window, an unclaimed I/O port, and protected BIOS/controller reads.
    const std::vector<uint8_t> program = {
        0x3a, 0x00, 0x20, 0x32, 0x00, 0x60,
        0xdb, 0x40, 0x32, 0x01, 0x60,
        0x3a, 0x00, 0x00, 0x32, 0x02, 0x60,
        0xdb, 0xfc, 0x32, 0x03, 0x60,
        0x76
    };
    Vcoleco_machine dut;
    dut.clk_sys = 0; dut.reset = 1;
    dut.controller_buttons = 0; dut.controller_keypad = 0;
    dut.media_ready = 1; dut.media_size = program.size();
    dut.media_data = 0; dut.peek_addr = 0;
    dut.firmware_we_a = 0; dut.firmware_we_b = 0;
    dut.firmware_addr = 0; dut.firmware_data = 0;
    dut.bus_response = 0;
    dut.eval();
    uint8_t registered_data = 0;
    unsigned wait_cycles = 0;
    for (unsigned i = 0; i < program.size() + 32; ++i)
        tick(dut, program, registered_data, attached, wait_cycles);
    dut.reset = 0;
    unsigned cycles = 0;
    for (; cycles < 200000 && dut.cpu_halt_n; ++cycles)
        tick(dut, program, registered_data, attached, wait_cycles);
    require(cycles < 200000, "Coleco expansion probe did not HALT");
    const uint8_t memory = peek(dut, 0x6000, program, registered_data, attached, wait_cycles);
    if (memory != (attached ? 0x5a : 0xff))
        std::cerr << "attached=" << attached << " memory=" << unsigned(memory)
                  << " waits=" << wait_cycles << '\n';
    require(memory == (attached ? 0x5a : 0xff), "expansion memory read");
    require(peek(dut, 0x6001, program, registered_data, attached, wait_cycles) ==
            (attached ? 0xa6 : 0xff), "expansion I/O read");
    require(peek(dut, 0x6002, program, registered_data, attached, wait_cycles) == 0xc3,
            "expansion overrode Coleco BIOS");
    require(peek(dut, 0x6003, program, registered_data, attached, wait_cycles) == 0x7f,
            "expansion overrode Coleco controller port");
    require(!attached || wait_cycles == 32, "expansion WAIT was not exercised");
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    run(false);
    run(true);
    std::cout << "FES Coleco vacant and CPU-bus expansion probe passed\n";
}
