#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd41a0000U;
constexpr uint32_t kArm = 0x13579bdfU;

uint32_t initial_word(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0xfffffu;
}

uint32_t command(unsigned addr, unsigned window, bool we, bool re, bool clkena, unsigned be = 0) {
    return (static_cast<uint32_t>(we) << 31) | (static_cast<uint32_t>(re) << 30) |
           (static_cast<uint32_t>(clkena) << 29) | (window << 27) | ((be & 3u) << 4) |
           (addr & 0x1ffu);
}

uint32_t write_command(unsigned addr, uint32_t data20, unsigned be) {
    return (1u << 31) | (1u << 30) | ((data20 & 0xfffffu) << 9) | ((be & 3u) << 4) |
           (addr & 0xfu);
}

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

void ticks(Vtop &top, int count) {
    for (int i = 0; i < count; ++i) tick(top);
}

uint32_t &gpo(Vtop &top) { return top.top->hps_gp->gpo; }
uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }

bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected) return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}

bool expect_read(Vtop &top, unsigned addr, uint32_t word, const char *label) {
    for (unsigned window = 0; window * 16 < 20; ++window) {
        gpo(top) = command(addr, window, false, true, true);
        ticks(top, 64);
        uint32_t expected = kSignature | ((word >> (window * 16)) & 0xffffu);
        if (!expect_word(gpi(top), expected, label)) return false;
    }
    return true;
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    gpo(top) = 0;
    top.eval();
    ticks(top, 32);
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) return EXIT_FAILURE;
    if (initial_word(0) != 0xa6u) {
        std::cerr << "contents(0) must be 0xa6\n";
        return EXIT_FAILURE;
    }
    for (unsigned addr : {0u, 1u, 2u, 3u, 7u, 15u, 31u, 63u, 127u, 255u}) {
        if (!expect_read(top, addr, initial_word(addr), "init")) return EXIT_FAILURE;
    }

    const unsigned addr = 7;
    uint32_t expected = initial_word(addr);
    gpo(top) = kArm;
    ticks(top, 8);

    const uint32_t low = 0x155;
    gpo(top) = write_command(addr, low, 1);
    ticks(top, 16);
    expected = (expected & ~0x3ffu) | low;
    if (!expect_read(top, addr, expected, "low lane")) return EXIT_FAILURE;

    const uint32_t high = 0x2aa;
    gpo(top) = write_command(addr, high << 10, 2);
    ticks(top, 16);
    expected = (expected & 0x3ffu) | (high << 10);
    if (!expect_read(top, addr, expected, "high lane")) return EXIT_FAILURE;

    gpo(top) = write_command(addr, 0x3ffff, 0);
    ticks(top, 16);
    if (!expect_read(top, addr, expected, "zero mask")) return EXIT_FAILURE;

    const uint32_t both = 0x54321;
    gpo(top) = write_command(addr, both, 3);
    ticks(top, 16);
    if (!expect_read(top, addr, both, "both lanes")) return EXIT_FAILURE;

    std::cout << "PASS: 20-bit M10K byte enables, independent clocks, init and lane masks\n";
    return EXIT_SUCCESS;
}
