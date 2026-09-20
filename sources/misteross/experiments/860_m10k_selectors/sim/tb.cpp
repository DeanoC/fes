#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd8600000U;
constexpr uint32_t kArm = 0x13579bdfU;
constexpr uint32_t kEn = 1u << 30;

uint32_t initial_word(unsigned bank, unsigned addr) {
    uint32_t word = ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0xfffffu;
    if (bank)
        word ^= 0x11u;
    return word;
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

bool expect_read(Vtop &top, unsigned bank, unsigned addr, uint32_t word,
                 const char *label) {
    gpo(top) = kEn | ((bank & 1u) << 9) | (addr & 0x1ffu);
    ticks(top, 16);
    return expect_word(gpi(top), kSignature | (word & 0xffffu), label);
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    gpo(top) = 0;
    top.eval();
    ticks(top, 8);
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature"))
        return EXIT_FAILURE;
    for (unsigned addr : {0u, 1u, 2u, 7u, 15u}) {
        if (!expect_read(top, 0, addr, initial_word(0, addr), "bank0 init"))
            return EXIT_FAILURE;
        if (!expect_read(top, 1, addr, initial_word(1, addr), "bank1 init"))
            return EXIT_FAILURE;
    }

    gpo(top) = kArm;
    ticks(top, 4);
    const unsigned addr = 7;
    const uint32_t written = 0x155u;
    gpo(top) = (1u << 31) | kEn | (written << 10) | addr;
    ticks(top, 16);
    if (!expect_read(top, 0, addr, written, "bank0 write"))
        return EXIT_FAILURE;
    if (!expect_read(top, 1, addr, initial_word(1, addr), "bank1 neighbour"))
        return EXIT_FAILURE;
    std::cout << "PASS: two dual-clock M10K banks with distinct INIT and a write\n";
    return EXIT_SUCCESS;
}
