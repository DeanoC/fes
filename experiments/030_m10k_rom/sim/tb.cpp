#include "Vtop.h"
#include "verilated.h"

#include <cstdlib>
#include <iostream>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;

    constexpr int kAddrBits = 4;
    constexpr int kDepth = 1 << kAddrBits;
    constexpr int kPattern = 0xa5;
    constexpr int kRisingEdges = kDepth + 1;

    top.FPGA_CLK1_50 = 0;
    top.eval();

    const int reset_led = top.LED & 1;
    if (reset_led != 0) {
        std::cerr << "mismatch reset LED expected=0 observed=" << reset_led << '\n';
        return EXIT_FAILURE;
    }

    for (int cycle = 1; cycle <= kRisingEdges; ++cycle) {
        top.FPGA_CLK1_50 = 1;
        top.eval();

        const int read_index = (cycle - 1) & (kDepth - 1);
        const int expected = ((read_index ^ kPattern) & 1);
        const int observed = top.LED & 1;
        if (observed != expected) {
            std::cerr << "mismatch cycle=" << cycle << " index=" << read_index
                      << " expected=" << expected << " observed=" << observed
                      << '\n';
            return EXIT_FAILURE;
        }

        if (cycle == 1 || cycle == 2) {
            std::cout << "transition cycle=" << cycle << " index=" << read_index
                      << " LED=" << observed << '\n';
        }

        top.FPGA_CLK1_50 = 0;
        top.eval();
    }

    std::cout << "PASS: 17 post-edge stored-pattern bits verified with one-cycle latency\n";
    return EXIT_SUCCESS;
}
