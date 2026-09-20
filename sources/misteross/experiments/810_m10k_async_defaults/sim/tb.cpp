#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd42e0000U;
constexpr uint32_t kArm = 0x13579bdfU;

uint32_t initial_word(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0xfffffu;
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

bool expect_read(Vtop &top, unsigned addr, uint32_t word, const char *label) {
    gpo(top) = addr & 0xfu;
    ticks(top, 8);
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
    for (unsigned addr : {0u, 1u, 2u, 3u, 7u, 15u}) {
        if (!expect_read(top, addr, initial_word(addr), "init"))
            return EXIT_FAILURE;
    }

    const unsigned addr = 7;
    const uint32_t written = 0x155u;
    gpo(top) = kArm;
    ticks(top, 4);
    gpo(top) = (1u << 31) | ((written & 0xfffffu) << 9) | addr;
    ticks(top, 8);
    if (!expect_read(top, addr, written, "write"))
        return EXIT_FAILURE;
    if (!expect_read(top, 1u, initial_word(1u), "neighbour"))
        return EXIT_FAILURE;
    std::cout << "PASS: combinational M10K read of INIT and a write, neighbour undisturbed\n";
    return EXIT_SUCCESS;
}
