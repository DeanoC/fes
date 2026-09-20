// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vexpansion_machine.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

static void tick(Vexpansion_machine &dut) {
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
}

static uint8_t peek(Vexpansion_machine &dut, uint16_t address) {
    dut.peek_addr = address;
    // The optional pack's diagnostic read crosses both socket registers.
    for (unsigned i = 0; i < 3; ++i) tick(dut);
    return dut.peek_data;
}

static uint16_t word(Vexpansion_machine &dut, uint16_t address) {
    return peek(dut, address) | (uint16_t(peek(dut, address + 1)) << 8);
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 2) return EXIT_FAILURE;
    const unsigned expected_top = std::strtoul(argv[1], nullptr, 0);
    Vexpansion_machine dut;
    dut.keyboard = 0xffffffffffull;
    dut.tape_ready = 0;
    dut.tape_size = 0;
    dut.tape_data = 0;
    dut.peek_addr = 0;
    dut.reset = 1;
    for (unsigned i = 0; i < 64; ++i) tick(dut);
    dut.reset = 0;
    bool basic_ready = false, pixels = false, halted = false;
    for (uint64_t cycle = 0; cycle < 120000000; ++cycle) {
        tick(dut);
        halted |= !dut.halt_n;
        pixels |= dut.ce_6m5 && !dut.hblank && !dut.vblank && dut.video_pixel;
        if ((cycle & 0x1ffff) == 0) {
            const auto display = word(dut, 0x400c);
            basic_ready = display >= 0x4070 && display < expected_top && peek(dut, display) == 0x76;
            if (basic_ready && halted && pixels) break;
        }
    }
    const unsigned ram_top = word(dut, 0x4004);
    if (!basic_ready || !halted || !pixels || ram_top != expected_top) {
        std::cerr << "ZX81 expansion boot failed: BASIC=" << basic_ready
                  << " HALT=" << halted << " pixels=" << pixels
                  << " RAMTOP=" << std::hex << ram_top << " expected=" << expected_top << '\n';
        return EXIT_FAILURE;
    }
    std::cout << "ZX81 socket BASIC/ULA boot passed, RAMTOP=" << std::hex << ram_top << '\n';
}
