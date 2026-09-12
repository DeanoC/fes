#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd8700000U;
constexpr uint32_t kArm = 0x13579bdfU;
constexpr uint32_t kEn = 1u << 30;

uint32_t initial_bit(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 1u;
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

bool expect_read(Vtop &top, unsigned addr, uint32_t bit, const char *label) {
    gpo(top) = kEn | (addr & 0x1fffu);
    ticks(top, 16);
    return expect_word(gpi(top), kSignature | (bit & 1u), label);
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
    for (unsigned addr : {0u, 1u, 2u, 7u, 15u, 31u, 63u, 127u, 255u, 1023u, 4095u, 8191u}) {
        if (!expect_read(top, addr, initial_bit(addr), "init"))
            return EXIT_FAILURE;
    }
    gpo(top) = kArm;
    ticks(top, 4);
    const unsigned addr = 7;
    const uint32_t written = 1u ^ initial_bit(addr);
    gpo(top) = (1u << 31) | kEn | (written << 13) | addr;
    ticks(top, 16);
    if (!expect_read(top, addr, written, "write"))
        return EXIT_FAILURE;
    if (!expect_read(top, 1, initial_bit(1), "neighbour"))
        return EXIT_FAILURE;
    std::cout << "PASS: 8192x1 TDP INIT and A-port write\n";
    return EXIT_SUCCESS;
}
