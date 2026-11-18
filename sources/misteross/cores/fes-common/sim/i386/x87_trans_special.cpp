// SPDX-License-Identifier: MIT
// What the references do where the 387 manual is silent (#1958; the record's
// rules.md, "Where the manual is silent: measured rules").
//
// For each of the eight x87 transcendentals this runs the instruction on a
// fixed list of special operands -- zeros, infinities, NaNs, denormals, the
// unsupported formats, the exact cases, the 2^63 range boundary, an empty
// stack and a full one -- under a masked and an unmasked control word, and
// prints the whole x87 state afterwards, taken with FNSAVE so that reading it
// disturbs nothing:
//
//   <instruction> <cw> <case> | a=<first loaded> b=<second, or the only operand> stack=<preparation>
//                               sw=<status> tw=<tags> st0=<...> st1=<...> st2=<...>
//
// One line a case, the same lines from every reference, so two references
// are compared with diff. It judges nothing itself.
//
//   x87_trans_special host      this host's own x87
//   x87_trans_special oracle    the Bochs oracle (not in a FES_PROBE_HOST_ONLY build)
//
// Host only: g++ -std=c++17 -O2 -DFES_PROBE_HOST_ONLY x87_trans_special.cpp
#ifndef FES_PROBE_HOST_ONLY
#include "i386_oracle.h"
#endif

#include <cinttypes>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <memory>
#include <string>
#include <vector>

namespace {

struct F80 {
    uint64_t sig;
    uint16_t se;
};
struct Named {
    const char *name;
    F80 v;
};
// Sign and exponent field, then the significand.
const Named ONE[] = {
    {"+0", {0, 0x0000}}, {"-0", {0, 0x8000}},
    {"+1", {1ull << 63, 0x3fff}}, {"-1", {1ull << 63, 0xbfff}},
    {"+0.5", {1ull << 63, 0x3ffe}}, {"-0.5", {1ull << 63, 0xbffe}},
    {"+2", {1ull << 63, 0x4000}}, {"+4", {1ull << 63, 0x4001}},
    {"+3", {3ull << 62, 0x4000}}, {"-3", {3ull << 62, 0xc000}},
    {"+0.25", {1ull << 63, 0x3ffd}}, {"+0.3", {0x999999999999999aull, 0x3ffd}},
    {"-0.3", {0x999999999999999aull, 0xbffd}}, {"-0.99", {0xfd70a3d70a3d70a4ull, 0xbffe}},
    {"-1.5", {3ull << 62, 0xbfff}}, {"+1.5", {3ull << 62, 0x3fff}},
    {"+inf", {1ull << 63, 0x7fff}}, {"-inf", {1ull << 63, 0xffff}},
    {"qnan", {0xc000000000000001ull, 0x7fff}}, {"snan", {0x8000000000000001ull, 0x7fff}},
    {"+denormal", {1ull << 62, 0x0000}}, {"-denormal", {1ull << 62, 0x8000}},
    {"+min-denormal", {1, 0x0000}},
    {"+pseudo-denormal", {1ull << 63, 0x0000}},
    {"+unnormal", {1ull << 62, 0x3fff}}, {"+pseudo-zero", {0, 0x3fff}},
    {"+pseudo-nan", {1, 0x7fff}}, {"+pseudo-inf", {0, 0x7fff}},
    {"+2^-70", {1ull << 63, 0x3fff - 70}}, {"-2^-70", {1ull << 63, 0xbfff - 70}},
    {"+2^-16000", {1ull << 63, 0x3fff - 16000}},
    {"+2^63-ulp", {~0ull, 0x403d}}, {"+2^63", {1ull << 63, 0x403e}}, {"-2^63", {1ull << 63, 0xc03e}},
    {"+2^63+ulp", {(1ull << 63) + 1, 0x403e}}, {"+2^64", {1ull << 63, 0x403f}},
    {"+max", {~0ull, 0x7ffe}}, {"-max", {~0ull, 0xfffe}},
    {"+2^16000", {1ull << 63, 0x3fff + 16000}},
};
// The second operands paired with every first one in the two-operand forms.
const char *const PAIR[] = {"+0", "-0", "+1", "-1", "+2", "+0.5", "+0.3", "-0.3", "+inf", "-inf", "qnan", "snan",
                            "+denormal", "+unnormal", "+2^16000", "+2^-16000", "+max"};

enum Op { F2XM1, FYL2X, FPTAN, FPATAN, FYL2XP1, FSINCOS, FSIN, FCOS, OPS };
const char *const NAMES[OPS] = {"F2XM1", "FYL2X", "FPTAN", "FPATAN", "FYL2XP1", "FSINCOS", "FSIN", "FCOS"};
const uint8_t MODRM[OPS] = {0xf0, 0xf1, 0xf2, 0xf3, 0xf9, 0xfb, 0xfe, 0xff};
bool two_operands(Op op) { return op == FYL2X || op == FPATAN || op == FYL2XP1; }

// How the stack is prepared before the operands are loaded.
enum Stack { NORMAL, EMPTY, EMPTY_ST1, FULL };

struct Case {
    Op op;
    uint16_t cw;
    Stack stack;
    F80 a, b;          // a is loaded first (ST(1) of a two-operand form), then b
    std::string label;
};

struct Image {
    uint16_t sw = 0, tw = 0;
    F80 st[3] = {};    // the physical registers ST(0), ST(1), ST(2) name after the instruction
};

void to_bytes(const F80 &v, uint8_t *b) {
    std::memcpy(b, &v.sig, 8);
    b[8] = uint8_t(v.se);
    b[9] = uint8_t(v.se >> 8);
}

// FNSAVE's registers are in stack order, ST(0) first, in both layouts.
Image decode(const uint8_t *image, bool wide) {
    Image r;
    const unsigned step = wide ? 4 : 2;
    r.sw = uint16_t(image[step] | image[step + 1] << 8);
    r.tw = uint16_t(image[2 * step] | image[2 * step + 1] << 8);
    const uint8_t *regs = image + (wide ? 28 : 14);
    for (int n = 0; n < 3; ++n) {
        std::memcpy(&r.st[n].sig, regs + 10 * n, 8);
        r.st[n].se = uint16_t(regs[10 * n + 8] | regs[10 * n + 9] << 8);
    }
    return r;
}

// The same instruction stream for both references, as machine code: 16-bit
// operand and address sizes for the oracle's real mode, absolute operands.
// Layout of the data page: 0x500 cw, 0x510 a, 0x520 b, 0x600 the FNSAVE image.
[[maybe_unused]] std::vector<uint8_t> program(const Case &c) {
    std::vector<uint8_t> code;
    auto add = [&](std::initializer_list<uint8_t> bytes) { code.insert(code.end(), bytes); };
    add({0xdb, 0xe3});                               // FNINIT
    add({0xd9, 0x2e, 0x00, 0x05});                   // FLDCW [0500h]
    if (c.stack == FULL)                             // six registers under the operands
        for (int n = 0; n < (two_operands(c.op) ? 6 : 7); ++n) add({0xd9, 0xe8});   // FLD1
    if (c.stack != EMPTY) {
        if (two_operands(c.op) && c.stack != EMPTY_ST1) add({0xdb, 0x2e, 0x10, 0x05});   // FLD m80 a
        add({0xdb, 0x2e, 0x20, 0x05});               // FLD m80 b (or the only operand)
    }
    add({0xd9, MODRM[c.op]});
    add({0xdd, 0x36, 0x00, 0x06});                   // FNSAVE [0600h]
    add({0xf4});                                     // HLT
    return code;
}

#if defined(__x86_64__) || defined(__i386__)
__attribute__((noinline)) Image host_run(const Case &c) {
    uint8_t image[108] = {0}, a[10], b[10];
    uint16_t cw = c.cw;
    to_bytes(c.a, a);
    to_bytes(c.b, b);
    const int fill = c.stack == FULL ? (two_operands(c.op) ? 6 : 7) : 0;
    const int load_a = c.stack != EMPTY && two_operands(c.op) && c.stack != EMPTY_ST1;
    const int load_b = c.stack != EMPTY;
#define RUN(INSN)                                                                                   \
    __asm__ volatile("fninit\n fldcw (%0)\n"                                                        \
                     "mov %4, %%ecx\n 1: test %%ecx, %%ecx\n jz 2f\n fld1\n dec %%ecx\n jmp 1b\n 2:\n" \
                     "test %5, %5\n jz 3f\n fldt (%1)\n 3:\n"                                       \
                     "test %6, %6\n jz 4f\n fldt (%2)\n 4:\n" INSN "\n fnsave (%3)\n fninit\n"      \
                     : : "r"(&cw), "r"(a), "r"(b), "r"(image), "r"(fill), "r"(load_a), "r"(load_b)  \
                     : "memory", "cc", "ecx", "st", "st(1)", "st(2)", "st(3)", "st(4)", "st(5)", "st(6)", "st(7)")
    switch (c.op) {
        case F2XM1: RUN("f2xm1"); break;
        case FYL2X: RUN("fyl2x"); break;
        case FPTAN: RUN("fptan"); break;
        case FPATAN: RUN("fpatan"); break;
        case FYL2XP1: RUN("fyl2xp1"); break;
        case FSINCOS: RUN("fsincos"); break;
        case FSIN: RUN("fsin"); break;
        default: RUN("fcos"); break;
    }
#undef RUN
    return decode(image, true);
}
#endif

#ifndef FES_PROBE_HOST_ONLY
class FlatBus final : public fes_i386::Bus {
public:
    fes_i386::PageMemory ram{1u << 20};
    uint8_t *page(uint32_t paddr, bool) override { return paddr < ram.size() ? &ram[paddr & ~0xfffu] : nullptr; }
    void read(uint32_t, unsigned length, uint8_t *data) override { std::memset(data, 0xff, length); }
    void write(uint32_t, unsigned, const uint8_t *) override {}
    uint32_t in(uint16_t, unsigned) override { return 0xffffffffu; }
    void out(uint16_t, unsigned, uint32_t) override {}
    uint8_t interrupt_acknowledge() override { return 0xff; }
};
struct OracleRun {
    FlatBus bus;
    std::unique_ptr<fes_i386::Oracle> oracle = fes_i386::make_oracle(bus);
    bool ok = true;
    Image run(const Case &c) {
        const std::vector<uint8_t> code = program(c);
        std::memset(&bus.ram[0x500], 0, 0x200);
        bus.ram[0x500] = uint8_t(c.cw);
        bus.ram[0x501] = uint8_t(c.cw >> 8);
        to_bytes(c.a, &bus.ram[0x510]);
        to_bytes(c.b, &bus.ram[0x520]);
        for (size_t n = 0; n < code.size(); ++n) bus.ram[0x1000 + n] = code[n];
        // The AT coupling is left off: an unmasked exception then holds
        // nothing, and FNSAVE is a no-wait form in any case.
        oracle->set_at_coprocessor(false);
        oracle->reset();
        oracle->invalidate(0, uint32_t(bus.ram.size()));
        fes_i386::State s = oracle->get_state();
        for (int n = 0; n < fes_i386::SEG_COUNT; ++n) s.seg[n] = fes_i386::real_mode_segment(0);
        s.eip = 0x1000;
        s.gpr[fes_i386::REG_ESP] = 0x8000;
        s.eflags = 0x2;
        oracle->set_state(s);
        oracle->set_intr(false);
        bool halted = false;
        for (unsigned n = 0; n < 64 && !halted; ++n) halted = oracle->step().halted;
        if (!halted) ok = false;
        return decode(&bus.ram[0x600], false);
    }
};
#endif

const Named &named(const char *name) {
    for (const Named &n : ONE)
        if (!std::strcmp(n.name, name)) return n;
    std::fprintf(stderr, "no operand %s\n", name);
    std::abort();
}

std::vector<Case> cases() {
    std::vector<Case> all;
    // 037F: everything masked. 0340: everything unmasked.
    for (uint16_t cw : {uint16_t(0x037f), uint16_t(0x0340)}) {
        for (int i = 0; i < OPS; ++i) {
            const Op op = Op(i);
            if (!two_operands(op)) {
                for (const Named &x : ONE) all.push_back({op, cw, NORMAL, {}, x.v, x.name});
                all.push_back({op, cw, EMPTY, {}, {}, "empty"});
                all.push_back({op, cw, FULL, {}, named("+0.5").v, "full +0.5"});
                all.push_back({op, cw, FULL, {}, named("+2^63").v, "full +2^63"});
                all.push_back({op, cw, FULL, {}, named("+inf").v, "full +inf"});
            } else {
                for (const Named &x : ONE)
                    for (const char *y : PAIR)
                        all.push_back({op, cw, NORMAL, named(y).v, x.v, std::string("st1=") + y + " st0=" + x.name});
                all.push_back({op, cw, EMPTY, {}, {}, "empty"});
                all.push_back({op, cw, EMPTY_ST1, {}, named("+2").v, "st1=empty st0=+2"});
            }
        }
    }
    // The rounding modes, on one operand that is not special.
    for (uint16_t cw : {uint16_t(0x077f), uint16_t(0x0b7f), uint16_t(0x0f7f), uint16_t(0x007f), uint16_t(0x027f)})
        for (int i = 0; i < OPS; ++i)
            all.push_back({Op(i), cw, NORMAL, named("+0.3").v, named("+0.3").v,
                           two_operands(Op(i)) ? "st1=+0.3 st0=+0.3" : "+0.3"});
    return all;
}

}  // namespace

int main(int argc, char **argv) {
    const bool host = argc == 2 && !std::strcmp(argv[1], "host");
    const bool oracle = argc == 2 && !std::strcmp(argv[1], "oracle");
    if (!host && !oracle) {
        std::fprintf(stderr, "usage: x87_trans_special host|oracle\n");
        return 2;
    }
#ifdef FES_PROBE_HOST_ONLY
    if (oracle) {
        std::fprintf(stderr, "this build has no oracle\n");
        return 2;
    }
#else
    OracleRun run;
#endif
    for (const Case &c : cases()) {
        Image r;
#ifndef FES_PROBE_HOST_ONLY
        if (oracle) r = run.run(c);
        else
#endif
            r = host_run(c);
        std::printf("%s %04x %s | a=%04x%016" PRIx64 " b=%04x%016" PRIx64 " stack=%d sw=%04x tw=%04x", NAMES[c.op],
                    unsigned(c.cw), c.label.c_str(), unsigned(c.a.se), c.a.sig, unsigned(c.b.se), c.b.sig, int(c.stack),
                    unsigned(r.sw), unsigned(r.tw));
        for (int n = 0; n < 3; ++n) std::printf(" st%d=%04x%016" PRIx64, n, unsigned(r.st[n].se), r.st[n].sig);
        std::printf("\n");
    }
#ifndef FES_PROBE_HOST_ONLY
    if (oracle && !run.ok) return 1;
#endif
    return 0;
}
