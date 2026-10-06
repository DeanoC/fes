// SPDX-License-Identifier: MIT
// fes_rv32_alu against C++ arithmetic: every operation on edge values and
// random operands.
#include "Vfes_rv32_alu.h"
#include "verilated.h"
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <iostream>

static uint64_t state = 0x1234567887654321ull;
static uint32_t rnd() { state ^= state << 13; state ^= state >> 7; state ^= state << 17; return uint32_t(state >> 7); }

static uint32_t reference(uint32_t a, uint32_t b, unsigned f3, bool alt) {
    switch (f3) {
    case 0: return alt ? a - b : a + b;
    case 1: return a << (b & 31);
    case 2: return int32_t(a) < int32_t(b);
    case 3: return a < b;
    case 4: return a ^ b;
    case 5: return alt ? uint32_t(int32_t(a) >> (b & 31)) : a >> (b & 31);
    case 6: return a | b;
    default: return a & b;
    }
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_rv32_alu alu;
    const uint32_t edges[] = {0, 1, 2, 31, 32, 0x7fffffff, 0x80000000u, 0xffffffffu, 0xfffffffeu, 0x12345678u, 0x80000001u};
    uint64_t checked = 0;
    auto check = [&](uint32_t a, uint32_t b, unsigned f3, bool alt) {
        alu.a = a; alu.b = b; alu.funct3 = f3; alu.alt = alt; alu.eval();
        uint32_t expect = reference(a, b, f3, alt);
        if (alu.result != expect || alu.eq != (a == b) || alu.lt != (int32_t(a) < int32_t(b)) || alu.ltu != (a < b)) {
            std::fprintf(stderr, "fes_rv32_alu: a=%08x b=%08x funct3=%u alt=%d result=%08x expected %08x eq=%d lt=%d ltu=%d\n",
                         a, b, f3, alt, alu.result, expect, alu.eq, alu.lt, alu.ltu);
            std::exit(EXIT_FAILURE);
        }
        ++checked;
    };
    for (uint32_t a : edges)
        for (uint32_t b : edges)
            for (unsigned f3 = 0; f3 < 8; ++f3)
                for (int alt = 0; alt < 2; ++alt) check(a, b, f3, alt);
    for (int i = 0; i < 2000000; ++i) {
        uint32_t a = rnd(), b = rnd();
        if (rnd() % 4 == 0) b &= 31;
        check(a, b, rnd() % 8, rnd() % 2);
    }
    std::cout << "fes_rv32_alu: " << checked << " operations match (host simulation only).\n";
    return 0;
}
