// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcart.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

static void require(bool ok, const char *why) {
    if (!ok) { std::cerr << why << '\n'; std::exit(EXIT_FAILURE); }
}
struct Driver {
    Vcart dut;
    static constexpr uint32_t idle = 0x3f000000;
    void tick() {
        dut.FPGA_CLK1_50 = 0; dut.eval();
        dut.FPGA_CLK1_50 = 1; dut.eval();
        dut.FPGA_CLK1_50 = 0; dut.eval();
    }
    void quiet() { dut.plug_addr = idle; tick(); tick(); }
    void port_write(uint8_t port, uint8_t data) {
        dut.plug_addr = (idle & ~(1u << 25) & ~(1u << 27)) |
                        (uint32_t(data) << 16) | port;
        for (unsigned i = 0; i < 20; ++i) tick();
        quiet();
    }
    uint32_t memory(uint16_t address, bool write = false) {
        dut.plug_addr = (idle & ~(1u << 24) & ~(1u << (write ? 27 : 26))) | address;
        dut.eval(); return dut.plug_rdata;
    }
    uint32_t port_read(uint8_t port) {
        dut.plug_addr = (idle & ~(1u << 25) & ~(1u << 26)) | port;
        dut.eval(); return dut.plug_rdata;
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Driver d;
    d.dut.FPGA_CLK1_50 = 0; d.dut.plug_addr = Driver::idle | (1u << 30);
    d.tick(); d.quiet();
    for (uint16_t addr : {0x0000, 0x1fff, 0x2000, 0x5fff, 0x6000, 0x7fff, 0x8000})
        require(!(d.memory(addr) & (1u << 11)), "SGM RAM claimed while disabled");
    d.port_write(0x53, 1);
    for (uint16_t addr : {0x2000, 0x5fff, 0x6000, 0x7fff})
        require(d.memory(addr) & (1u << 11), "SGM upper RAM claim missing");
    for (uint16_t addr : {0x0000, 0x1fff, 0x8000, 0xffff})
        require(!(d.memory(addr) & (1u << 11)), "SGM claimed forbidden address");
    require(d.memory(0x6000, true) & (1u << 11), "SGM write claim missing");
    d.port_write(0x7f, 0);
    for (uint16_t addr : {0x0000, 0x1fff})
        require(d.memory(addr) & (1u << 11), "SGM BIOS overlay claim missing");
    d.port_write(0x53, 0);
    require(!(d.memory(0x6000) & (1u << 11)), "SGM upper disable failed");
    d.port_write(0x7f, 2);
    require(!(d.memory(0x0000) & (1u << 11)), "SGM lower disable failed");

    d.port_write(0x50, 8); d.port_write(0x51, 15);
    const uint32_t read = d.port_read(0x52);
    require((read & 0x1ff) == 0x10f, "SGM AY data-read claim/value");
    require(!(d.port_read(0xbf) & (1u << 8)), "SGM stole VDP I/O");
    require(!(d.port_read(0xfc) & (1u << 8)), "SGM stole controller I/O");
    d.port_write(0x50, 7); d.port_write(0x51, 0x3f);
    for (unsigned i = 0; i < 16; ++i) d.tick();
    require(int16_t(d.dut.plug_rdata >> 12) > 0, "SGM AY PCM absent");

    d.dut.plug_addr = Driver::idle | (1u << 30); d.dut.eval();
    require(d.dut.plug_rdata == 0, "SGM reset response is not vacant");
    d.tick(); d.quiet();
    require(!(d.memory(0x0000) & (1u << 11)), "SGM reset did not disable overlay");
    std::cout << "Coleco SGM v2 module control/AY ports passed\n";
}
