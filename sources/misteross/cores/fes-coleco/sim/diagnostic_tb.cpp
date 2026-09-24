// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcart.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

static void require(bool ok, const char* message) {
    if (!ok) { std::cerr << message << '\n'; std::exit(EXIT_FAILURE); }
}

static constexpr uint32_t idle = 0x3f000000u;
static uint32_t mem_read(uint16_t address) { return (idle & ~((1u << 24) | (1u << 26))) | address; }
static uint32_t mem_write(uint16_t address, uint8_t data) {
    return (idle & ~((1u << 24) | (1u << 27))) | (uint32_t(data) << 16) | address;
}
static uint32_t io_read(uint8_t port) { return (idle & ~((1u << 25) | (1u << 26))) | port; }

static void tick(Vcart& dut, uint32_t request) {
    dut.plug_addr = request;
    dut.FPGA_CLK1_50 = 0; dut.eval();
    dut.FPGA_CLK1_50 = 1; dut.eval();
    dut.FPGA_CLK1_50 = 0; dut.eval();
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vcart dut;
    tick(dut, idle | (1u << 30));
    tick(dut, mem_read(0x2000));
    require((dut.plug_rdata & 0x1ff) == 0x15a, "reset register read");
    tick(dut, mem_write(0x2000, 0xa5));
    tick(dut, mem_read(0x2000));
    require((dut.plug_rdata & 0x1ff) == 0x1a5, "written register read");
    tick(dut, mem_write(0x2001, 5));
    for (int i = 0; i < 64; ++i) tick(dut, idle);
    tick(dut, mem_read(0x2000));
    require((dut.plug_rdata & (1u << 9)) != 0, "armed WAIT expired before read");
    for (int i = 0; i < 94; ++i) tick(dut, mem_read(0x2000));
    require((dut.plug_rdata & (1u << 9)) != 0, "WAIT did not span CPU enables");
    tick(dut, mem_read(0x2000));
    require((dut.plug_rdata & (1u << 9)) == 0, "WAIT did not release");
    tick(dut, mem_write(0x2002, 1));
    tick(dut, io_read(0x40));
    require((dut.plug_rdata & 0x5ff) == 0x501, "I/O status or INT failed");
    tick(dut, io_read(0xfc));
    require((dut.plug_rdata & (1u << 8)) == 0, "claimed controller I/O");
    tick(dut, mem_read(0x0000));
    require((dut.plug_rdata & (1u << 8)) == 0, "claimed BIOS");
    tick(dut, idle | (1u << 30));
    tick(dut, io_read(0x40));
    require((dut.plug_rdata & 0x5ff) == 0x100, "reset did not clear INT");
    std::cout << "FES Coleco diagnostic module passed\n";
}
