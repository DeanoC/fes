// SPDX-License-Identifier: MIT
// fes_i386_x87 on its own, against the host's x87 (#1768 slice 2). An
// x86-64 host's FPU is the same 80-bit unit a 387 is, so its own
// instructions are the reference for everything this slice converts: the
// loads, the stores, FXAM, FTST and the constants, with their status words,
// in every rounding mode. The host is a later part than a 387, so where the
// two differ the case is read against the 387 Programmer's Reference Manual
// and not settled by the host; the differences this slice found are in the
// unit's header and in divergences.toml.
//
// The bench drives the unit's CPU interface itself: a load's parts commit
// through `wb`, a store's value comes out of `sval` once `hold` drops, and
// every state change lands at the instruction's commit (`wb_last`).
//
// Output: one "PASS <case>" or "FAIL <case> <what>" line per case, then
// "SUMMARY <passed> <failed>". Exit status 1 on any failure.
// scripts/test_fes_i386_x87_unit.py builds and runs it.
#include "Vfes_i386_x87.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <array>
#include <random>
#include <string>
#include <vector>

namespace {

constexpr unsigned FMT_F32 = 1, FMT_F64 = 2, FMT_F80 = 3;
constexpr unsigned FMT_I16 = 4, FMT_I32 = 5, FMT_I64 = 6, FMT_ENV = 7;

std::string hex(uint64_t v, int digits) {
    char buf[32];
    std::snprintf(buf, sizeof buf, "%0*llx", digits, (unsigned long long)v);
    return buf;
}
std::string hex80(const uint8_t *b) {
    std::string s;
    for (int n = 9; n >= 0; --n) s += hex(b[n], 2);
    return s;
}

// ---- The host reference. ----
// Each helper sets the control word, clears the status word, runs the one
// instruction and reads the status word back, leaving the host's stack as it
// was found. The exception flags stay masked, so every result is the masked
// one, which is what this slice's masked cases compare against.
uint16_t host_cw(unsigned rc) { return uint16_t(0x037f | (rc << 10)); }

struct Ext { uint8_t b[10]; };

#define HOST_OP(load, srctype)                                                        \
    do {                                                                              \
        srctype s;                                                                    \
        std::memcpy(&s, src, sizeof s);                                               \
        asm volatile("fldcw %[cw]\n\tfnclex\n\t" load " %[src]\n\tfnstsw %%ax\n\t"  \
                     "fstpt %[dst]\n\tfnclex"                                         \
                     : "=a"(sw), [dst] "=m"(o)                                        \
                     : [cw] "m"(cw), [src] "m"(s)                                     \
                     : "memory");                                                     \
    } while (0)

// A load of `fmt` from `src`, as the 10 bytes FSTP m80 then stores.
void host_load(const void *src, unsigned fmt, unsigned rc, uint8_t out[10], uint16_t &st) {
    uint16_t cw = host_cw(rc), sw = 0;
    Ext o{};
    switch (fmt) {
        case FMT_F32: HOST_OP("flds", float); break;
        case FMT_F64: HOST_OP("fldl", double); break;
        case FMT_I16: HOST_OP("filds", int16_t); break;
        case FMT_I32: HOST_OP("fildl", int32_t); break;
        default:      HOST_OP("fildll", int64_t); break;
    }
    std::memcpy(out, o.b, 10);
    st = sw;
}

// A store of ST(0) = the 10 bytes at `src`, to `fmt`.
void host_store(const uint8_t src[10], unsigned fmt, unsigned rc, uint8_t out[10], uint16_t &st) {
    Ext s{}, o{};
    std::memcpy(s.b, src, 10);
    uint16_t cw = host_cw(rc), sw = 0;
    std::memset(out, 0, 10);
#define HOST_STORE(store)                                                            \
    asm volatile("fldcw %[cw]\n\tfnclex\n\tfldt %[src]\n\t" store " %[dst]\n\tfnstsw %%ax\n\tfnclex" \
                 : "=a"(sw), [dst] "=m"(o)                                           \
                 : [cw] "m"(cw), [src] "m"(s)                                        \
                 : "memory")
    switch (fmt) {
        case FMT_F32: HOST_STORE("fstps"); break;
        case FMT_F64: HOST_STORE("fstpl"); break;
        case FMT_F80: HOST_STORE("fstpt"); break;
        case FMT_I16: HOST_STORE("fistps"); break;
        case FMT_I32: HOST_STORE("fistpl"); break;
        default:      HOST_STORE("fistpll"); break;
    }
#undef HOST_STORE
    std::memcpy(out, o.b, 10);
    st = sw;
}

// FXAM and FTST of ST(0) = the 10 bytes at `src`: the status word.
uint16_t host_examine(const uint8_t src[10], bool ftst, unsigned rc) {
    Ext s{};
    std::memcpy(s.b, src, 10);
    uint16_t cw = host_cw(rc), sw = 0;
    if (ftst)
        asm volatile("fldcw %[cw]\n\tfnclex\n\tfldt %[src]\n\tftst\n\tfnstsw %%ax\n\tfstp %%st(0)\n\tfnclex"
                     : "=a"(sw) : [cw] "m"(cw), [src] "m"(s) : "memory");
    else
        asm volatile("fldcw %[cw]\n\tfnclex\n\tfldt %[src]\n\tfxam\n\tfnstsw %%ax\n\tfstp %%st(0)\n\tfnclex"
                     : "=a"(sw) : [cw] "m"(cw), [src] "m"(s) : "memory");
    return sw;
}

// The constant instruction `n` (D9 E8+n), as FSTP m80 stores it.
void host_const(unsigned n, unsigned rc, uint8_t out[10]) {
    uint16_t cw = host_cw(rc);
    Ext o{};
    switch (n) {
        case 0: asm volatile("fldcw %[cw]\n\tfld1\n\tfstpt %[dst]" : [dst] "=m"(o) : [cw] "m"(cw) : "memory"); break;
        case 1: asm volatile("fldcw %[cw]\n\tfldl2t\n\tfstpt %[dst]" : [dst] "=m"(o) : [cw] "m"(cw) : "memory"); break;
        case 2: asm volatile("fldcw %[cw]\n\tfldl2e\n\tfstpt %[dst]" : [dst] "=m"(o) : [cw] "m"(cw) : "memory"); break;
        case 3: asm volatile("fldcw %[cw]\n\tfldpi\n\tfstpt %[dst]" : [dst] "=m"(o) : [cw] "m"(cw) : "memory"); break;
        case 4: asm volatile("fldcw %[cw]\n\tfldlg2\n\tfstpt %[dst]" : [dst] "=m"(o) : [cw] "m"(cw) : "memory"); break;
        case 5: asm volatile("fldcw %[cw]\n\tfldln2\n\tfstpt %[dst]" : [dst] "=m"(o) : [cw] "m"(cw) : "memory"); break;
        default: asm volatile("fldcw %[cw]\n\tfldz\n\tfstpt %[dst]" : [dst] "=m"(o) : [cw] "m"(cw) : "memory"); break;
    }
    std::memcpy(out, o.b, 10);
}

// ---- The unit under test, with the CPU's side of its interface. ----
struct Unit {
    Vfes_i386_x87 top;

    Unit() {
        top.clk = 0;
        top.reset = 1;
        top.present = 1;
        top.f0 = top.f1 = 0;
        top.iss = top.iss_wait = top.iss_first = top.iss_last = top.iss_fire = 0;
        top.iss_op = 0;
        top.iss_fmt = 0;
        top.iss_part = 0;
        top.wb = top.wb_last = top.flush = top.wb_opcode_set = 0;
        top.wb_opcode = 0;
        top.wb_val = 0;
        top.wb_part = 0;
        for (int n = 0; n < 4; ++n) tick();
        top.reset = 0;
        tick();
    }
    void tick() {
        top.eval();
        top.clk = 1;
        top.eval();
        top.clk = 0;
        top.eval();
    }
    void idle(int n = 1) {
        for (int i = 0; i < n; ++i) tick();
    }
    // Present one uop in D until its hold drops, then let it issue. Returns
    // the value the CPU would store for it.
    uint32_t present(unsigned op, unsigned fmt, bool first, bool last, unsigned part) {
        top.iss = 1;
        top.iss_wait = 0;
        top.iss_op = op;
        top.iss_fmt = fmt;
        top.iss_first = first;
        top.iss_last = last;
        top.iss_part = part;
        top.iss_fire = 0;
        top.eval();
        for (int n = 0; n < 64 && top.hold; ++n) tick();
        const uint32_t v = top.sval;
        top.iss_fire = 1;   // the uop issues on this edge, as the CPU's does
        top.eval();
        tick();
        top.iss = 0;
        top.iss_fire = 0;
        top.eval();
        return v;
    }
    void commit(unsigned part, uint32_t val, bool last) {
        top.wb = 1;
        top.wb_part = part;
        top.wb_val = val;
        top.wb_last = last;
        tick();
        top.wb = 0;
        top.wb_last = 0;
        top.eval();
    }
    // A write to port F0h, which clears the board's BUSY# latch.
    void clear_latch() {
        top.f0 = 1;
        tick();
        top.f0 = 0;
        tick();
    }
    // Two ESC instructions back to back, with no settling between them: the
    // unit must hold the second until it has finished the first.
    void back_to_back(unsigned op_a, unsigned op_b) {
        present(op_a, 0, true, true, 0);
        commit(0, 0, true);
        present(op_b, 0, true, true, 0);
        commit(0, 0, true);
        settle();
    }
    // Unmask the invalid-operation exception and raise one, which sets ES
    // and latches BUSY#.
    void settle() {
        for (int n = 0; n < 64 && top.busy_o; ++n) tick();
    }
    // One instruction with no memory operand.
    void reg_form(unsigned op) {
        present(op, 0, true, true, 0);
        commit(0, 0, true);
        settle();
    }
    // A load of `fmt` from `bytes`: the parts commit high first, as the
    // CPU's microcode loops read them.
    void load(unsigned op, unsigned fmt, const uint8_t *bytes) {
        const int parts = fmt == FMT_F80 ? 3 : (fmt == FMT_F64 || fmt == FMT_I64) ? 2 : 1;
        for (int p = parts - 1; p >= 0; --p) {
            present(op, fmt, p == parts - 1, p == 0, unsigned(p));
            uint32_t v = 0;
            std::memcpy(&v, bytes + 4 * p, p == 2 ? 2 : 4);
            commit(unsigned(p), v, p == 0);
        }
        settle();
    }
    // A store of `fmt`: the bytes the CPU would write.
    void store(unsigned op, unsigned fmt, uint8_t out[10]) {
        const int parts = fmt == FMT_F80 ? 3 : (fmt == FMT_F64 || fmt == FMT_I64) ? 2 : 1;
        std::memset(out, 0, 10);
        for (int p = 0; p < parts; ++p) {
            const uint32_t v = present(op, fmt, p == 0, p == parts - 1, unsigned(p));
            std::memcpy(out + 4 * p, &v, p == 2 ? 2 : (fmt == FMT_I16 ? 2 : 4));
            commit(unsigned(p), 0, p == parts - 1);
        }
        settle();
    }
    bool access_made() const { return !top.nx; }
    uint16_t sw() const { return uint16_t(top.sw_o); }
    uint16_t cw() const { return uint16_t(top.cw_o); }
    uint16_t tw() const { return uint16_t(top.tw_o); }
    void reg(unsigned i, uint8_t out[10]) const {
        // regs_o is eight 80-bit registers, little-endian in 32-bit words.
        for (int n = 0; n < 10; ++n) {
            const unsigned bit = 80 * i + 8 * unsigned(n);
            const unsigned w = bit / 32, s = bit % 32;
            uint64_t v = top.regs_o[w];
            if (s > 24) v |= uint64_t(top.regs_o[w + 1]) << 32;
            out[n] = uint8_t(v >> s);
        }
    }
    void st0(uint8_t out[10]) const { reg(unsigned((top.sw_o >> 11) & 7), out); }
    unsigned top_of() const { return unsigned((top.sw_o >> 11) & 7); }
    // ST(i), which is the physical register (TOP + i) & 7.
    void sti(unsigned i, uint8_t out[10]) const { reg((top_of() + i) & 7, out); }

    // FNSTENV or FNSAVE: the environment's first three fields (the CPU
    // builds fields 3 to 6, the x87 pointers) and, for FNSAVE, each
    // register's two dwords and its top word, which is how the CPU's
    // microcode moves the image.
    void save(bool image, uint16_t &cw_out, uint16_t &sw_out, uint16_t &tw_out, uint8_t regs[8][10]) {
        const unsigned op = image ? (5u << 8) | 0x35 : (1u << 8) | 0x35;  // DD /6, D9 /6
        const unsigned fields = 7, chunks = image ? 24u : 0u;
        for (unsigned f = 0; f < fields; ++f) {
            const uint32_t v = present(op, FMT_ENV, f == 0, !image && f == fields - 1, f);
            if (f == 0) cw_out = uint16_t(v);
            if (f == 1) sw_out = uint16_t(v);
            if (f == 2) tw_out = uint16_t(v);
            commit(f, 0, !image && f == fields - 1);
        }
        for (unsigned c = 0; c < chunks; ++c) {
            const unsigned part = 0x20u | (c / 3) << 2 | (c % 3);
            const uint32_t v = present(op, FMT_ENV, false, c == chunks - 1, part);
            std::memcpy(regs[c / 3] + 4 * (c % 3), &v, (c % 3) == 2 ? 2 : 4);
            commit(part, 0, c == chunks - 1);
        }
        settle();
    }
    // FLDENV or FRSTOR, from the words and image a save left.
    void restore(bool image, uint16_t cw_in, uint16_t sw_in, uint16_t tw_in, const uint8_t regs[8][10]) {
        const unsigned op = image ? (5u << 8) | 0x25 : (1u << 8) | 0x25;  // DD /4, D9 /4
        const unsigned fields = 7, chunks = image ? 24u : 0u;
        for (unsigned f = 0; f < fields; ++f) {
            present(op, FMT_ENV, f == 0, !image && f == fields - 1, f);
            const uint32_t v = f == 0 ? cw_in : f == 1 ? sw_in : f == 2 ? tw_in : 0;
            commit(f, v, !image && f == fields - 1);
        }
        for (unsigned c = 0; c < chunks; ++c) {
            const unsigned part = 0x20u | (c / 3) << 2 | (c % 3);
            present(op, FMT_ENV, false, c == chunks - 1, part);
            uint32_t v = 0;
            std::memcpy(&v, regs[c / 3] + 4 * (c % 3), (c % 3) == 2 ? 2 : 4);
            commit(part, v, c == chunks - 1);
        }
        settle();
    }
};

// The opcodes this bench issues: {ESC's low three bits, the ModRM byte}.
constexpr unsigned OP_FLD_M32 = (1u << 8) | 0x05;   // D9 /0
constexpr unsigned OP_FLD_M64 = (5u << 8) | 0x05;   // DD /0
constexpr unsigned OP_FLD_M80 = (3u << 8) | 0x2d;   // DB /5
constexpr unsigned OP_FILD_M16 = (7u << 8) | 0x05;  // DF /0
constexpr unsigned OP_FILD_M32 = (3u << 8) | 0x05;  // DB /0
constexpr unsigned OP_FILD_M64 = (7u << 8) | 0x2d;  // DF /5
constexpr unsigned OP_FSTP_M32 = (1u << 8) | 0x1d;  // D9 /3
constexpr unsigned OP_FSTP_M64 = (5u << 8) | 0x1d;  // DD /3
constexpr unsigned OP_FSTP_M80 = (3u << 8) | 0x3d;  // DB /7
constexpr unsigned OP_FISTP_M16 = (7u << 8) | 0x1d; // DF /3
constexpr unsigned OP_FISTP_M32 = (3u << 8) | 0x1d; // DB /3
constexpr unsigned OP_FISTP_M64 = (7u << 8) | 0x3d; // DF /7
constexpr unsigned OP_FLDCW = (1u << 8) | 0x2d;     // D9 /5
constexpr unsigned OP_FNINIT = (3u << 8) | 0xe3;
constexpr unsigned OP_FNCLEX = (3u << 8) | 0xe2;
constexpr unsigned OP_FXAM = (1u << 8) | 0xe5;
constexpr unsigned OP_FTST = (1u << 8) | 0xe4;
constexpr unsigned OP_FCHS = (1u << 8) | 0xe0;
constexpr unsigned OP_FABS = (1u << 8) | 0xe1;
constexpr unsigned OP_FINCSTP = (1u << 8) | 0xf7;
constexpr unsigned OP_FDECSTP = (1u << 8) | 0xf6;
unsigned op_const(unsigned n) { return (1u << 8) | (0xe8 + n); }
unsigned op_fxch(unsigned i) { return (1u << 8) | (0xc8 + i); }
unsigned op_fld_st(unsigned i) { return (1u << 8) | (0xc0 + i); }
unsigned op_ffree(unsigned i) { return (5u << 8) | (0xc0 + i); }

int passed = 0, failed = 0;
void ok(const std::string &name) {
    std::printf("PASS %s\n", name.c_str());
    ++passed;
}
void bad(const std::string &name, const std::string &what) {
    std::printf("FAIL %s %s\n", name.c_str(), what.c_str());
    ++failed;
}
// The status-word bits a load or a store defines: the six exception flags,
// the stack flag, ES, and C1, which says a rounding went up or which way a
// stack fault went. C0, C2 and C3 are undefined for these forms, so the
// host's are no reference.
uint16_t flags_of(uint16_t sw) { return uint16_t(sw & 0x02ff); }

// Set the unit's rounding mode with FLDCW.
void set_rc(Unit &u, unsigned rc) {
    const uint16_t cw = host_cw(rc);
    u.present(OP_FLDCW, FMT_F32, true, true, 0);
    u.commit(0, cw, true);
    u.settle();
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);

    // The patterns every conversion runs: the classes the 387 PRM names,
    // then random bits.
    const uint32_t f32s[] = {0x00000000, 0x80000000, 0x3f800000, 0xbf800000, 0x7f800000,
                             0xff800000, 0x7fc00000, 0x7f800001, 0xffbfffff, 0x00000001,
                             0x007fffff, 0x80000001, 0x00400000, 0x7f7fffff, 0x33333333};
    const uint64_t f64s[] = {0x0000000000000000ull, 0x8000000000000000ull, 0x3ff0000000000000ull,
                             0xbff0000000000000ull, 0x7ff0000000000000ull, 0xfff0000000000000ull,
                             0x7ff8000000000000ull, 0x7ff0000000000001ull, 0x0000000000000001ull,
                             0x000fffffffffffffull, 0x0008000000000000ull, 0x7fefffffffffffffull,
                             0x4005bf0a8b145769ull};
    const int64_t ints[] = {0, 1, -1, 2, -2, 32767, -32768, 0x7fffffff, -0x7fffffff - 1,
                            0x7fffffffffffffffll, -0x7fffffffffffffffll - 1, 12345, -98765,
                            0x0000000100000000ll, 0x5555555555555555ll};

    std::mt19937_64 rng(20261114);

    // ---- Loads. ----
    for (unsigned rc = 0; rc < 4; ++rc) {
        for (unsigned which = 0; which < 5; ++which) {
            const unsigned fmt = which == 0 ? FMT_F32 : which == 1 ? FMT_F64
                               : which == 2 ? FMT_I16 : which == 3 ? FMT_I32 : FMT_I64;
            const unsigned op = which == 0 ? OP_FLD_M32 : which == 1 ? OP_FLD_M64
                              : which == 2 ? OP_FILD_M16 : which == 3 ? OP_FILD_M32 : OP_FILD_M64;
            const char *tag = which == 0 ? "f32" : which == 1 ? "f64"
                            : which == 2 ? "i16" : which == 3 ? "i32" : "i64";
            std::vector<uint64_t> values;
            if (which == 0) for (uint32_t v : f32s) values.push_back(v);
            else if (which == 1) for (uint64_t v : f64s) values.push_back(v);
            else for (int64_t v : ints) values.push_back(uint64_t(v));
            for (int n = 0; n < 24; ++n) values.push_back(rng());
            int diffs = 0;
            std::string first;
            Unit u;
            for (uint64_t raw : values) {
                uint8_t bytes[10] = {0};
                std::memcpy(bytes, &raw, 8);
                u.reg_form(OP_FNINIT);
                set_rc(u, rc);
                u.load(op, fmt, bytes);
                uint8_t got[10], want[10];
                uint16_t want_sw = 0;
                u.st0(got);
                host_load(bytes, fmt, rc, want, want_sw);
                const uint16_t got_fl = flags_of(u.sw());
                if (std::memcmp(got, want, 10) != 0 || got_fl != flags_of(want_sw)) {
                    if (!diffs)
                        first = "value=" + hex(raw, 16) + " got=" + hex80(got) + "/" + hex(got_fl, 4) +
                                " want=" + hex80(want) + "/" + hex(flags_of(want_sw), 4);
                    ++diffs;
                }
            }
            const std::string name = std::string("load-") + tag + "-rc" + std::to_string(rc);
            if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(values.size()) + " " + first);
            else ok(name);
        }
    }

    // ---- Stores. ----
    // Each case loads a 10-byte pattern with FLD m80, which is a bit copy,
    // then stores it to the format under test.
    std::vector<std::array<uint8_t, 10>> ext;
    {
        auto add = [&](uint16_t se, uint64_t sig) {
            std::array<uint8_t, 10> b{};
            std::memcpy(b.data(), &sig, 8);
            std::memcpy(b.data() + 8, &se, 2);
            ext.push_back(b);
        };
        add(0x0000, 0);                       // +0
        add(0x8000, 0);                       // -0
        add(0x3fff, 0x8000000000000000ull);   // 1.0
        add(0xbfff, 0x8000000000000000ull);   // -1.0
        add(0x7fff, 0x8000000000000000ull);   // +inf
        add(0xffff, 0x8000000000000000ull);   // -inf
        add(0x7fff, 0xc000000000000000ull);   // QNaN
        add(0x7fff, 0xa000000000000000ull);   // SNaN
        add(0x7fff, 0x4000000000000000ull);   // pseudo-NaN (unsupported)
        add(0x0000, 0x8000000000000000ull);   // pseudo-denormal
        add(0x0000, 0x0000000000000001ull);   // the smallest denormal
        add(0x4000, 0x4000000000000000ull);   // an unnormal (unsupported)
        add(0x3ffe, 0x8000000000000000ull);   // 0.5
        add(0x4005, 0xfffffffffffffffeull);   // just under 128
        add(0x407e, 0xffffff0000000000ull);   // around the f32 maximum
        add(0x4080, 0x8000000000000000ull);   // past the f32 maximum
        add(0x3f6a, 0x8000000000000000ull);   // under the f32 denormals
        add(0x43fe, 0xffffffffffffffffull);   // past the f64 maximum
        add(0x403e, 0xffffffffffffffffull);   // just under 2^63
        add(0x403e, 0x8000000000000000ull);   // 2^63
        add(0xc03e, 0x8000000000000000ull);   // -2^63
        add(0x400d, 0xffffc00000000000ull);   // around the i16 maximum
        for (int n = 0; n < 40; ++n) {
            std::array<uint8_t, 10> b{};
            uint64_t sig = rng() | 0x8000000000000000ull;
            uint16_t se = uint16_t((rng() % 0x8000) | ((rng() & 1) << 15));
            if ((se & 0x7fff) == 0 || (se & 0x7fff) == 0x7fff) se = uint16_t((se & 0x8000) | 0x4000);
            std::memcpy(b.data(), &sig, 8);
            std::memcpy(b.data() + 8, &se, 2);
            ext.push_back(b);
        }
    }
    for (unsigned rc = 0; rc < 4; ++rc) {
        for (unsigned which = 0; which < 6; ++which) {
            const unsigned fmt = which == 0 ? FMT_F32 : which == 1 ? FMT_F64 : which == 2 ? FMT_F80
                               : which == 3 ? FMT_I16 : which == 4 ? FMT_I32 : FMT_I64;
            const unsigned op = which == 0 ? OP_FSTP_M32 : which == 1 ? OP_FSTP_M64 : which == 2 ? OP_FSTP_M80
                              : which == 3 ? OP_FISTP_M16 : which == 4 ? OP_FISTP_M32 : OP_FISTP_M64;
            const int width = which == 0 ? 4 : which == 1 ? 8 : which == 2 ? 10 : which == 3 ? 2 : which == 4 ? 4 : 8;
            const char *tag = which == 0 ? "f32" : which == 1 ? "f64" : which == 2 ? "m80"
                            : which == 3 ? "i16" : which == 4 ? "i32" : "i64";
            int diffs = 0;
            std::string first;
            Unit u;
            for (const auto &b : ext) {
                u.reg_form(OP_FNINIT);
                set_rc(u, rc);
                u.load(OP_FLD_M80, FMT_F80, b.data());
                uint8_t got[10], want[10];
                uint16_t want_sw = 0;
                u.store(op, fmt, got);
                host_store(b.data(), fmt, rc, want, want_sw);
                const uint16_t got_fl = flags_of(u.sw());
                if (std::memcmp(got, want, size_t(width)) != 0 || got_fl != flags_of(want_sw)) {
                    if (!diffs)
                        first = "st0=" + hex80(b.data()) + " got=" + hex80(got) + "/" + hex(got_fl, 4) +
                                " want=" + hex80(want) + "/" + hex(flags_of(want_sw), 4);
                    ++diffs;
                }
            }
            const std::string name = std::string("store-") + tag + "-rc" + std::to_string(rc);
            if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(ext.size()) + " " + first);
            else ok(name);
        }
    }

    // ---- FXAM and FTST. ----
    for (int ftst = 0; ftst < 2; ++ftst) {
        int diffs = 0;
        std::string first;
        Unit u;
        for (const auto &b : ext) {
            u.reg_form(OP_FNINIT);
            u.load(OP_FLD_M80, FMT_F80, b.data());
            u.reg_form(ftst ? OP_FTST : OP_FXAM);
            const uint16_t want = host_examine(b.data(), ftst != 0, 0);
            // The condition codes and, for FTST, the exception flags.
            const uint16_t mask = ftst ? uint16_t(0x47ff) : uint16_t(0x4700);
            if ((u.sw() & mask) != (want & mask)) {
                if (!diffs)
                    first = "st0=" + hex80(b.data()) + " got=" + hex(u.sw() & mask, 4) +
                            " want=" + hex(want & mask, 4);
                ++diffs;
            }
        }
        if (diffs) bad(ftst ? "ftst" : "fxam", std::to_string(diffs) + " of " +
                       std::to_string(ext.size()) + " " + first);
        else ok(ftst ? "ftst" : "fxam");
    }

    // ---- The constants. ----
    for (unsigned rc = 0; rc < 4; ++rc) {
        int diffs = 0;
        std::string first;
        Unit u;
        for (unsigned n = 0; n < 7; ++n) {
            u.reg_form(OP_FNINIT);
            set_rc(u, rc);
            u.reg_form(op_const(n));
            uint8_t got[10], want[10] = {0};
            u.st0(got);
            host_const(n, rc, want);
            if (std::memcmp(got, want, 10) != 0) {
                if (!diffs)
                    first = "n=" + std::to_string(n) + " got=" + hex80(got) + " want=" + hex80(want);
                ++diffs;
            }
        }
        const std::string name = "const-rc" + std::to_string(rc);
        if (diffs) bad(name, std::to_string(diffs) + " of 7 " + first);
        else ok(name);
    }

    // ---- The state forms. ----
    {
        Unit u;
        u.reg_form(OP_FNINIT);
        if (u.cw() != 0x037f || u.sw() != 0 || u.tw() != 0xffff) bad("fninit", "cw/sw/tw");
        else ok("fninit");

        // The F1h reset: control 037Eh, status 8081h, every tag empty.
        u.top.f1 = 1;
        u.tick();
        u.top.f1 = 0;
        u.tick();
        if (u.cw() != 0x037e || u.sw() != 0x8081 || u.tw() != 0xffff) bad("f1-reset", "cw=" + hex(u.cw(), 4) +
                                                                          " sw=" + hex(u.sw(), 4));
        else if (!u.top.error) bad("f1-reset", "ERROR# not active");
        else if (u.top.latched) bad("f1-reset", "the latch was set");
        else ok("f1-reset");
        // An unmasked exception sets ES, which latches BUSY#: the next ESC
        // then holds until a write to F0h, and FNCLEX and FNINIT do not.
        u.reg_form(OP_FNINIT);
        u.present(OP_FLDCW, FMT_F32, true, true, 0);
        u.commit(0, 0x037e, true);
        u.settle();
        u.reg_form(OP_FCHS);  // an empty ST(0): an unmasked stack fault
        u.idle(2);
        u.top.iss = 1;
        u.top.iss_wait = 0;
        u.top.iss_op = OP_FLD_M32;
        u.top.iss_fmt = FMT_F32;
        u.top.iss_first = 1;
        u.top.eval();
        const bool held = u.top.hold;
        u.top.iss_op = OP_FNCLEX;
        u.top.iss_fmt = 0;
        u.top.eval();
        const bool clex_free = !u.top.hold;
        u.top.iss = u.top.iss_first = 0;
        u.top.eval();
        if (!held) bad("busy-latch", "an ESC did not hold");
        else if (!clex_free) bad("busy-latch", "FNCLEX held");
        else {
            if (!u.top.latched) bad("busy-latch", "the latch is clear");
            else {
                u.top.f0 = 1;
                u.tick();
                u.top.f0 = 0;
                u.tick();
                if (u.top.latched) bad("busy-latch", "F0h left the latch set");
                else ok("busy-latch");
            }
        }
        u.clear_latch();
        u.reg_form(OP_FNINIT);

        // FINCSTP and FDECSTP move TOP and leave the tags.
        u.reg_form(OP_FDECSTP);
        const bool dec = ((u.sw() >> 11) & 7) == 7 && u.tw() == 0xffff;
        u.reg_form(OP_FINCSTP);
        const bool inc = ((u.sw() >> 11) & 7) == 0;
        if (dec && inc) ok("incstp-decstp");
        else bad("incstp-decstp", "top=" + hex((u.sw() >> 11) & 7, 1));

        // FCHS and FABS of 1.0, and the stack underflow of an empty ST(0).
        uint8_t one[10] = {0, 0, 0, 0, 0, 0, 0, 0x80, 0xff, 0x3f};
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, one);
        u.reg_form(OP_FCHS);
        uint8_t got[10];
        u.st0(got);
        const bool chs = got[9] == 0xbf;
        u.reg_form(OP_FABS);
        u.st0(got);
        const bool fabs_ok = got[9] == 0x3f;
        if (chs && fabs_ok) ok("fchs-fabs");
        else bad("fchs-fabs", hex80(got));

        u.reg_form(OP_FNINIT);
        u.reg_form(OP_FCHS);  // ST(0) is empty
        if ((u.sw() & 0x41) == 0x41 && (u.sw() & 0x200) == 0) ok("stack-underflow");
        else bad("stack-underflow", "sw=" + hex(u.sw(), 4));

        // A stack overflow: eight pushes fill the stack, and the ninth
        // raises invalid with the stack flag and C1 set.
        u.reg_form(OP_FNINIT);
        for (int n = 0; n < 8; ++n) u.load(OP_FLD_M80, FMT_F80, one);
        const uint16_t before = u.sw();
        u.load(OP_FLD_M80, FMT_F80, one);
        if ((before & 0x41) == 0 && (u.sw() & 0x241) == 0x241) ok("stack-overflow");
        else bad("stack-overflow", "before=" + hex(before, 4) + " after=" + hex(u.sw(), 4));

        // FXCH swaps ST(0) and ST(1); FFREE empties a register.
        uint8_t two[10] = {0, 0, 0, 0, 0, 0, 0, 0x80, 0x00, 0x40};
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, one);
        u.load(OP_FLD_M80, FMT_F80, two);
        u.reg_form(op_fxch(1));
        uint8_t a[10], b2[10];
        u.st0(a);
        u.reg(unsigned((u.sw() >> 11) & 7) + 1 == 8 ? 0 : unsigned((u.sw() >> 11) & 7) + 1, b2);
        if (std::memcmp(a, one, 10) == 0 && std::memcmp(b2, two, 10) == 0) ok("fxch");
        else bad("fxch", "st0=" + hex80(a) + " st1=" + hex80(b2));

        u.reg_form(op_ffree(0));
        if ((u.tw() >> (2 * ((u.sw() >> 11) & 7))) == 3 || (u.tw() & (3u << (2 * ((u.sw() >> 11) & 7)))) ==
            (3u << (2 * ((u.sw() >> 11) & 7)))) ok("ffree");
        else bad("ffree", "tw=" + hex(u.tw(), 4));

        // FLD ST(i) copies, and an empty source is a stack underflow.
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, two);
        u.reg_form(op_fld_st(0));
        u.st0(a);
        if (std::memcmp(a, two, 10) == 0 && (u.sw() & 0x41) == 0) ok("fld-st");
        else bad("fld-st", hex80(a) + " sw=" + hex(u.sw(), 4));

        // FLDCW takes the word, and FNCLEX clears the flags and ES.
        u.reg_form(OP_FNINIT);
        u.present(OP_FLDCW, FMT_F32, true, true, 0);
        u.commit(0, 0x0b72, true);
        u.settle();
        const bool ldcw = u.cw() == 0x0b72;
        u.reg_form(OP_FNINIT);
        u.reg_form(OP_FCHS);  // sets IE and SF
        const bool dirty = (u.sw() & 0x41) == 0x41;
        u.reg_form(OP_FNCLEX);
        if (ldcw && dirty && (u.sw() & 0x80ff) == 0) ok("ldcw-fnclex");
        else bad("ldcw-fnclex", "cw=" + hex(u.cw(), 4) + " sw=" + hex(u.sw(), 4));

        // An unmasked exception sets ES, which raises ERROR#, and leaves the
        // destination alone: the store makes no access.
        u.reg_form(OP_FNINIT);
        u.top.f0 = 1;
        u.tick();
        u.top.f0 = 0;
        u.tick();
        u.present(OP_FLDCW, FMT_F32, true, true, 0);
        u.commit(0, 0x037e, true);  // invalid unmasked
        u.settle();
        uint8_t out[10];
        u.store(OP_FISTP_M16, FMT_I16, out);  // ST(0) is empty: a stack fault
        u.idle(2);                            // ERROR# latches BUSY# a cycle later
        if (u.top.error && (u.sw() & 0x80) && u.top.latched) ok("unmasked-error");
        else bad("unmasked-error", "sw=" + hex(u.sw(), 4));
    }

    // ---- The environment and the save image. ----
    // The layouts the CPU's microcode moves: FNSTENV's and FLDENV's three
    // words, and FNSAVE's and FRSTOR's image, which holds ST(0) to ST(7)
    // (387 PRM, the FSAVE image) and so follows TOP.
    {
        Unit u;
        u.clear_latch();
        uint8_t one[10] = {0, 0, 0, 0, 0, 0, 0, 0x80, 0xff, 0x3f};
        uint8_t two[10] = {0, 0, 0, 0, 0, 0, 0, 0x80, 0x00, 0x40};
        uint8_t four[10] = {0, 0, 0, 0, 0, 0, 0, 0x80, 0x01, 0x40};

        // Three pushes, so TOP is 5 and the image's ST(0) is physical 5:
        // the round trip must bring the same stack back.
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, one);
        u.load(OP_FLD_M80, FMT_F80, two);
        u.load(OP_FLD_M80, FMT_F80, four);
        uint16_t cw0 = 0, sw0 = 0, tw0 = 0;
        uint8_t image[8][10] = {{0}};
        u.save(true, cw0, sw0, tw0, image);
        // FNSAVE leaves the unit as FNINIT does.
        if (u.cw() != 0x037f || u.sw() != 0 || u.tw() != 0xffff)
            bad("fnsave-reinit", "cw=" + hex(u.cw(), 4) + " sw=" + hex(u.sw(), 4) + " tw=" + hex(u.tw(), 4));
        else if (sw0 != 0x2800 || tw0 != 0x03ff || cw0 != 0x037f)
            bad("fnsave-words", "cw=" + hex(cw0, 4) + " sw=" + hex(sw0, 4) + " tw=" + hex(tw0, 4));
        else if (std::memcmp(image[0], four, 10) || std::memcmp(image[1], two, 10) ||
                 std::memcmp(image[2], one, 10))
            bad("fnsave-image", "st0=" + hex80(image[0]) + " st1=" + hex80(image[1]) +
                                " st2=" + hex80(image[2]));
        else ok("fnsave");

        u.restore(true, cw0, sw0, tw0, image);
        uint8_t got0[10], got1[10], got2[10];
        u.sti(0, got0);
        u.sti(1, got1);
        u.sti(2, got2);
        if (u.cw() != cw0 || u.sw() != sw0 || u.tw() != tw0)
            bad("frstor-words", "cw=" + hex(u.cw(), 4) + " sw=" + hex(u.sw(), 4) + " tw=" + hex(u.tw(), 4));
        else if (std::memcmp(got0, four, 10) || std::memcmp(got1, two, 10) || std::memcmp(got2, one, 10))
            bad("frstor-image", "st0=" + hex80(got0) + " st1=" + hex80(got1) + " st2=" + hex80(got2));
        else ok("frstor");

        // FNSTENV stores the same three words and then masks every
        // exception; FLDENV takes them back and leaves the registers.
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, one);
        u.reg_form(OP_FCHS);
        uint16_t cw1 = 0, sw1 = 0, tw1 = 0;
        uint8_t none[8][10] = {{0}};
        u.save(false, cw1, sw1, tw1, none);
        const bool masked = (u.cw() & 0x3f) == 0x3f;
        uint8_t kept[10], minus_one[10] = {0, 0, 0, 0, 0, 0, 0, 0x80, 0xff, 0xbf};
        u.sti(0, kept);
        if (!masked) bad("fnstenv-masks", "cw=" + hex(u.cw(), 4));
        else if (std::memcmp(kept, minus_one, 10) != 0) bad("fnstenv-keeps", hex80(kept));
        else if (sw1 != 0x3800 || cw1 != 0x037f || tw1 != 0x3fff)
            bad("fnstenv-words", "cw=" + hex(cw1, 4) + " sw=" + hex(sw1, 4) + " tw=" + hex(tw1, 4));
        else ok("fnstenv");

        u.reg_form(OP_FNINIT);
        u.restore(false, 0x0b7f, 0x3800, 0xf3ff, none);
        if (u.cw() == 0x0b7f && u.sw() == 0x3800 && ((u.tw() >> 10) & 3) != 3 && ((u.tw() >> 12) & 3) == 3)
            ok("fldenv");
        else bad("fldenv", "cw=" + hex(u.cw(), 4) + " sw=" + hex(u.sw(), 4) + " tw=" + hex(u.tw(), 4));
    }

    // ---- The protocol. ----
    {
        Unit u;
        u.clear_latch();
        // FNINIT and FNCLEX run while the board's latch holds BUSY#, which
        // is slice 0's no-wait case: FNCLEX drops IRQ 13 and FNINIT resets.
        u.reg_form(OP_FNINIT);
        u.top.f1 = 1;
        u.tick();
        u.top.f1 = 0;
        u.tick();
        const bool latch_clear_after_f1 = !u.top.latched;
        // ERROR# is active, so the next waiting ESC latches nothing by
        // itself: the latch is the board's, set when ERROR# goes active.
        u.reg_form(OP_FNCLEX);
        if (!latch_clear_after_f1) bad("f1-keeps-latch", "F1h set the latch");
        else if (u.sw() != 0x037e - 0x037e) {  // FNCLEX left the flags and ES clear
            if ((u.sw() & 0x80ff) != 0) bad("nowait-fnclex", "sw=" + hex(u.sw(), 4));
            else ok("nowait-fnclex");
        } else ok("nowait-fnclex");
        if (u.top.error) bad("nowait-fnclex-error", "ERROR# still active");
        else ok("nowait-fnclex-error");

        // FNINIT runs under the latch too, and resets the unit.
        u.reg_form(OP_FNINIT);
        u.present(OP_FLDCW, FMT_F32, true, true, 0);
        u.commit(0, 0x037e, true);   // invalid unmasked
        u.settle();
        u.reg_form(OP_FCHS);         // an empty ST(0): an unmasked stack fault
        u.idle(1);
        const bool latched_now = u.top.latched;
        u.reg_form(OP_FNINIT);
        if (!latched_now) bad("nowait-fninit", "the latch did not set");
        else if (u.cw() != 0x037f || u.sw() != 0) bad("nowait-fninit", "cw=" + hex(u.cw(), 4) +
                                                      " sw=" + hex(u.sw(), 4));
        else ok("nowait-fninit");
        u.top.f0 = 1;
        u.tick();
        u.top.f0 = 0;
        u.tick();

        // The latch sets in the same cycle as ES, so an ESC waiting in D
        // cannot slip past it: with the unmasked exception's commit, the
        // next cycle's hold is already up.
        u.reg_form(OP_FNINIT);
        u.present(OP_FLDCW, FMT_F32, true, true, 0);
        u.commit(0, 0x037e, true);
        u.settle();
        u.present(OP_FCHS, 0, true, true, 0);
        u.commit(0, 0, true);        // the apply sets ES and the latch
        u.top.iss = 1;
        u.top.iss_op = OP_FCHS;
        u.top.iss_fmt = 0;
        u.top.iss_first = 1;
        u.top.eval();
        if (!u.top.hold) bad("latch-no-gap", "an ESC was free the cycle after ES");
        else ok("latch-no-gap");
        u.top.iss = u.top.iss_first = 0;
        u.top.eval();
        u.settle();
        u.reg_form(OP_FNINIT);   // clears ES and masks every exception again
        u.clear_latch();

        // A squashed store leaves the unit idle and the stack as it was; a
        // load that has retired finishes, as a 387 does with BUSY# active.
        u.clear_latch();
        uint8_t one[10] = {0, 0, 0, 0, 0, 0, 0, 0x80, 0xff, 0x3f};
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, one);
        const uint16_t top_before = uint16_t((u.sw() >> 11) & 7);
        u.present(OP_FSTP_M32, FMT_F32, true, true, 0);
        u.top.flush = 1;
        u.tick();
        u.top.flush = 0;
        u.tick();
        u.settle();
        if (u.top.busy_o) bad("flush-store", "the unit stayed busy");
        else if (((u.sw() >> 11) & 7) != top_before) bad("flush-store", "the stack moved");
        else ok("flush-store");
        // The unit takes the next instruction after the squash.
        u.reg_form(OP_FCHS);
        uint8_t got[10];
        u.st0(got);
        if (got[9] == 0xbf) ok("flush-store-next");
        else bad("flush-store-next", hex80(got));

        // A flush after the load's commit must not lose it.
        u.reg_form(OP_FNINIT);
        u.present(OP_FLD_M80, FMT_F80, true, false, 2);
        u.commit(2, 0x3fff, false);
        u.present(OP_FLD_M80, FMT_F80, false, false, 1);
        u.commit(1, 0x80000000u, false);
        u.present(OP_FLD_M80, FMT_F80, false, true, 0);
        u.commit(0, 0, true);
        u.top.flush = 1;
        u.tick();
        u.top.flush = 0;
        u.settle();
        u.st0(got);
        if (std::memcmp(got, one, 10) == 0) ok("flush-after-commit");
        else bad("flush-after-commit", hex80(got));

        // Two pushes back to back, with no settling between them: the
        // second must wait for the first, which the unit finishes after the
        // instruction has retired.
        u.clear_latch();
        u.reg_form(OP_FNINIT);
        u.back_to_back(op_const(0), op_const(6));   // FLD1 then FLDZ
        uint8_t b2b0[10], b2b1[10];
        u.sti(0, b2b0);
        u.sti(1, b2b1);
        const uint8_t want1[10] = {0, 0, 0, 0, 0, 0, 0, 0x80, 0xff, 0x3f};
        if (((u.sw() >> 11) & 7) != 6) bad("back-to-back", "top=" + hex((u.sw() >> 11) & 7, 1));
        else if (b2b0[9] != 0 || b2b0[8] != 0 || std::memcmp(b2b1, want1, 10) != 0)
            bad("back-to-back", "st0=" + hex80(b2b0) + " st1=" + hex80(b2b1));
        else ok("back-to-back");

        // A control form leaves C1 and the other condition codes alone.
        u.clear_latch();
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, one);
        u.reg_form(OP_FXAM);
        const uint16_t cc_after_fxam = uint16_t(u.sw() & 0x4700);
        u.present(OP_FLDCW, FMT_F32, true, true, 0);
        u.commit(0, 0x037f, true);
        u.settle();
        uint8_t out2[10];
        u.store((1u << 8) | 0x3d, FMT_F32, out2);  // FNSTCW (D9 /7)
        if ((u.sw() & 0x4700) == cc_after_fxam) ok("control-keeps-cc");
        else bad("control-keeps-cc", "was=" + hex(cc_after_fxam, 4) + " now=" + hex(u.sw() & 0x4700, 4));
    }

    std::printf("SUMMARY %d %d\n", passed, failed);
    return failed ? 1 : 0;
}
