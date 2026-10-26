// SPDX-License-Identifier: MIT
// D's address operands and AG (dag_tb_top) against the architectural address
// and the late-operand rules, cycle by cycle.
//
// D works its forwarding out a cycle ahead (fes_i386_dfwd), so this bench runs
// a pipeline: each cycle D holds a uop or nothing, and the uops it let go move
// through AG, M1, M2, EX and WB. A uop that writes a GPR is a producer whose
// result is early (end of AG), load data (end of M2) or late (end of EX). Each
// cycle the model works out each address register's youngest value from the
// producers then in flight, and when it is available:
//   in a result register now                  D forwards it
//   completing this cycle (AG early, M2 load, EX late)
//                                             late into AG's last row
//   later                                     D stalls
// D must stall exactly when an operand is not yet available, when both
// operands are late, or when the late one is a scaled index; a stalled uop
// stays in D and is checked again next cycle. Otherwise AG's linear address
// and offset must equal the architectural ones: 32-bit sums wrap at 4 GiB,
// and a 16-bit offset wraps at 64 KiB before the segment base is added.
//
// The uops are every memory ModRM (mod 0-2) with every SIB byte in both
// address sizes, FES_I386_DAG_ROUNDS times over (default 16). Each uop's own
// destination is often an address register of one of the next three, so
// back-to-back and near dependencies occur on every kind; D is sometimes
// empty. Then directed pairs time each kind of result into the next uop's
// base, unscaled index and scaled index against the design's latencies.
// Output: one JSON line; exit status 1 on any failure.
#include "Vdag_tb_top.h"
#include "verilated.h"

#include <cstdio>
#include <cstdlib>
#include <random>

namespace {

enum Kind { EARLY, LOAD, LATE };
enum Status { NOW, LATE_VALUE, WAIT };

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

struct Uop {
    bool valid = false;
    unsigned modrm = 0, sib = 0;
    bool addr32 = false;
    Mode mode;
    bool writes = false;  // a producer: writes dst, ready per kind
    unsigned dst = 0;
    Kind kind = EARLY;
    uint32_t seg = 0, disp = 0;
};

struct Producer {
    bool valid = false;
    unsigned dst = 0;
    Kind kind = EARLY;
    uint32_t value = 0;
};

struct Counts {
    uint64_t uops = 0, checked = 0, failures = 0;
    uint64_t addresses = 0, wrapped = 0, forwarded = 0, from_file = 0;
    uint64_t late_base[3] = {}, late_index[3] = {};
    uint64_t stall_wait = 0, stall_both_late = 0, stall_scaled_late = 0;
    uint64_t from_ag = 0;    // checks whose operand's youngest producer is the uop just ahead
    uint64_t released = 0;   // uops that stalled and then left D
};

class Bench {
public:
    Bench(VerilatedContext &context, std::mt19937_64 &random) : top(&context), random(random) {}

    Vdag_tb_top top;
    std::mt19937_64 &random;
    Producer stage[5];
    Counts n;

    uint32_t value() {
        static const uint32_t edges[] = {0, 1, 0x7fff, 0x8000, 0xfffe, 0xffff, 0x10000, 0xffff0000,
                                         0xfffffff0, 0xffffffff};
        uint32_t v = uint32_t(random());
        switch (random() % 4) {
        case 0: return edges[random() % (sizeof edges / sizeof edges[0])];
        case 1: return v & 0xffff;
        default: return v;
        }
    }

    void reset() {
        for (Producer &p : stage) p = Producer();
        top.reset = 1;
        top.d_valid = 0;
        top.prod_valid = 0;
        top.clk = 0;
        top.eval();
        top.clk = 1;
        top.eval();
        top.reset = 0;
    }

    // One cycle with d in D: check D and AG, clock, move the producers.
    // Returns whether d left D.
    bool cycle(const Uop &d) {
        uint32_t gpr[8];
        for (auto &g : gpr) g = value();
        top.d_valid = d.valid;
        top.modrm = d.modrm;
        top.sib = d.sib;
        top.addr32 = d.addr32;
        top.d_writes = d.writes;
        top.d_dst = d.dst;
        top.d_kind = d.kind;
        top.seg_base = d.seg;
        top.disp = d.disp;
        for (int i = 0; i < 8; ++i) top.gpr[i] = gpr[i];
        top.prod_valid = 0; top.prod_dst = 0; top.prod_kind = 0;
        for (int k = 0; k < 4; ++k) {
            top.prod_valid |= stage[k].valid << k;
            top.prod_dst |= stage[k].dst << (3 * k);
            top.prod_kind |= unsigned(stage[k].kind) << (2 * k);
        }
        // Result registers hold a final value only where the model says so.
        for (int k = 1; k < 5; ++k)
            top.stage_value[k - 1] = stage[k].valid && status(k, stage[k].kind) == NOW ? stage[k].value : value();
        auto late_value = [&](int k) {
            return stage[k].valid && status(k, stage[k].kind) == LATE_VALUE ? stage[k].value : value();
        };
        top.late_m1 = late_value(0);
        top.late_ex = late_value(2);
        top.late_wb = late_value(3);
        top.clk = 0;
        top.eval();

        bool stall = top.stall;
        if (d.valid) check(d, gpr);
        bool leaves = d.valid && !stall;
        top.clk = 1;
        top.eval();
        for (int k = 4; k > 0; --k) stage[k] = stage[k - 1];
        stage[0] = Producer();
        if (leaves && d.writes) stage[0] = {true, d.dst, d.kind, value()};
        return leaves;
    }

private:
    // What each register holds architecturally, and when.
    void operand(bool present, unsigned reg, const uint32_t *gpr, uint32_t &v, Status &s, int &k) {
        v = 0; s = NOW; k = -1;
        if (!present) return;
        v = gpr[reg];
        for (int i = 0; i < 5; ++i) {
            if (stage[i].valid && stage[i].dst == reg) {
                v = stage[i].value; s = status(i, stage[i].kind); k = i;
                return;
            }
        }
    }

    void check(const Uop &d, const uint32_t *gpr) {
        const Mode &mode = d.mode;
        uint32_t bv, xv;
        Status bs, xs;
        int bk, xk;
        operand(mode.base_ok, mode.base, gpr, bv, bs, bk);
        operand(mode.index_ok, mode.index, gpr, xv, xs, xk);
        bool expect_stall = bs == WAIT || xs == WAIT || (bs == LATE_VALUE && xs == LATE_VALUE)
                         || (xs == LATE_VALUE && mode.scale != 0);
        ++n.checked;
        n.from_ag += (bk == 0) + (xk == 0);
        bool ok = bool(top.stall) == expect_stall;
        if (expect_stall) {
            if (bs == WAIT || xs == WAIT) ++n.stall_wait;
            else if (bs == LATE_VALUE && xs == LATE_VALUE) ++n.stall_both_late;
            else ++n.stall_scaled_late;
        } else {
            uint32_t offset, linear;
            if (d.addr32) {
                offset = bv + (xv << mode.scale) + d.disp;
            } else {
                uint32_t raw = (bv & 0xffff) + (xv & 0xffff) + d.disp;
                n.wrapped += raw > 0xffff;
                offset = raw & 0xffff;
            }
            linear = d.seg + offset;
            ok = ok && top.linear == linear && top.offset == offset;
            ++n.addresses;
            auto source = [](int k) { return k == 0 ? 0 : k == 2 ? 1 : 2; };
            if (bs == LATE_VALUE) ++n.late_base[source(bk)];
            if (xs == LATE_VALUE) ++n.late_index[source(xk)];
            n.forwarded += (bk > 0 && bs == NOW) + (xk > 0 && xs == NOW);
            n.from_file += (mode.base_ok && bk < 0) + (mode.index_ok && xk < 0);
        }
        if (!ok && ++n.failures <= 10)
            std::printf("FAIL modrm %02x sib %02x addr32 %d: rtl stall %d linear %08x offset %08x, "
                        "expected stall %d (base %d/%d, index %d/%d)\n",
                        d.modrm, d.sib, d.addr32, top.stall, top.linear, top.offset, expect_stall,
                        bs, bk, xs, xk);
    }
};

// Uop i of the enumeration: ModRM 00-BF, SIB, address size, repeated.
constexpr unsigned kCases = 0xc0 * 256 * 2;

Uop enumerated(uint64_t i) {
    Uop u;
    unsigned c = unsigned(i % kCases);
    u.valid = true;
    u.modrm = c / 512;
    u.sib = (c / 2) % 256;
    u.addr32 = c % 2;
    u.mode = addressing(u.modrm, u.sib, u.addr32);
    return u;
}

// The cycles a uop reading the register in role (0 base, 1 unscaled index,
// 2 scaled index) waits in D behind the uop just ahead writing it.
unsigned wait_behind(Bench &bench, Kind kind, int role) {
    bench.reset();
    Uop producer;
    producer.valid = true;
    producer.modrm = 0x05;  // [disp32]: no address register
    producer.addr32 = true;
    producer.mode = addressing(producer.modrm, 0, true);
    producer.writes = true;
    producer.dst = 3;       // EBX
    producer.kind = kind;
    Uop consumer;
    consumer.valid = true;
    consumer.addr32 = true;
    consumer.modrm = role == 0 ? 0x03 : 0x04;          // [EBX], or SIB
    consumer.sib = role == 1 ? 0x18 : 0x98;            // [EAX + EBX], [EAX + EBX*4]
    consumer.mode = addressing(consumer.modrm, consumer.sib, true);
    bench.cycle(producer);
    unsigned waited = 0;
    while (!bench.cycle(consumer) && waited < 10) ++waited;
    return waited;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    VerilatedContext context;
    std::mt19937_64 random(1);
    Bench bench(context, random);
    unsigned rounds = 16;
    if (const char *value = std::getenv("FES_I386_DAG_ROUNDS")) rounds = unsigned(std::atoi(value));

    // The enumeration as a stream, with producers biased onto the next uops' registers.
    bench.reset();
    const uint64_t total = uint64_t(rounds) * kCases;
    std::bernoulli_distribution empty(0.1), writes(0.8);
    for (uint64_t i = 0; i < total; ++i) {
        Uop d = enumerated(i);
        d.writes = writes(random);
        d.kind = Kind(random() % 3);
        d.seg = bench.value();
        d.disp = d.addr32 ? bench.value() : bench.value() & 0xffff;
        const Mode next = enumerated(i + 1 + random() % 3).mode;
        switch (random() % 5) {
        case 0: case 1: d.dst = next.base_ok ? next.base : unsigned(random() % 8); break;
        case 2: case 3: d.dst = next.index_ok ? next.index : unsigned(random() % 8); break;
        default: d.dst = unsigned(random() % 8); break;
        }
        while (empty(random)) bench.cycle(Uop());
        ++bench.n.uops;
        bool stalled = false;
        unsigned guard = 0;
        while (!bench.cycle(d)) {
            stalled = true;
            if (++guard > 10) {
                ++bench.n.failures;
                std::printf("FAIL modrm %02x sib %02x addr32 %d: still in D after 10 cycles\n", d.modrm, d.sib,
                            d.addr32);
                break;
            }
        }
        bench.n.released += stalled;
    }

    // Directed: each kind into the next uop's base, unscaled and scaled index,
    // against the design's latencies: an early result costs nothing, a load
    // two cycles, a late result three, and a scaled index one more.
    static const unsigned expected[3][3] = {{0, 0, 1}, {2, 2, 3}, {3, 3, 4}};
    unsigned measured[3][3];
    bool directed_ok = true;
    for (int kind = 0; kind < 3; ++kind)
        for (int role = 0; role < 3; ++role) {
            measured[kind][role] = wait_behind(bench, Kind(kind), role);
            if (measured[kind][role] != expected[kind][role]) {
                directed_ok = false;
                std::printf("FAIL kind %d role %d: waited %u cycles, expected %u\n", kind, role,
                            measured[kind][role], expected[kind][role]);
            }
        }

    const Counts &n = bench.n;
    std::printf("{\"uops\": %llu, \"dag_cases\": %llu, \"addresses\": %llu, \"wrapped_16\": %llu, "
                "\"forwarded\": %llu, \"from_file\": %llu, \"late_base\": [%llu, %llu, %llu], "
                "\"late_index\": [%llu, %llu, %llu], \"stall_not_ready\": %llu, \"stall_both_late\": %llu, "
                "\"stall_scaled_late\": %llu, \"from_ag\": %llu, \"released\": %llu, "
                "\"wait_cycles\": {\"early\": [%u, %u, %u], \"load\": [%u, %u, %u], \"late\": [%u, %u, %u]}, "
                "\"failures\": %llu}\n",
                (unsigned long long)n.uops, (unsigned long long)n.checked, (unsigned long long)n.addresses,
                (unsigned long long)n.wrapped, (unsigned long long)n.forwarded, (unsigned long long)n.from_file,
                (unsigned long long)n.late_base[0], (unsigned long long)n.late_base[1],
                (unsigned long long)n.late_base[2], (unsigned long long)n.late_index[0],
                (unsigned long long)n.late_index[1], (unsigned long long)n.late_index[2],
                (unsigned long long)n.stall_wait, (unsigned long long)n.stall_both_late,
                (unsigned long long)n.stall_scaled_late, (unsigned long long)n.from_ag,
                (unsigned long long)n.released, measured[0][0], measured[0][1], measured[0][2], measured[1][0],
                measured[1][1], measured[1][2], measured[2][0], measured[2][1], measured[2][2],
                (unsigned long long)(n.failures + !directed_ok));
    return n.failures || !directed_ok ? 1 : 0;
}
