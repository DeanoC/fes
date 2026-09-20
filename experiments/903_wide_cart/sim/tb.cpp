#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd9010000U;

uint32_t initial_word(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0x3ffu;
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
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    gpo(top) = 0;
    top.eval();
    ticks(top, 8);
    if ((gpi(top) & 0xffff0000U) != kSignature)
        return EXIT_FAILURE;
    gpo(top) = 7;
    ticks(top, 16);
    if ((gpi(top) & 0x3ffu) != initial_word(7)) {
        std::cerr << "mismatch cart B bank 0\n";
        return EXIT_FAILURE;
    }
    gpo(top) = 7u | (1u << 10);
    ticks(top, 16);
    if ((gpi(top) & 0x3ffu) != (initial_word(7) ^ 17u)) {
        std::cerr << "mismatch cart B bank 1\n";
        return EXIT_FAILURE;
    }
    std::cout << "PASS: cart B decode occupies multiple slot cells\n";
    return EXIT_SUCCESS;
}
