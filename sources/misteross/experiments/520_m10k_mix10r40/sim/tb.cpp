#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd41c0000U;
constexpr uint32_t kArm = 0x13579bdfU;

uint32_t lane(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0x3ffu;
}

uint64_t wide_word(unsigned wide_addr) {
    uint64_t value = 0;
    for (unsigned i = 0; i < 4; ++i)
        value |= static_cast<uint64_t>(lane(wide_addr * 4 + i)) << (10 * i);
    return value;
}

uint32_t command(unsigned addr, unsigned seed, unsigned window, bool we, bool re, bool clkena) {
    return (static_cast<uint32_t>(we) << 31) | (static_cast<uint32_t>(re) << 30) |
           (static_cast<uint32_t>(clkena) << 29) | ((window & 3u) << 26) |
           ((seed & 0x3ffu) << 16) | (addr & 0x3ffu);
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

bool expect_wide(Vtop &top, unsigned wide_addr, uint64_t word, const char *label) {
    for (unsigned window = 0; window * 16 < 40; ++window) {
        gpo(top) = command(wide_addr, 0, window, false, true, true);
        ticks(top, 64);
        uint32_t expected = kSignature | static_cast<uint32_t>((word >> (window * 16)) & 0xffffu);
        if (!expect_word(gpi(top), expected, label)) return false;
    }
    return true;
}

void stage_write(Vtop &top, unsigned addr, uint32_t seed, bool clkena) {
    uint32_t staged = command(addr, seed, 0, false, true, clkena);
    gpo(top) = staged;
    ticks(top, 8);
    gpo(top) = staged | (1u << 31);
    ticks(top, 16);
    gpo(top) = staged;
    ticks(top, 8);
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
    if (lane(0) != 0xa6u) {
        std::cerr << "contents(0) must be 0xa6\n";
        return EXIT_FAILURE;
    }
    for (unsigned addr : {0u, 1u, 2u, 7u, 15u, 31u, 63u, 127u, 255u}) {
        if (!expect_wide(top, addr, wide_word(addr), "init")) return EXIT_FAILURE;
    }

    gpo(top) = kArm;
    ticks(top, 8);

    const unsigned lane_addr = 32;
    const uint32_t seed = 0x155;
    uint64_t expected = wide_word(lane_addr / 4);
    if (!expect_wide(top, 0, wide_word(0), "addr0 before write")) return EXIT_FAILURE;
    gpo(top) = command(0, 0, 0, false, true, false);
    ticks(top, 16);
    stage_write(top, lane_addr, seed, false);
    expected = (expected & ~0x3ffull) | seed;
    if (!expect_word(gpi(top), kSignature | static_cast<uint32_t>(wide_word(0) & 0xffffu),
                     "held while read clock stopped"))
        return EXIT_FAILURE;
    if (!expect_wide(top, lane_addr / 4, expected, "narrow write")) return EXIT_FAILURE;
    if (!expect_wide(top, lane_addr / 4 + 1, wide_word(lane_addr / 4 + 1), "neighbor"))
        return EXIT_FAILURE;

    const uint32_t held = static_cast<uint32_t>(wide_word(lane_addr / 4 + 1) & 0xffffu);
    gpo(top) = command(lane_addr / 4, 0, 0, false, false, true);
    ticks(top, 16);
    if (!expect_word(gpi(top), kSignature | held, "read-enable hold")) return EXIT_FAILURE;
    if (!expect_wide(top, lane_addr / 4, expected, "read-enable resume")) return EXIT_FAILURE;

    std::cout << "PASS: mixed-width 10-to-40 M10K init, lane order, stopped-clock write, enables\n";
    return EXIT_SUCCESS;
}
