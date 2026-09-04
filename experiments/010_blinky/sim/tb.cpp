#include "Vtop.h"
#include "verilated.h"

#include <cstdlib>
#include <iostream>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;

    constexpr int kCounterMask = 0x0f;
    constexpr int kRisingEdges = 24;

    top.FPGA_CLK1_50 = 0;
    top.eval();

    for (int cycle = 1; cycle <= kRisingEdges; ++cycle) {
        top.FPGA_CLK1_50 = 1;
        top.eval();

        const int count = cycle & kCounterMask;
        const int expected = count >= 8 ? 1 : 0;
        const int observed = top.LED & 1;
        if (observed != expected) {
            std::cerr << "mismatch cycle=" << cycle << " count=" << count
                      << " expected=" << expected << " observed=" << observed
                      << '\n';
            return EXIT_FAILURE;
        }

        if (cycle == 8 || cycle == 16) {
            std::cout << "transition cycle=" << cycle << " count=" << count
                      << " LED=" << observed << '\n';
        }

        top.FPGA_CLK1_50 = 0;
        top.eval();
    }

    std::cout << "PASS: 24 post-edge counts verified (0-7 low, 8-15 high, wrap low)\n";
    return EXIT_SUCCESS;
}
