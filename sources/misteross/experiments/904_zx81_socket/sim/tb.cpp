#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd9040000U;

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
    if ((gpi(top) & 0x3ffu) != 0) {
        std::cerr << "mismatch empty socket should read 0\n";
        return EXIT_FAILURE;
    }
    if (((gpi(top) >> 10) & 0x3fu) != 7u) {
        std::cerr << "mismatch plug_addr should follow GPO\n";
        return EXIT_FAILURE;
    }
    std::cout << "PASS: 904 empty socket reads 0\n";
    return EXIT_SUCCESS;
}
