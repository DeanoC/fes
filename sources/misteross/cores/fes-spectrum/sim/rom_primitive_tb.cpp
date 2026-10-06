// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vrom_primitive_top.h"
#include "verilated.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vrom_primitive_top top;
    top.clk = 0;
    top.load = 0;
    top.eval();
    uint64_t checked = 0;
    for (unsigned salt : {0u, 0x5au, 0xffu}) {
        top.salt = salt;
        top.load = 1;
        top.clk = 1; top.eval();
        top.clk = 0; top.eval();
        top.load = 0;
        unsigned warmup = 0;
        auto tick = [&](unsigned address) {
            unsigned previous = top.actual;
            top.address = address;
            top.eval();
            if (top.actual != previous)
                throw std::runtime_error("output changed between clocks");
            top.clk = 1; top.eval();
            if (++warmup > 2) {
                if (top.actual != top.expected) {
                    std::cerr << "address=" << address << " salt=" << salt
                              << " actual=" << unsigned(top.actual)
                              << " expected=" << unsigned(top.expected) << '\n';
                    throw std::runtime_error("primitive ROM differs from two-stage reference");
                }
                ++checked;
            }
            top.clk = 0; top.eval();
        };
        // Full address coverage in both directions, including each bank edge.
        for (unsigned a = 0; a < 16384; ++a) tick(a);
        for (unsigned a = 16384; a-- > 0;) tick(a);
        // Bank and local address change together, independently every clock.
        uint32_t state = 0x98765432u;
        for (unsigned i = 0; i < 65536; ++i) {
            state ^= state << 13; state ^= state >> 17; state ^= state << 5;
            tick(state & 16383u);
        }
        // Flush the pipeline and revisit word zero to detect accidental writes.
        tick(0); tick(0); tick(0);
    }
    std::cout << "PASS production M10K ROM: " << checked
              << " comparisons, three images, all addresses and bank transitions\n";
}
