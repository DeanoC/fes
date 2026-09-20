#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd41b0000U;
constexpr uint32_t kArm = 0x13579bdfU;

uint32_t lane(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0x3ffu;
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

bool expect_lane(Vtop &top, unsigned addr, uint32_t word, const char *label) {
    gpo(top) = command(addr, 0, 0, false, true, true);
    ticks(top, 64);
    return expect_word(gpi(top) & 0xffff03ffU, kSignature | (word & 0x3ffu), label);
}

void stage_write(Vtop &top, unsigned wide_addr, uint32_t seed, bool clkena) {
    uint32_t staged = command(wide_addr, seed, 0, false, true, clkena);
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
    for (unsigned addr : {0u, 1u, 2u, 3u, 7u, 15u, 31u, 63u, 127u, 255u, 1023u}) {
        if (!expect_lane(top, addr, lane(addr), "init")) return EXIT_FAILURE;
    }

    gpo(top) = kArm;
    ticks(top, 8);

    const unsigned wide = 8;
    const uint32_t seed = 0x155;
    if (!expect_lane(top, 0, lane(0), "addr0 before write")) return EXIT_FAILURE;
    gpo(top) = command(0, 0, 0, false, true, false);
    ticks(top, 16);
    stage_write(top, wide, seed, false);
    if (!expect_word(gpi(top), kSignature | lane(0), "held while read clock stopped"))
        return EXIT_FAILURE;
    for (unsigned i = 0; i < 4; ++i) {
        uint32_t expected = (seed ^ (i * 0x93u)) & 0x3ffu;
        if (!expect_lane(top, wide * 4 + i, expected, "wide write lane")) return EXIT_FAILURE;
    }
    if (!expect_lane(top, wide * 4 + 4, lane(wide * 4 + 4), "neighbor")) return EXIT_FAILURE;

    const uint32_t neighbor = lane(wide * 4 + 4);
    gpo(top) = command(wide * 4, 0, 0, false, false, true);
    ticks(top, 16);
    if (!expect_word(gpi(top), kSignature | neighbor, "read-enable hold")) return EXIT_FAILURE;
    if (!expect_lane(top, wide * 4, seed & 0x3ffu, "read-enable resume")) return EXIT_FAILURE;

    std::cout << "PASS: mixed-width 40-to-10 M10K init, lane order, stopped-clock write, enables\n";
    return EXIT_SUCCESS;
}
