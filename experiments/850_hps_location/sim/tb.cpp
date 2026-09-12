#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kExpected = 0xd85000a6U;

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.top->hps_gp->gpo = 0;
    top.eval();
    for (int i = 0; i < 8; ++i)
        tick(top);
    const uint32_t observed = top.top->hps_gp->gpi;
    if (observed != kExpected) {
        std::cerr << "mismatch expected=0x" << std::hex << kExpected
                  << " observed=0x" << observed << std::dec << '\n';
        return EXIT_FAILURE;
    }
    std::cout << "PASS: HPS I2C design returns signature 0xD850 and payload 0x00A6\n";
    return EXIT_SUCCESS;
}
