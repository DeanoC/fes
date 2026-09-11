#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

namespace {
constexpr uint32_t kSignature = 0xd4250000U;
constexpr uint32_t kArm = 0x13579bdfU;

uint32_t lane(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0x3ffu;
}

uint32_t command(unsigned addr, unsigned window, bool we, bool re, bool clkena, unsigned be = 0) {
    return (static_cast<uint32_t>(we) << 31) | (static_cast<uint32_t>(re) << 30) |
           (static_cast<uint32_t>(clkena) << 29) | (window << 27) | ((be & 3u) << 4) |
           (addr & 0x3ffu);
}

uint32_t write_command(unsigned wide, uint32_t data20, unsigned be) {
    return (1u << 31) | (1u << 30) | ((data20 & 0xfffffu) << 9) | ((be & 3u) << 4) |
           (wide & 0xfu);
}

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

void ticks(Vtop &top, int count) {
    for (int i = 0; i < count; ++i)
        tick(top);
}

uint32_t &gpo(Vtop &top) { return top.top->hps_gp->gpo; }
uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }

bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected)
        return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}

bool expect_lane(Vtop &top, unsigned addr, uint32_t word, const char *label) {
    gpo(top) = command(addr, 0, false, true, true);
    ticks(top, 64);
    return expect_word(gpi(top) & 0xffff03ffU, kSignature | (word & 0x3ffu), label);
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    gpo(top) = 0;
    top.eval();
    ticks(top, 32);
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature"))
        return EXIT_FAILURE;
    if (lane(0) != 0xa6u) {
        std::cerr << "contents(0) must be 0xa6\n";
        return EXIT_FAILURE;
    }
    for (unsigned addr : {0u, 1u, 2u, 3u, 7u, 14u, 15u, 31u, 63u, 127u, 255u, 1023u}) {
        if (!expect_lane(top, addr, lane(addr), ("init " + std::to_string(addr)).c_str()))
            return EXIT_FAILURE;
    }

    gpo(top) = kArm;
    ticks(top, 8);

    const unsigned wide = 7;
    const unsigned low_addr = wide * 2;
    const unsigned high_addr = wide * 2 + 1;
    uint32_t low_expected = lane(low_addr);
    uint32_t high_expected = lane(high_addr);

    const uint32_t low = 0x155;
    gpo(top) = write_command(wide, low, 1);
    ticks(top, 16);
    low_expected = low;
    if (!expect_lane(top, low_addr, low_expected, "low lane"))
        return EXIT_FAILURE;
    if (!expect_lane(top, high_addr, high_expected, "high lane held"))
        return EXIT_FAILURE;

    const uint32_t high = 0x2aa;
    gpo(top) = write_command(wide, high << 10, 2);
    ticks(top, 16);
    high_expected = high;
    if (!expect_lane(top, low_addr, low_expected, "low lane held"))
        return EXIT_FAILURE;
    if (!expect_lane(top, high_addr, high_expected, "high lane"))
        return EXIT_FAILURE;

    gpo(top) = write_command(wide, 0x3ffff, 0);
    ticks(top, 16);
    if (!expect_lane(top, low_addr, low_expected, "zero mask low"))
        return EXIT_FAILURE;
    if (!expect_lane(top, high_addr, high_expected, "zero mask high"))
        return EXIT_FAILURE;

    const uint32_t both = 0x54321;
    gpo(top) = write_command(wide, both, 3);
    ticks(top, 16);
    if (!expect_lane(top, low_addr, both & 0x3ffu, "both low"))
        return EXIT_FAILURE;
    if (!expect_lane(top, high_addr, (both >> 10) & 0x3ffu, "both high"))
        return EXIT_FAILURE;

    std::cout << "PASS: mixed 20-to-10 M10K byte enables, independent clocks, init and lane masks\n";
    return EXIT_SUCCESS;
}
