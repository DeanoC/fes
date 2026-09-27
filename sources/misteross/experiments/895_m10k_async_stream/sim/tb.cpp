#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd8950000U;
constexpr uint32_t kDone = 0x8000U;

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
    top.eval();
    top.top->hps_gp->gpo = 0;
    top.eval();
    for (int cycle = 0; cycle < 450000; ++cycle) {
        tick(top);
        const uint32_t result = top.top->hps_gp->gpi;
        if ((result & 0xffff0000U) != kSignature) {
            std::cerr << "missing signature: " << std::hex << result << '\n';
            return EXIT_FAILURE;
        }
        if (result & kDone) {
            if (result & 0x3fffU) {
                std::cerr << "at-speed M10K read errors: " << std::hex << result << '\n';
                return EXIT_FAILURE;
            }
            break;
        }
        if (cycle == 449999) {
            std::cerr << "at-speed M10K sweep did not finish\n";
            return EXIT_FAILURE;
        }
    }
    top.top->hps_gp->gpo = 0x109;
    top.eval();
    if ((top.top->hps_gp->gpi & 0xffffU) != 0xc201U) {
        std::cerr << "clock-stopped address 1 did not read asynchronously\n";
        return EXIT_FAILURE;
    }
    top.top->hps_gp->gpo = 0x209;
    top.eval();
    if ((top.top->hps_gp->gpi & 0xffffU) != 0xc102U) {
        std::cerr << "clock-stopped address 2 did not read asynchronously\n";
        return EXIT_FAILURE;
    }

    Vtop damaged;
    damaged.FPGA_CLK1_50 = 0;
    damaged.eval();
    damaged.top->hps_gp->gpo = 0;
    damaged.eval();
    bool injected = false;
    bool restored = false;
    for (int cycle = 0; cycle < 450000; ++cycle) {
        tick(damaged);
        if (!injected && damaged.top->phase == 2 && damaged.top->samples == 0) {
            damaged.top->memory[1] ^= 1ULL;
            damaged.eval();
            injected = true;
        }
        if (injected && !restored && damaged.top->phase == 3) {
            damaged.top->memory[1] ^= 1ULL;
            damaged.eval();
            restored = true;
        }
        if (damaged.top->hps_gp->gpi & kDone)
            break;
    }
    if (!injected || !restored || !(damaged.top->hps_gp->gpi & kDone) ||
        !(damaged.top->hps_gp->gpi & 0x3fffU)) {
        std::cerr << "checker missed an injected bit error\n";
        return EXIT_FAILURE;
    }
    damaged.top->hps_gp->gpo = 1;
    damaged.eval();
    if ((damaged.top->hps_gp->gpi & 0xffffU) != 1U) {
        std::cerr << "first error address was not recorded\n";
        return EXIT_FAILURE;
    }
    damaged.top->hps_gp->gpo = 2;
    damaged.eval();
    if ((damaged.top->hps_gp->gpi & 0xffffU) != 0xc201U) {
        std::cerr << "first expected word was not recorded\n";
        return EXIT_FAILURE;
    }
    damaged.top->hps_gp->gpo = 3;
    damaged.eval();
    if ((damaged.top->hps_gp->gpi & 0xffffU) != 0xc200U) {
        std::cerr << "first observed word was not recorded\n";
        return EXIT_FAILURE;
    }
    damaged.top->hps_gp->gpo = 8;
    damaged.eval();
    if (damaged.top->hps_gp->gpi & 0x3fffU) {
        std::cerr << "restored memory still failed the held-address sweep\n";
        return EXIT_FAILURE;
    }
    std::cout << "PASS: fast and held-address sweeps, injected fast read error\n";
    return EXIT_SUCCESS;
}
