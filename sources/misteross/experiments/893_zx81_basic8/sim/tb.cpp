#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd8930000U;

uint32_t initial_word(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0x3ffu;
}

uint32_t lane_word(unsigned addr) {
    unsigned bank = (addr >> 10) & 7u;
    unsigned lane = addr & 0x3ffu;
    if (lane == 0)
        return (0x100u + bank) & 0x3ffu;
    return initial_word(lane);
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

bool expect_read(Vtop &top, unsigned addr, const char *label) {
    gpo(top) = addr & 0x1fffu;
    ticks(top, 8);
    return expect_word(gpi(top), kSignature | lane_word(addr), label);
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
    for (unsigned addr : {0u, 1u, 1024u, 1025u, 2048u, 3072u, 4096u, 5120u, 6144u, 7168u, 8191u}) {
        if (!expect_read(top, addr, "lane"))
            return EXIT_FAILURE;
    }
    std::cout << "PASS: eight 1024x10 lanes on the column-26 blank\n";
    return EXIT_SUCCESS;
}
