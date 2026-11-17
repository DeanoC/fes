// SPDX-License-Identifier: MIT
// How far apart the two references are on the x87 transcendentals (#1958).
//
// The unit bench compares fes_i386_x87 with the host's own x87 and the
// lockstep compares it with the Bochs oracle. For the transcendentals neither
// is a 387 and the two do not agree with each other, so a tolerance has to be
// at least as wide as their own disagreement wherever both are to pass. This
// probe measures that disagreement and nothing else: it runs each instruction
// on the oracle and on the host over the same operands and reports the
// distance between the two results, counted in representable extended values
// (the record's method.md, "The eight transcendentals", rule 3), and whether
// the exception flags and C0, C2 and C3 agree.
//
// It runs no RTL. It reads the oracle only through the oracle interface, as
// a program's results; nothing of Bochs's source is consulted.
//
//   x87_trans_probe <samples> <seed>
//   x87_trans_probe --vectors <file>
//   x87_trans_probe --host-dump <samples> <seed> [<vectors file>]
//
// The second form judges each reference separately against a third: lines of
// "<instruction> <a> <b> <down> <up>", 20 hex digits a value, where down and
// up are the two extended values that bracket the mathematical result (equal
// where it is exact). scripts/probe_fes_i386_x87_trans.py writes that file at
// run time from QEMU's pinned tests/tcg/i386 tables, which are never
// committed. Each reference's distance outside the bracket is reported, zero
// meaning it returned one of the two neighbours of the true value.
//
// Output: one JSON object on stdout. Each instruction has operand classes;
// each class reports the count, how many results were bit-identical, a
// cumulative histogram of the distance, the largest distance with its
// operands, and the count of flag disagreements.
//
// The third form runs the host alone, over the same operands as the first
// form and then the vector file's, and writes one line a result:
// "<instruction> <class> <a> <b> <ST(0)> <ST(1)> <status word>". Two hosts'
// dumps compared line by line say whether two x87 units agree. Built with
// -DFES_PROBE_HOST_ONLY it needs no oracle, so it compiles on a host that has
// none: g++ -std=c++17 -O2 -DFES_PROBE_HOST_ONLY x87_trans_probe.cpp.
// scripts/probe_fes_i386_x87_trans.py builds and runs it.
#ifndef FES_PROBE_HOST_ONLY
#include "i386_oracle.h"
#endif

#include <cinttypes>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <memory>
#include <string>
#include <vector>

namespace {

#ifndef FES_PROBE_HOST_ONLY
using fes_i386::State;
using fes_i386::StepResult;
#endif

struct F80 {
    uint64_t sig = 0;
    uint16_t se = 0;
    bool operator==(const F80 &o) const { return sig == o.sig && se == o.se; }
};
static F80 make(bool sign, int exp_unbiased, uint64_t sig) {
    return {sig, uint16_t((sign ? 0x8000 : 0) | ((exp_unbiased + 16383) & 0x7fff))};
}
static void to_bytes(const F80 &v, uint8_t *b) {
    std::memcpy(b, &v.sig, 8);
    b[8] = uint8_t(v.se);
    b[9] = uint8_t(v.se >> 8);
}
static F80 from_bytes(const uint8_t *b) {
    F80 v;
    std::memcpy(&v.sig, b, 8);
    v.se = uint16_t(b[8] | b[9] << 8);
    return v;
}
// A finite, non-zero normal or denormal value.
static bool ordinary(const F80 &v) {
    const unsigned e = v.se & 0x7fff;
    if (e == 0x7fff || v.sig == 0) return false;
    return e == 0 ? !(v.sig >> 63) : bool(v.sig >> 63);
}
// The integer that orders the format's magnitudes: consecutive representable
// values are consecutive integers, across binades and into the denormals.
static unsigned __int128 ordinal(const F80 &v) {
    const unsigned e = v.se & 0x7fff;
    if (e == 0) return v.sig;
    return (unsigned __int128)(e - 1) << 63 | v.sig;
}

enum Op { F2XM1, FYL2X, FPTAN, FPATAN, FYL2XP1, FSINCOS, FSIN, FCOS, OPS };
const char *const NAMES[OPS] = {"F2XM1", "FYL2X", "FPTAN", "FPATAN", "FYL2XP1", "FSINCOS", "FSIN", "FCOS"};
const uint8_t MODRM[OPS] = {0xf0, 0xf1, 0xf2, 0xf3, 0xf9, 0xfb, 0xfe, 0xff};
[[maybe_unused]] static bool two_operands(Op op) { return op == FYL2X || op == FPATAN || op == FYL2XP1; }
static bool two_results(Op op) { return op == FPTAN || op == FSINCOS; }

struct Result {
    F80 r0, r1;     // ST(0) and ST(1) after the instruction
    uint16_t sw = 0;
};

// The host's own instruction. `a` is loaded first, so for a two-operand form
// it is ST(1) and `b` is ST(0), as FYL2X, FYL2XP1 and FPATAN take them.
__attribute__((noinline)) static Result host(Op op, const F80 &a, const F80 &b) {
    uint8_t in0[10], in1[10], out0[10] = {0}, out1[10] = {0};
    uint16_t sw = 0;
    to_bytes(a, in0);
    to_bytes(b, in1);
#define RUN(INSN, LOAD2, STORE2)                                                              \
    __asm__ volatile("fninit\n fldt (%0)\n" LOAD2 INSN "\n fnstsw (%4)\n fstpt (%2)\n" STORE2 \
                     "fninit\n"                                                               \
                     : : "r"(in0), "r"(in1), "r"(out0), "r"(out1), "r"(&sw)                   \
                     : "memory", "st", "st(1)", "st(2)", "st(3)", "st(4)", "st(5)", "st(6)", "st(7)")
    switch (op) {
        case F2XM1: RUN("f2xm1", "", ""); break;
        case FYL2X: RUN("fyl2x", "fldt (%1)\n", ""); break;
        case FPTAN: RUN("fptan", "", "fstpt (%3)\n"); break;
        case FPATAN: RUN("fpatan", "fldt (%1)\n", ""); break;
        case FYL2XP1: RUN("fyl2xp1", "fldt (%1)\n", ""); break;
        case FSINCOS: RUN("fsincos", "", "fstpt (%3)\n"); break;
        case FSIN: RUN("fsin", "", ""); break;
        default: RUN("fcos", "", ""); break;
    }
#undef RUN
    return {from_bytes(out0), from_bytes(out1), sw};
}

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

constexpr uint32_t CODE = 0x1000, IN0 = 0x520, IN1 = 0x530, OUT0 = 0x540, OUT1 = 0x550, SW = 0x560;

class OracleRun {
public:
    FlatBus bus;
    std::unique_ptr<fes_i386::Oracle> oracle = fes_i386::make_oracle(bus);
    bool ok = true;

    Result run(Op op, const F80 &a, const F80 &b) {
        std::vector<uint8_t> code = {0xdb, 0xe3,                                  // FNINIT
                                     0xdb, 0x2e, uint8_t(IN0), uint8_t(IN0 >> 8)};   // FLD m80
        auto add = [&](std::initializer_list<uint8_t> bytes) { code.insert(code.end(), bytes); };
        if (two_operands(op)) add({0xdb, 0x2e, uint8_t(IN1), uint8_t(IN1 >> 8)});
        add({0xd9, MODRM[op]});
        add({0xdd, 0x3e, uint8_t(SW), uint8_t(SW >> 8)});                         // FNSTSW m16
        add({0xdb, 0x3e, uint8_t(OUT0), uint8_t(OUT0 >> 8)});                     // FSTP m80
        if (two_results(op)) add({0xdb, 0x3e, uint8_t(OUT1), uint8_t(OUT1 >> 8)});
        add({0xf4});                                                              // HLT
        std::memset(&bus.ram[0x500], 0, 0x100);
        to_bytes(a, &bus.ram[IN0]);
        to_bytes(b, &bus.ram[IN1]);
        for (size_t n = 0; n < code.size(); ++n) bus.ram[CODE + n] = code[n];
        oracle->set_at_coprocessor(true);
        oracle->reset();
        oracle->invalidate(0, uint32_t(bus.ram.size()));
        State s = oracle->get_state();
        for (int n = 0; n < fes_i386::SEG_COUNT; ++n) s.seg[n] = fes_i386::real_mode_segment(0);
        s.eip = CODE;
        s.gpr[fes_i386::REG_ESP] = 0x8000;
        s.eflags = 0x2;
        oracle->set_state(s);
        oracle->set_intr(false);
        bool halted = false;
        for (unsigned n = 0; n < 64 && !halted; ++n) halted = oracle->step().halted;
        if (!halted) ok = false;
        Result r;
        r.r0 = from_bytes(&bus.ram[OUT0]);
        r.r1 = from_bytes(&bus.ram[OUT1]);
        r.sw = uint16_t(bus.ram[SW] | bus.ram[SW + 1] << 8);
        return r;
    }
};

#else
// No oracle in this build: only --host-dump is offered.
class OracleRun {
public:
    bool ok = true;
    Result run(Op, const F80 &, const F80 &) { return {}; }
};
#endif

std::FILE *host_dump = nullptr;
static void dump_row(Op op, int cls, const F80 &a, const F80 &b, const Result &h) {
    std::fprintf(host_dump, "%s %d %04x%016" PRIx64 " %04x%016" PRIx64 " %04x%016" PRIx64 " %04x%016" PRIx64 " %04x\n",
                 NAMES[op], cls, unsigned(a.se), a.sig, unsigned(b.se), b.sig, unsigned(h.r0.se), h.r0.sig,
                 unsigned(h.r1.se), h.r1.sig, unsigned(h.sw));
}

struct Rng {
    uint64_t s;
    uint64_t next() {
        s ^= s << 13;
        s ^= s >> 7;
        s ^= s << 17;
        return s;
    }
    // A normal value with a uniform exponent in [lo, hi] and random bits.
    F80 value(int lo, int hi, int sign) {
        const int e = lo + int(next() % uint64_t(hi - lo + 1));
        const bool neg = sign < 0 || (sign == 0 && (next() & 1));
        return make(neg, e, next() | 1ull << 63);
    }
};

// pi/2 to 64 bits, for operands near a multiple of it. This is the nearest
// extended value to pi/2 and is used only to place operands, not to judge a
// result.
const uint64_t HALF_PI_SIG = 0xc90fdaa22168c235ull;

struct ClassStats {
    std::string name;
    uint64_t count = 0, identical = 0, flags_differ = 0, cc_differ = 0, c1_differ = 0, unordinary = 0;
    static constexpr int BINS = 9;
    const uint64_t edges[BINS] = {0, 1, 2, 4, 16, 256, 65536, 1ull << 32, ~0ull};
    uint64_t at_most[BINS] = {0};
    unsigned __int128 worst = 0;
    F80 worst_a, worst_b, worst_oracle, worst_host;
};

static void account(ClassStats &st, const F80 &a, const F80 &b, const F80 &o, const F80 &h) {
    if (!ordinary(o) || !ordinary(h) || ((o.se ^ h.se) & 0x8000)) {
        // Zeros, infinities, NaNs, or results of opposite sign: not a
        // distance. Counted, and identical ones still count as identical.
        if (!(o == h)) ++st.unordinary;
        return;
    }
    const unsigned __int128 x = ordinal(o), y = ordinal(h);
    const unsigned __int128 d = x > y ? x - y : y - x;
    for (int n = 0; n < ClassStats::BINS; ++n)
        if (d <= st.edges[n]) ++st.at_most[n];
    if (d > st.worst) {
        st.worst = d;
        st.worst_a = a;
        st.worst_b = b;
        st.worst_oracle = o;
        st.worst_host = h;
    }
}

static void print_f80(const char *key, const F80 &v, const char *tail) {
    std::printf("\"%s\": \"%04x%016" PRIx64 "\"%s", key, unsigned(v.se), v.sig, tail);
}

}  // namespace

static F80 parse_f80(const char *text) {
    F80 v;
    char head[5] = {text[0], text[1], text[2], text[3], 0};
    v.se = uint16_t(std::strtoul(head, nullptr, 16));
    v.sig = std::strtoull(text + 4, nullptr, 16);
    return v;
}

// How far `r` lies outside [down, up], in representable values; 0 inside.
// A result that is not comparable with the bracket (a zero or infinity that
// is not the bracket's own, or the wrong sign) is counted apart.
static bool outside(const F80 &r, const F80 &down, const F80 &up, unsigned __int128 &d) {
    d = 0;
    if (r == down || r == up) return true;
    if (!ordinary(r) || !ordinary(down) || !ordinary(up) || ((r.se ^ down.se) & 0x8000) || ((r.se ^ up.se) & 0x8000))
        return false;
    const unsigned __int128 x = ordinal(r), lo = ordinal(down) < ordinal(up) ? ordinal(down) : ordinal(up),
                            hi = ordinal(down) < ordinal(up) ? ordinal(up) : ordinal(down);
    d = x < lo ? lo - x : x > hi ? x - hi : 0;
    return true;
}

static int run_vectors(const char *path) {
    std::FILE *file = std::fopen(path, "r");
    if (!file) {
        std::fprintf(stderr, "cannot read %s\n", path);
        return 2;
    }
    struct Side {
        uint64_t inside = 0, incomparable = 0;
        uint64_t at_most[ClassStats::BINS] = {0};
        unsigned __int128 worst = 0;
        F80 worst_a, worst_b, worst_r;
    };
    struct Per {
        uint64_t count = 0;
        Side oracle, host;
    } per[OPS];
    const uint64_t edges[ClassStats::BINS] = {0, 1, 2, 4, 16, 256, 65536, 1ull << 32, ~0ull};
    OracleRun oracle;
    char name[16], ta[32], tb[32], td[32], tu[32];
    while (std::fscanf(file, "%15s %31s %31s %31s %31s", name, ta, tb, td, tu) == 5) {
        int op = -1;
        for (int i = 0; i < OPS; ++i)
            if (!std::strcmp(name, NAMES[i])) op = i;
        if (op < 0 || two_results(Op(op))) {
            std::fprintf(stderr, "unknown instruction %s\n", name);
            return 2;
        }
        const F80 a = parse_f80(ta), b = parse_f80(tb), down = parse_f80(td), up = parse_f80(tu);
        const Result h = host(Op(op), a, b);
        if (host_dump) {
            dump_row(Op(op), -1, a, b, h);
            continue;
        }
        const Result o = oracle.run(Op(op), a, b);
        ++per[op].count;
        auto tally = [&](Side &side, const F80 &r) {
            unsigned __int128 d;
            if (!outside(r, down, up, d)) {
                ++side.incomparable;
                return;
            }
            if (d == 0) ++side.inside;
            for (int n = 0; n < ClassStats::BINS; ++n)
                if (d <= edges[n]) ++side.at_most[n];
            if (d > side.worst) {
                side.worst = d;
                side.worst_a = a;
                side.worst_b = b;
                side.worst_r = r;
            }
        };
        tally(per[op].oracle, o.r0);
        tally(per[op].host, h.r0);
    }
    std::fclose(file);
    if (host_dump) return 0;
    const char *const labels[ClassStats::BINS] = {"0", "1", "2", "4", "16", "256", "65536", "2^32", "any"};
    std::printf("{\"brackets\": {\n");
    bool first = true;
    for (int i = 0; i < OPS; ++i) {
        if (!per[i].count) continue;
        std::printf("%s  \"%s\": {\"count\": %" PRIu64, first ? "" : ",\n", NAMES[i], per[i].count);
        first = false;
        const Side *sides[2] = {&per[i].oracle, &per[i].host};
        const char *const side_names[2] = {"oracle", "host"};
        for (int s = 0; s < 2; ++s) {
            std::printf(",\n    \"%s\": {\"inside\": %" PRIu64 ", \"incomparable\": %" PRIu64 ", \"outside_at_most\": {",
                        side_names[s], sides[s]->inside, sides[s]->incomparable);
            for (int n = 0; n < ClassStats::BINS; ++n)
                std::printf("\"%s\": %" PRIu64 "%s", labels[n], sides[s]->at_most[n], n + 1 < ClassStats::BINS ? ", " : "}, ");
            std::printf("\"worst_outside\": %.17g, ", double(sides[s]->worst));
            print_f80("worst_a", sides[s]->worst_a, ", ");
            print_f80("worst_b", sides[s]->worst_b, ", ");
            print_f80("worst_result", sides[s]->worst_r, "}");
        }
        std::printf("}");
    }
    std::printf("\n}, \"oracle_ran_every_program\": %s}\n", oracle.ok ? "true" : "false");
    return oracle.ok ? 0 : 1;
}

int main(int argc, char **argv) {
    if (argc >= 4 && !std::strcmp(argv[1], "--host-dump")) {
        host_dump = stdout;
        // What produced this dump: the CPU, the compiler, the sweep.
        char model[256] = "unknown";
        if (std::FILE *info = std::fopen("/proc/cpuinfo", "r")) {
            char line[512];
            while (std::fgets(line, sizeof line, info))
                if (!std::strncmp(line, "model name", 10)) {
                    const char *colon = std::strchr(line, ':');
                    if (colon) std::snprintf(model, sizeof model, "%s", colon + 2);
                    model[std::strcspn(model, "\n")] = 0;
                    break;
                }
            std::fclose(info);
        }
        std::printf("# host-dump samples=%s seed=%s vectors=%s compiler=\"%s\" cpu=\"%s\"\n", argv[2], argv[3],
                    argc == 5 ? "yes" : "no", __VERSION__, model);
        if (argc == 5 && run_vectors(argv[4])) return 2;
        argv += 1;
    } else {
#ifdef FES_PROBE_HOST_ONLY
        std::fprintf(stderr, "this build has no oracle: only --host-dump\n");
        return 2;
#endif
        if (argc == 3 && !std::strcmp(argv[1], "--vectors")) return run_vectors(argv[2]);
    }
    if (!host_dump && argc != 3) {
        std::fprintf(stderr, "usage: x87_trans_probe <samples> <seed>\n");
        return 2;
    }
    const uint64_t samples = std::strtoull(argv[1], nullptr, 10);
    Rng rng{std::strtoull(argv[2], nullptr, 10) * 0x9e3779b97f4a7c15ull + 1};
    OracleRun oracle;
    if (!host_dump)
        std::printf("{\"samples_per_class\": %" PRIu64 ", \"seed\": %s, \"instructions\": {\n", samples, argv[2]);
    for (int i = 0; i < OPS; ++i) {
        const Op op = Op(i);
        // The operand classes. Each is a generator of (a, b); a one-operand
        // form uses a alone.
        struct Class {
            const char *name;
            int kind;
        };
        std::vector<Class> classes;
        const bool trig = op == FSIN || op == FCOS || op == FSINCOS || op == FPTAN;
        if (trig) classes = {{"|x| in [2^-8, pi/4]", 0}, {"|x| in [pi/4, 2^10]", 1}, {"|x| in [2^10, 2^62]", 2},
                             {"within 2^-20 relative of k*pi/2, k in 1..64", 3}, {"|x| in [2^-70, 2^-8]", 4}};
        else if (op == F2XM1) classes = {{"|x| in [2^-8, 1)", 0}, {"|x| in [2^-70, 2^-8]", 4}};
        else if (op == FYL2X) classes = {{"x in [2^-4, 2^4], y in [2^-4, 2^4]", 0},
                                         {"x within 2^-20 relative of 1, y in [2^-4, 2^4]", 3},
                                         {"x in [2^-1000, 2^1000], y in [2^-4, 2^4]", 2}};
        else if (op == FYL2XP1) classes = {{"|x| in [2^-8, 0.29), y in [2^-4, 2^4]", 0},
                                           {"|x| in [2^-70, 2^-8], y in [2^-4, 2^4]", 4}};
        else classes = {{"|y|, |x| in [2^-4, 2^4]", 0}, {"|y| in [2^-70, 2^-8], |x| in [2^-4, 2^4]", 4},
                        {"|y|, |x| in [2^-1000, 2^1000]", 2}};
        if (!host_dump) std::printf("  \"%s\": [\n", NAMES[op]);
        for (size_t c = 0; c < classes.size(); ++c) {
            ClassStats st;
            st.name = classes[c].name;
            for (uint64_t n = 0; n < samples; ++n) {
                F80 a, b;
                const int kind = classes[c].kind;
                if (trig) {
                    if (kind == 0) {
                        a = rng.value(-8, -1, 0);
                        if ((a.se & 0x7fff) == 16382 && a.sig > HALF_PI_SIG) a.sig = HALF_PI_SIG;  // <= pi/4
                    } else if (kind == 1) a = rng.value(0, 9, 0);
                    else if (kind == 2) a = rng.value(10, 61, 0);
                    else if (kind == 3) {
                        // k * pi/2 with the low 43 bits randomised.
                        const unsigned k = 1 + unsigned(rng.next() % 64);
                        unsigned __int128 m = (unsigned __int128)HALF_PI_SIG * k;
                        int e = 0;
                        while (m >> 64) {
                            m >>= 1;
                            ++e;
                        }
                        uint64_t sig = uint64_t(m);
                        sig = (sig & ~((1ull << 43) - 1)) | (rng.next() & ((1ull << 43) - 1));
                        a = make(rng.next() & 1, e, sig);
                    } else a = rng.value(-70, -9, 0);
                } else if (op == F2XM1) {
                    a = kind == 0 ? rng.value(-8, -1, 0) : rng.value(-70, -9, 0);
                } else if (op == FYL2X) {
                    // a is y (ST(1)), b is x (ST(0)), which must be positive.
                    a = rng.value(-4, 3, 0);
                    if (kind == 0) b = rng.value(-4, 3, 1);
                    else if (kind == 3) {
                        const bool below = rng.next() & 1;
                        const uint64_t low = rng.next() & ((1ull << 43) - 1);
                        b = below ? make(false, -1, ~0ull - low) : make(false, 0, 1ull << 63 | low);
                    } else b = rng.value(-1000, 1000, 1);
                } else if (op == FYL2XP1) {
                    a = rng.value(-4, 3, 0);
                    if (kind == 0) {
                        b = rng.value(-8, -2, 0);
                        if ((b.se & 0x7fff) == 16381 && b.sig > 0x94c583ada5b52800ull) b.sig = 0x94c583ada5b52800ull;
                    } else b = rng.value(-70, -9, 0);
                } else {
                    // FPATAN: a is y (ST(1)), b is x (ST(0)).
                    if (kind == 0) {
                        a = rng.value(-4, 3, 0);
                        b = rng.value(-4, 3, 0);
                    } else if (kind == 4) {
                        a = rng.value(-70, -9, 0);
                        b = rng.value(-4, 3, 0);
                    } else {
                        a = rng.value(-1000, 1000, 0);
                        b = rng.value(-1000, 1000, 0);
                    }
                }
                const Result h = host(op, a, b);
                if (host_dump) {
                    dump_row(op, int(c), a, b, h);
                    continue;
                }
                const Result o = oracle.run(op, a, b);
                ++st.count;
                if (o.r0 == h.r0 && (!two_results(op) || o.r1 == h.r1)) ++st.identical;
                if ((o.sw ^ h.sw) & 0x00ff) ++st.flags_differ;   // the six flags, SF and ES
                if ((o.sw ^ h.sw) & 0x4500) ++st.cc_differ;      // C0, C2, C3
                if ((o.sw ^ h.sw) & 0x0200) ++st.c1_differ;
                // The value each instruction computes: FPTAN's is under the
                // pushed 1.0, FSINCOS's two are both counted.
                if (op == FPTAN) account(st, a, b, o.r1, h.r1);
                else {
                    account(st, a, b, o.r0, h.r0);
                    if (op == FSINCOS) account(st, a, b, o.r1, h.r1);
                }
            }
            if (host_dump) continue;
            std::printf("    {\"class\": \"%s\", \"count\": %" PRIu64 ", \"identical\": %" PRIu64
                        ", \"flags_differ\": %" PRIu64 ", \"c0_c2_c3_differ\": %" PRIu64 ", \"c1_differ\": %" PRIu64
                        ", \"not_a_distance\": %" PRIu64 ",\n     \"distance_at_most\": {",
                        st.name.c_str(), st.count, st.identical, st.flags_differ, st.cc_differ, st.c1_differ,
                        st.unordinary);
            const char *const labels[ClassStats::BINS] = {"0", "1", "2", "4", "16", "256", "65536", "2^32", "any"};
            for (int n = 0; n < ClassStats::BINS; ++n)
                std::printf("\"%s\": %" PRIu64 "%s", labels[n], st.at_most[n], n + 1 < ClassStats::BINS ? ", " : "},\n");
            const double worst = double(st.worst);
            std::printf("     \"worst_distance\": %.17g, ", worst);
            print_f80("worst_a", st.worst_a, ", ");
            print_f80("worst_b", st.worst_b, ", ");
            print_f80("worst_oracle", st.worst_oracle, ", ");
            print_f80("worst_host", st.worst_host, "}");
            std::printf("%s\n", c + 1 < classes.size() ? "," : "");
        }
        if (!host_dump) std::printf("  ]%s\n", i + 1 < OPS ? "," : "");
    }
    if (host_dump) return 0;
    std::printf("}, \"oracle_ran_every_program\": %s}\n", oracle.ok ? "true" : "false");
    return oracle.ok ? 0 : 1;
}
