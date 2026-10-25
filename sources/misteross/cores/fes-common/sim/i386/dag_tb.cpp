// SPDX-License-Identifier: MIT
// D's address operands and AG (dag_tb_top) against the architectural address
// and the late-operand rules.
//
// Every memory ModRM (mod 0-2) with every SIB byte, in both address sizes,
// runs against random register files and random producers in AG, M1, M2, EX
// and WB. A producer's result is early (AG), load data (end of M2) or late
// (end of EX). The model works out each address register's youngest value
// and when it is available:
//   in a result register now                  D forwards it
//   completing this cycle (AG early, M2 load, EX late)
//                                             late into AG's last row
//   later                                     D stalls
// D must stall exactly when an operand is not yet available, when both
// operands are late, or when the late one is a scaled index. Otherwise AG's
// linear address and offset must equal the architectural ones: 32-bit sums
// wrap at 4 GiB, and a 16-bit offset wraps at 64 KiB before the segment base
// is added. Output: one JSON line; exit status 1 on any failure.
#include "Vdag_tb_top.h"
#include "verilated.h"

#include <cstdio>
#include <cstdlib>
#include <random>

namespace {

enum Kind { EARLY, LOAD, LATE };
enum Status { NOW, LATE_VALUE, WAIT };

struct Producer {
    bool valid;
    unsigned dst;
    Kind kind;
    uint32_t value;
};

// Stage 0 AG, 1 M1, 2 M2, 3 EX, 4 WB.
Status status(int stage, Kind kind) {
    switch (stage) {
    case 0: return kind == EARLY ? LATE_VALUE : WAIT;
    case 1: return kind == EARLY ? NOW : WAIT;
    case 2: return kind == EARLY ? NOW : kind == LOAD ? LATE_VALUE : WAIT;
    case 3: return kind == LATE ? LATE_VALUE : NOW;
    default: return NOW;
    }
}

struct Mode {
    bool base_ok = false, index_ok = false;
    unsigned base = 0, index = 0, scale = 0;
};

// x86 addressing, with the i386's scaled base for SIB index 100b.
Mode addressing(unsigned modrm, unsigned sib, bool addr32) {
    Mode m;
    unsigned mod = modrm >> 6, rm = modrm & 7;
    if (!addr32) {
        static const int base16[8] = {3, 3, 5, 5, -1, -1, 5, 3};
        static const int index16[8] = {6, 7, 6, 7, 6, 7, -1, -1};
        if (base16[rm] >= 0 && !(rm == 6 && mod == 0)) { m.base_ok = true; m.base = unsigned(base16[rm]); }
        if (index16[rm] >= 0) { m.index_ok = true; m.index = unsigned(index16[rm]); }
        return m;
    }
    if (rm != 4) {
        if (!(mod == 0 && rm == 5)) { m.base_ok = true; m.base = rm; }
        return m;
    }
    unsigned base = sib & 7, index = (sib >> 3) & 7, scale = sib >> 6;
    bool no_base = mod == 0 && base == 5;
    if (index == 4) {
        if (scale && !no_base) { m.index_ok = true; m.index = base; m.scale = scale; }
        else if (!no_base) { m.base_ok = true; m.base = base; }
        return m;
    }
    m.index_ok = true; m.index = index; m.scale = scale;
    if (!no_base) { m.base_ok = true; m.base = base; }
    return m;
}

struct Counts {
    uint64_t checked = 0, failures = 0;
    uint64_t addresses = 0, wrapped = 0, forwarded = 0, from_file = 0;
    uint64_t late_base[3] = {}, late_index[3] = {};
    uint64_t stall_wait = 0, stall_both_late = 0, stall_scaled_late = 0;
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    VerilatedContext context;
    Vdag_tb_top top(&context);
    std::mt19937_64 random(1);
    unsigned rounds = 16;
    if (const char *value = std::getenv("FES_I386_DAG_ROUNDS")) rounds = unsigned(std::atoi(value));
    Counts n;
    const uint32_t edges[] = {0, 1, 0x7fff, 0x8000, 0xfffe, 0xffff, 0x10000, 0xffff0000, 0xfffffff0, 0xffffffff};
    auto value = [&]() -> uint32_t {
        uint32_t v = uint32_t(random());
        switch (random() % 4) {
        case 0: return edges[random() % (sizeof edges / sizeof edges[0])];
        case 1: return v & 0xffff;
        default: return v;
        }
    };

    for (unsigned modrm = 0; modrm < 0xc0; ++modrm) {
        for (unsigned sib = 0; sib < 256; ++sib) {
            for (int addr32 = 0; addr32 < 2; ++addr32) {
                const Mode mode = addressing(modrm, sib, addr32);
                for (unsigned round = 0; round < rounds; ++round) {
                    uint32_t gpr[8], seg = value(), disp = addr32 ? value() : value() & 0xffff;
                    for (auto &g : gpr) g = value();
                    Producer prod[5];
                    for (Producer &p : prod) {
                        p.valid = random() & 1;
                        unsigned pick = random() % 4;
                        p.dst = pick == 0 && mode.base_ok ? mode.base : pick == 1 && mode.index_ok ? mode.index
                              : unsigned(random() % 8);
                        p.kind = Kind(random() % 3);
                        p.value = value();
                    }
                    // What each register holds architecturally, and when.
                    auto operand = [&](bool present, unsigned reg, uint32_t &v, Status &s, int &stage) {
                        v = 0; s = NOW; stage = -1;
                        if (!present) return;
                        v = gpr[reg];
                        for (int k = 0; k < 5; ++k) {
                            if (prod[k].valid && prod[k].dst == reg) {
                                v = prod[k].value; s = status(k, prod[k].kind); stage = k;
                                return;
                            }
                        }
                    };
                    uint32_t bv, xv;
                    Status bs, xs;
                    int bstage, xstage;
                    operand(mode.base_ok, mode.base, bv, bs, bstage);
                    operand(mode.index_ok, mode.index, xv, xs, xstage);
                    bool expect_stall = bs == WAIT || xs == WAIT || (bs == LATE_VALUE && xs == LATE_VALUE)
                                     || (xs == LATE_VALUE && mode.scale != 0);

                    top.modrm = modrm;
                    top.sib = sib;
                    top.addr32 = addr32;
                    top.seg_base = seg;
                    top.disp = disp;
                    for (int i = 0; i < 8; ++i) top.gpr[i] = gpr[i];
                    top.prod_valid = 0; top.prod_dst = 0; top.prod_ready = 0; top.prod_late = 0;
                    for (int k = 0; k < 5; ++k) {
                        Status s = status(k, prod[k].kind);
                        top.prod_valid |= prod[k].valid << k;
                        top.prod_dst |= prod[k].dst << (3 * k);
                        top.prod_ready |= (s == NOW) << k;
                        top.prod_late |= (s == LATE_VALUE) << k;
                    }
                    // Result registers hold a final value only where the model says so.
                    top.stage_value[0] = status(1, prod[1].kind) == NOW ? prod[1].value : value();
                    top.stage_value[1] = status(2, prod[2].kind) == NOW ? prod[2].value : value();
                    top.stage_value[2] = status(3, prod[3].kind) == NOW ? prod[3].value : value();
                    top.stage_value[3] = prod[4].value;
                    top.late_m1 = status(0, prod[0].kind) == LATE_VALUE ? prod[0].value : value();
                    top.late_ex = status(2, prod[2].kind) == LATE_VALUE ? prod[2].value : value();
                    top.late_wb = status(3, prod[3].kind) == LATE_VALUE ? prod[3].value : value();
                    top.eval();

                    ++n.checked;
                    bool ok = bool(top.stall) == expect_stall;
                    if (expect_stall) {
                        if (bs == WAIT || xs == WAIT) ++n.stall_wait;
                        else if (bs == LATE_VALUE && xs == LATE_VALUE) ++n.stall_both_late;
                        else ++n.stall_scaled_late;
                    } else {
                        uint32_t offset, linear;
                        if (addr32) {
                            offset = bv + (xv << mode.scale) + disp;
                        } else {
                            uint32_t raw = (bv & 0xffff) + (xv & 0xffff) + disp;
                            n.wrapped += raw > 0xffff;
                            offset = raw & 0xffff;
                        }
                        linear = seg + offset;
                        ok = ok && top.linear == linear && top.offset == offset;
                        ++n.addresses;
                        auto source = [](int stage) { return stage == 0 ? 0 : stage == 2 ? 1 : 2; };
                        if (bs == LATE_VALUE) ++n.late_base[source(bstage)];
                        if (xs == LATE_VALUE) ++n.late_index[source(xstage)];
                        n.forwarded += (bstage > 0 && bs == NOW) + (xstage > 0 && xs == NOW);
                        n.from_file += (mode.base_ok && bstage < 0) + (mode.index_ok && xstage < 0);
                    }
                    if (!ok && ++n.failures <= 10)
                        std::printf("FAIL modrm %02x sib %02x addr32 %d: rtl stall %d linear %08x offset %08x, "
                                    "expected stall %d (base %d/%d, index %d/%d)\n",
                                    modrm, sib, addr32, top.stall, top.linear, top.offset, expect_stall,
                                    bs, bstage, xs, xstage);
                }
            }
        }
    }
    std::printf("{\"dag_cases\": %llu, \"addresses\": %llu, \"wrapped_16\": %llu, \"forwarded\": %llu, "
                "\"from_file\": %llu, \"late_base\": [%llu, %llu, %llu], \"late_index\": [%llu, %llu, %llu], "
                "\"stall_not_ready\": %llu, \"stall_both_late\": %llu, \"stall_scaled_late\": %llu, "
                "\"failures\": %llu}\n",
                (unsigned long long)n.checked, (unsigned long long)n.addresses, (unsigned long long)n.wrapped,
                (unsigned long long)n.forwarded, (unsigned long long)n.from_file,
                (unsigned long long)n.late_base[0], (unsigned long long)n.late_base[1],
                (unsigned long long)n.late_base[2], (unsigned long long)n.late_index[0],
                (unsigned long long)n.late_index[1], (unsigned long long)n.late_index[2],
                (unsigned long long)n.stall_wait, (unsigned long long)n.stall_both_late,
                (unsigned long long)n.stall_scaled_late, (unsigned long long)n.failures);
    return n.failures ? 1 : 0;
}
