#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd89100a6U;

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();
    for (int i = 0; i < 8; ++i)
        tick(top);
    if (gpi(top) != kSignature) {
        std::cerr << "mismatch signature expected=0x" << std::hex << kSignature
                  << " observed=0x" << gpi(top) << std::dec << '\n';
        return EXIT_FAILURE;
    }
    std::cout << "PASS: empty-slot base signature\n";
    return EXIT_SUCCESS;
}
