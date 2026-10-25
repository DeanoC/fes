// SPDX-License-Identifier: MIT
// fes_i386_agu against the architectural address: 32-bit sums wrap at 4 GiB;
// a 16-bit offset wraps at 64 KiB before the segment base is added and the
// linear sum does not. Random operands, plus operands at the 16-bit wrap
// edges, in both address sizes. Output: one JSON line; exit status 1 on any
// mismatch.
#include "Vfes_i386_agu.h"
#include "verilated.h"

#include <cstdio>
#include <cstdlib>
#include <random>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    VerilatedContext context;
    Vfes_i386_agu top(&context);
    std::mt19937_64 random(1);
    uint64_t count = 4000000, checked = 0, wrapped = 0, failures = 0;
    if (const char *value = std::getenv("FES_I386_AGU_VECTORS")) count = std::strtoull(value, nullptr, 10);
    // Values near the edges that decide a wrap, mixed with random ones.
    const uint32_t edges[] = {0, 1, 2, 0x7fff, 0x8000, 0xfffe, 0xffff, 0x10000, 0xffff0000, 0xfffffff0, 0xffffffff};
    auto pick = [&](void) -> uint32_t {
        uint32_t value = uint32_t(random());
        switch (random() % 4) {
        case 0: return edges[random() % (sizeof edges / sizeof edges[0])];
        case 1: return value & 0xffff;
        default: return value;
        }
    };

    for (uint64_t n = 0; n < count; ++n) {
        const bool addr32 = n & 1;
        const uint32_t seg = pick(), base = pick(), index = pick(), disp = pick();
        const unsigned scale = addr32 ? unsigned(random() % 4) : 0;
        top.addr32 = addr32;
        top.seg_base = seg;
        top.seg_base_hi_plus1 = uint16_t((seg >> 16) + 1);
        top.base = base;
        top.index = index;
        top.scale = scale;
        top.disp = disp;
        top.eval();

        uint32_t offset, linear;
        if (addr32) {
            offset = base + (index << scale) + disp;
        } else {
            uint32_t raw = (base & 0xffff) + (index & 0xffff) + (disp & 0xffff);
            wrapped += raw > 0xffff;
            offset = raw & 0xffff;
        }
        linear = seg + offset;
        ++checked;
        if (top.offset != offset || top.linear != linear) {
            if (++failures <= 10)
                std::printf("FAIL addr32 %d seg %08x base %08x index %08x scale %u disp %08x: "
                            "rtl %08x/%08x, expected %08x/%08x\n",
                            addr32, seg, base, index, scale, disp, top.linear, top.offset, linear, offset);
        }
    }
    std::printf("{\"agu_vectors\": %llu, \"wrapped_16\": %llu, \"failures\": %llu}\n",
                (unsigned long long)checked, (unsigned long long)wrapped, (unsigned long long)failures);
    return failures ? 1 : 0;
}
