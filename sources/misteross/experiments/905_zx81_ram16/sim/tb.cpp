#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd9040000U;
constexpr uint32_t kMemWe = 1u << 24;

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

uint32_t pack(uint32_t addr, uint32_t data = 0, uint32_t strobes = 0) {
    return (addr & 0xffffu) | ((data & 0xffu) << 16) | strobes;
}

uint32_t data_bits(uint32_t status) { return status & 0x3ffu; }
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

    gpo(top) = 0x13579BDFU;
    ticks(top, 8);

    gpo(top) = pack(0x4000, 0x5a, kMemWe);
    ticks(top, 8);
    gpo(top) = pack(0x4000);
    ticks(top, 8);
    if (data_bits(gpi(top)) != 0x5a) {
        std::cerr << "mismatch 16K pack low write\n";
        return EXIT_FAILURE;
    }

    gpo(top) = pack(0x7c00, 0xa5, kMemWe);
    ticks(top, 8);
    gpo(top) = pack(0x7c00);
    ticks(top, 8);
    if (data_bits(gpi(top)) != 0xa5) {
        std::cerr << "mismatch 16K pack last-bank write\n";
        return EXIT_FAILURE;
    }

    gpo(top) = pack(0x2000);
    ticks(top, 8);
    if (data_bits(gpi(top)) != 0) {
        std::cerr << "mismatch 16K pack must ignore ROM window\n";
        return EXIT_FAILURE;
    }
    std::cout << "PASS: 16K pack window 4000-7FFF\n";
    return EXIT_SUCCESS;
}
