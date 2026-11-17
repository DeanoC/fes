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

#include <sys/mman.h>

#include <cstdint>
#include <cstdio>
#include <initializer_list>
#include <cstring>
#include <cstdlib>
#include <array>
#include <random>
#include <string>
#include <fstream>
#include <iomanip>
#include <sstream>
#include <vector>

namespace {

// How long the bench waits for the unit: slice 2's forms settle in a few
// cycles, but slice 3's arithmetic runs up to four multiplies of four
// clocks apiece, so a 64-cycle wait ran out and let a uop issue while the
// unit still held it, which wedged the case and every one after it.
constexpr int X87_WAIT = 512;

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
// ... and with the precision field as well: PC 00 is 24 bits, 10 is 53 and
// 11 is the extended format, which 037Fh already holds.
uint16_t cw_of(unsigned rc, unsigned pc) {
    return uint16_t((0x037f & ~0x0300) | (pc << 8) | (rc << 10));
}

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

// A uop the unit held past the bench's patience. Forcing it through is a
// protocol violation that corrupts the unit's state, so the run reports it
// rather than letting the cases after it compare a wedged unit.
int held_for_ever = 0;
// Every state the slice 3 cases compared with the host's, so the record can
// state the count rather than estimate it.
long compared = 0;

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
        int waited = 0;
        for (; waited < X87_WAIT && top.hold; ++waited) tick();
        if (waited >= X87_WAIT) ++held_for_ever;
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
    // One uop presented for `held` cycles before it issues, as the speed
    // governor holds D at an instruction's first uop. The unit must not
    // wait on its own work while it waits there.
    void present_held(unsigned op, unsigned fmt, unsigned held) {
        top.iss = 1;
        top.iss_wait = 0;
        top.iss_op = op;
        top.iss_fmt = fmt;
        top.iss_first = 1;
        top.iss_last = 1;
        top.iss_part = 0;
        top.iss_fire = 0;
        top.eval();
        for (unsigned n = 0; n < held; ++n) tick();
        for (int n = 0; n < X87_WAIT && top.hold; ++n) tick();
        top.iss_fire = 1;
        top.eval();
        tick();
        top.iss = top.iss_first = top.iss_fire = 0;
        top.eval();
        commit(0, 0, true);
        settle();
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
    // The second instruction presented before the first commits, which is
    // what the CPU does: its uop sits in D while the one before it is still
    // in the pipeline. `present` returns only once the hold drops, so this
    // is the overlap the per-instruction helpers cannot make.
    void overlap(unsigned op_a, unsigned op_b) {
        present(op_a, 0, true, true, 0);
        top.wb = 1;                      // A commits while B waits in D
        top.wb_part = 0;
        top.wb_val = 0;
        top.wb_last = 1;
        top.iss = 1;
        top.iss_wait = 0;
        top.iss_op = op_b;
        top.iss_fmt = 0;
        top.iss_first = 1;
        top.iss_last = 1;
        top.iss_part = 0;
        top.iss_fire = 0;
        tick();
        top.wb = top.wb_last = 0;
        top.eval();
        for (int n = 0; n < X87_WAIT && top.hold; ++n) tick();
        top.iss_fire = 1;
        top.eval();
        tick();
        top.iss = top.iss_first = top.iss_fire = 0;
        top.eval();
        commit(0, 0, true);
        settle();
    }
    // Unmask the invalid-operation exception and raise one, which sets ES
    // and latches BUSY#.
    void settle() {
        for (int n = 0; n < X87_WAIT && top.busy_o; ++n) tick();
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
unsigned op_fstp_st(unsigned i) { return (5u << 8) | (0xd8 + i); }  // DD D8+i

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

// FSTP m32 presented while the instruction before it commits: the unit must
// take it and have its value ready, not hold for ever.
void top_push_overlap_store(Unit &u) {
    u.top.wb = 1;
    u.top.wb_part = 0;
    u.top.wb_val = 0;
    u.top.wb_last = 1;
    u.top.iss = 1;
    u.top.iss_wait = 0;
    u.top.iss_op = OP_FSTP_M32;
    u.top.iss_fmt = FMT_F32;
    u.top.iss_first = 1;
    u.top.iss_last = 1;
    u.top.iss_part = 0;
    u.top.iss_fire = 0;
    u.tick();
    u.top.wb = u.top.wb_last = 0;
    u.top.eval();
    int n = 0;
    for (; n < 64 && u.top.hold; ++n) {
        if (std::getenv("X87_TB_DBG") && n < 12)
            std::fprintf(stderr, "overlap.store n=%d hold=%d busy=%d seq=%d sw=%04x\n", n, int(u.top.hold),
                         int(u.top.busy_o), int(u.top.trace_seq), unsigned(u.top.sw_o));
        u.tick();
    }
    const uint32_t stored = u.top.sval;
    u.top.iss_fire = 1;
    u.top.eval();
    u.tick();
    u.top.iss = u.top.iss_first = u.top.iss_fire = 0;
    u.top.eval();
    u.commit(0, 0, true);
    u.settle();
    if (n >= 64) bad("overlap.store", "the store held for ever");
    else if (stored != 0x3f800000u) bad("overlap.store", "stored " + hex(stored, 8));  // 1.0
    else ok("overlap.store");
}


// ---- Slice 3's arithmetic: the host reference. ----
// Each case runs the form's own two opcode bytes on the host and compares the
// whole architectural state: the control, status and tag words and all eight
// registers, which is exactly what FNSAVE reports and what the unit's own
// FNSAVE hands back.
//
// The instruction is *generated* rather than written as assembly, because the
// forms differ only in those two bytes and GNU as does not name them the way
// the hardware behaves: `fsub %st,%st(1)` assembles to DC E1, and DC E1 on the
// hardware computes ST(0) - ST(1), which Intel calls FSUBR. The unit is given
// the bytes, so the reference has to be driven by the bytes too. The
// encodings were checked against a real x87 before this bench was written:
// with ST(0) = 3 and ST(1) = 10, D8 E1 and DC E9 and DE E9 give "destination
// minus the other operand", and D8 E9, DC E1 and DE E1 the reverse.
struct Image {
    uint16_t cw, sw, tw;
    uint8_t  reg[8][10];
    bool     setup_ok = true;   // the unit took the control word the case set
};

// The generated stub, called as
//   sw = stub(cw, st1_operand, st0_operand, memory_operand, image)
// It initialises the unit, sets the control word, pushes `pushes` extended
// operands, runs the form, reads the status word and saves the state.
using Stub = unsigned (*)(const void *, const void *, const void *, const void *, void *);

class StubPage {
  public:
    StubPage() {
        page_ = static_cast<uint8_t *>(mmap(nullptr, 4096, PROT_READ | PROT_WRITE,
                                            MAP_PRIVATE | MAP_ANONYMOUS, -1, 0));
        if (page_ == MAP_FAILED) page_ = nullptr;
    }
    ~StubPage() {
        if (page_) munmap(page_, 4096);
    }
    // Build and return a stub for one form. `ops` is the instruction's bytes.
    // The operands are a 16-byte-strided array at %rsi, and they are pushed
    // from the last to the first so that operand 0 ends up as ST(0).
    Stub build(unsigned pushes, const uint8_t *ops, unsigned nops) {
        // A deliberate failure, so the counterexample can be built rather
        // than argued: with FES_X87_NO_STUB set, every host stub is
        // unavailable, which is what a denied mprotect(PROT_EXEC) looks like.
        // Any row that reports a pass under it is a row that can pass without
        // comparing anything.
        if (std::getenv("FES_X87_NO_STUB")) return nullptr;
        if (!page_) return nullptr;
        if (mprotect(page_, 4096, PROT_READ | PROT_WRITE) != 0) return nullptr;
        std::vector<uint8_t> code;
        auto emit = [&](std::initializer_list<uint8_t> bytes) {
            for (uint8_t b : bytes) code.push_back(b);
        };
        emit({0xdb, 0xe3});                     // fninit
        emit({0xd9, 0x2f});                     // fldcw (%rdi)
        for (unsigned n = pushes; n-- > 0;)
            emit({0xdb, 0x6e, uint8_t(16 * n)});  // fldt 16n(%rsi)
        for (unsigned n = 0; n < nops; ++n) code.push_back(ops[n]);
        emit({0xdf, 0xe0});                     // fnstsw %ax, before anything clears it
        emit({0x41, 0xdd, 0x30});               // fnsave (%r8), which is no-wait
        emit({0x0f, 0xb7, 0xc0});               // movzwl %ax, %eax
        emit({0xc3});                           // ret
        std::memcpy(page_, code.data(), code.size());
        if (mprotect(page_, 4096, PROT_READ | PROT_EXEC) != 0) return nullptr;
        return reinterpret_cast<Stub>(page_);
    }

  private:
    uint8_t *page_ = nullptr;
};

// The 32-bit protected FNSAVE image: the words at 0, 4 and 8, and the eight
// registers from 28, ST(0) first.
void image_of(const uint8_t *raw, uint16_t sw, Image &out) {
    std::memcpy(&out.cw, raw + 0, 2);
    std::memcpy(&out.tw, raw + 8, 2);
    out.sw = sw;
    for (int n = 0; n < 8; ++n) std::memcpy(out.reg[n], raw + 28 + 10 * n, 10);
}

// `stack` holds the operands ST(0) first, 10 bytes each.
// Every host comparison in this bench goes through host_form, and it is the
// only place that can tell a stub page was unavailable. **The invariant lives
// here rather than at the call sites.** Nine call sites take its return value
// and five of them used to `break` out of their loop without recording
// anything, so a denied mprotect(PROT_EXEC) made those rows report a pass
// having compared nothing -- 704 of them, measured with FES_X87_NO_STUB.
// Guarding each site would have meant nine copies of one rule and a tenth
// grid getting missed; counting here covers every present and future caller.
unsigned stub_failures = 0;

bool host_form(StubPage &page, uint16_t cw, const uint8_t (*stack)[10], unsigned pushes,
               const void *mem, const uint8_t *ops, unsigned nops, Image &out) {
    alignas(16) uint8_t raw[128] = {0};
    alignas(16) uint8_t operands[8][16] = {};
    for (unsigned n = 0; n < pushes && n < 8; ++n) std::memcpy(operands[n], stack[n], 10);
    Stub stub = page.build(pushes, ops, nops);
    if (!stub) {
        ++stub_failures;
        return false;
    }
    const unsigned sw = stub(&cw, operands, nullptr, mem, raw);
    image_of(raw, uint16_t(sw), out);
    return true;
}

// The same form on the unit: the control word, the pushes as FLD m80 (a bit
// copy, so it raises nothing either side), the form itself, then the unit's
// own FNSAVE.
void unit_form(Unit &u, uint16_t cw, const uint8_t (*stack)[10], unsigned pushes,
               const uint8_t *mem, unsigned op, unsigned fmt, Image &out) {
    u.clear_latch();
    u.reg_form(OP_FNINIT);
    u.present(OP_FLDCW, FMT_I16, true, true, 0);
    u.commit(0, cw, true);
    u.settle();
    // The setup has to have taken, or the case is void rather than wrong: a
    // control word that never landed means the unit held the FLDCW and the
    // bench forced it through, which is how a stuck BUSY# latch once made 40
    // comparisons compare a fresh unit against a real result.
    out.setup_ok = u.cw() == cw;
    for (unsigned n = pushes; n-- > 0;) u.load(OP_FLD_M80, FMT_F80, stack[n]);
    if (fmt == 0) u.reg_form(op);
    else u.load(op, fmt, mem);
    out.cw = u.cw();
    out.sw = u.sw();
    out.tw = u.tw();
    // The registers in the order FNSAVE stores them, which is the order the
    // host's image has: ST(0) first. The unit's regs_o is the eight physical
    // registers, so TOP picks them out.
    for (unsigned n = 0; n < 8; ++n) u.sti(n, out.reg[n]);
}

// The status-word bits an arithmetic form defines: the six exception flags,
// the stack flag, ES, B, TOP and all four condition codes. C0, C2 and C3 are
// defined for the compares and undefined elsewhere, so the mask narrows for
// the forms that leave them alone.
// A register the tag word marks empty holds whatever was last written to it,
// which is architecturally undefined: the host's FNSAVE image carries the
// bytes a previous case left there and the unit's register file carries its
// own, so only the registers the tag word says are in use are compared.
// `reg` is indexed from ST(0), and the tag word by physical register, which
// TOP maps between.
bool reg_live(const Image &im, unsigned i) {
    const unsigned phys = ((im.sw >> 11) + i) & 7;
    return ((im.tw >> (2 * phys)) & 3) != 3;
}
std::string state_str(const Image &im, uint16_t swmask) {
    std::string s = "cw=" + hex(im.cw, 4) + " sw=" + hex(im.sw & swmask, 4) + " tw=" + hex(im.tw, 4);
    for (unsigned n = 0; n < 8; ++n) s += reg_live(im, n) ? " " + hex80(im.reg[n]) : " --";
    return s;
}
bool same_state(const Image &x, const Image &y, uint16_t swmask) {
    if (!x.setup_ok || !y.setup_ok) return false;
    if (x.cw != y.cw || (x.sw & swmask) != (y.sw & swmask) || x.tw != y.tw) return false;
    for (unsigned n = 0; n < 8; ++n)
        if (reg_live(x, n) && std::memcmp(x.reg[n], y.reg[n], 10) != 0) return false;
    return true;
}

// Set the unit's rounding mode with FLDCW.
void set_rc(Unit &u, unsigned rc) {
    const uint16_t cw = host_cw(rc);
    u.present(OP_FLDCW, FMT_F32, true, true, 0);
    u.commit(0, cw, true);
    u.settle();
}

std::array<uint8_t, 10> parse_ext80(const std::string &text) {
    if (text.size() != 20) throw std::runtime_error("TestFloat extF80 value is not 20 hex digits: " + text);
    const uint16_t se = uint16_t(std::stoul(text.substr(0, 4), nullptr, 16));
    const uint64_t sig = std::stoull(text.substr(4), nullptr, 16);
    std::array<uint8_t, 10> bytes{};
    std::memcpy(bytes.data(), &sig, sizeof sig);
    std::memcpy(bytes.data() + 8, &se, sizeof se);
    return bytes;
}

uint16_t testfloat_control(unsigned precision, unsigned rc) {
    const unsigned pc = precision == 32 ? 0 : precision == 64 ? 2 : 3;
    return uint16_t((0x037f & ~0x0300) | (pc << 8) | (rc << 10));
}

uint16_t testfloat_flags(unsigned flags) {
    // SoftFloat flags: inexact, underflow, overflow, infinite, invalid.
    // x87 status has a separate denormal-operand bit, which TestFloat does
    // not report, so compare the five corresponding architectural flags.
    return uint16_t(((flags & 0x01) ? 0x20 : 0) | ((flags & 0x02) ? 0x10 : 0) |
                    ((flags & 0x04) ? 0x08 : 0) | ((flags & 0x08) ? 0x04 : 0) |
                    ((flags & 0x10) ? 0x01 : 0));
}

struct TestFloatManifest {
    unsigned vectors = 0;
    unsigned configs = 0;
};

TestFloatManifest run_testfloat_manifest(const std::string &manifest_path) {
    std::ifstream manifest(manifest_path);
    if (!manifest) throw std::runtime_error("cannot open TestFloat manifest: " + manifest_path);
    TestFloatManifest total;
    std::string operation, vector_path;
    unsigned precision = 0, rc = 0, declared = 0;
    std::string line;
    while (std::getline(manifest, line)) {
        if (line.empty()) continue;
        std::istringstream fields(line);
        if (!(fields >> operation >> precision >> rc >> declared >> std::quoted(vector_path)))
            throw std::runtime_error("malformed TestFloat manifest line: " + line);
        if (rc > 3 || (precision != 32 && precision != 64 && precision != 80))
            throw std::runtime_error("invalid TestFloat precision or rounding mode: " + line);
        std::ifstream vectors(vector_path);
        if (!vectors) throw std::runtime_error("cannot open TestFloat vectors: " + vector_path);
        Unit unit;
        const uint16_t cw = testfloat_control(precision, rc);
        unsigned count = 0, differences = 0;
        std::string first;
        std::string vector_line;
        while (std::getline(vectors, vector_line)) {
            if (vector_line.empty()) continue;
            std::istringstream values(vector_line);
            std::vector<std::string> v;
            for (std::string token; values >> token;) v.push_back(token);
            const bool unary = operation == "roundToInt";
            const bool compare = operation == "eq" || operation == "le" || operation == "lt";
            const size_t expected_fields = unary ? 3 : 4;
            if (v.size() != expected_fields)
                throw std::runtime_error("malformed TestFloat vector for " + operation + ": " + vector_line);
            std::array<uint8_t, 10> a = parse_ext80(v[0]);
            std::array<uint8_t, 10> b{};
            if (!unary) b = parse_ext80(v[1]);
            const std::string &expected = unary ? v[1] : v[2];
            const unsigned expected_flags = std::stoul(v[expected_fields - 1], nullptr, 16);
            unsigned op = 0;
            if (operation == "add") op = 0xc1;          // D8 C1: FADD ST,ST(1)
            else if (operation == "mul") op = 0xc9;     // D8 C9: FMUL ST,ST(1)
            else if (operation == "sub") op = 0xe1;     // D8 E1: FSUB ST,ST(1)
            else if (operation == "eq") op = 0x5e1;      // DD E1: FUCOM ST(1), quiet for QNaNs
            else if (operation == "le" || operation == "lt") op = 0xd1; // D8 D1: ordered FCOM
            else if (unary) op = 0x1fc;                  // D9 FC: FRNDINT
            else throw std::runtime_error("unsupported TestFloat extF80 operation: " + operation);
            const uint8_t stack[2][10] = {{a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7], a[8], a[9]},
                                          {b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7], b[8], b[9]}};
            Image got;
            unit_form(unit, cw, stack, unary ? 1 : 2, nullptr, op, 0, got);
            bool mismatch = !got.setup_ok || (got.sw & 0x003d) != testfloat_flags(expected_flags);
            if (compare) {
                const unsigned condition = got.sw & 0x4500; // C0, C2, C3
                const bool c0 = (condition & 0x0100) != 0, c2 = (condition & 0x0400) != 0;
                const bool c3 = (condition & 0x4000) != 0;
                const bool want = std::stoul(expected, nullptr, 16) != 0;
                const bool have = operation == "eq" ? (c3 && !c0 && !c2)
                                  : operation == "le" ? ((c0 || c3) && !c2)
                                                       : (c0 && !c2);
                mismatch = mismatch || have != want;
            } else {
                const auto expected_bytes = parse_ext80(expected);
                mismatch = mismatch || std::memcmp(got.reg[0], expected_bytes.data(), expected_bytes.size()) != 0;
            }
            if (mismatch) {
                if (!differences) {
                    first = "vector=" + std::to_string(count) + " input=" + vector_line +
                            " got=" + hex80(got.reg[0]) + "/" + hex(got.sw & 0x003d, 4) +
                            " want=" + expected + "/" + hex(testfloat_flags(expected_flags), 4);
                }
                ++differences;
            }
            ++count;
        }
        if (count != declared)
            throw std::runtime_error("TestFloat manifest count mismatch for " + vector_path + ": " +
                                     std::to_string(count) + " != " + std::to_string(declared));
        const std::string name = "testfloat-extF80-" + operation + "-p" + std::to_string(precision) + "-rc" +
                                 std::to_string(rc);
        if (differences) bad(name, std::to_string(differences) + " of " + std::to_string(count) + " " + first);
        else ok(name);
        total.vectors += count;
        ++total.configs;
    }
    if (!total.configs) throw std::runtime_error("TestFloat manifest contains no configurations");
    return total;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::string testfloat_manifest;
    if (argc == 3 && std::string(argv[1]) == "--testfloat-manifest") testfloat_manifest = argv[2];
    else if (argc != 1) throw std::runtime_error("usage: x87_unit_tb [--testfloat-manifest path]");

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
        // Invalid and the stack flag, C1 clear, and the destination holding
        // the real indefinite, so its tag is special (10) and not empty.
        if ((u.sw() & 0x41) == 0x41 && (u.sw() & 0x200) == 0 && (u.tw() & 3) == 2) ok("stack-underflow");
        else bad("stack-underflow", "sw=" + hex(u.sw(), 4) + " tw=" + hex(u.tw(), 4));

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

        // The same check at the cycle the unit goes idle, which is the one
        // the apply makes: ES rises with the apply, and the latch has to be
        // up in that very cycle. An edge detector a cycle later leaves a
        // window where an ESC sees neither busy nor latched (Codex P1).
        u.present(OP_FLDCW, FMT_F32, true, true, 0);
        u.commit(0, 0x037e, true);   // invalid unmasked
        u.settle();
        u.present(OP_FCHS, 0, true, true, 0);
        u.commit(0, 0, true);        // an empty ST(0): an unmasked stack fault
        {
            bool idle_seen = false, gap = false;
            for (int n = 0; n < 64 && !idle_seen; ++n) {
                if (!u.top.busy_o) {
                    idle_seen = true;
                    u.top.iss = 1;
                    u.top.iss_wait = 0;
                    u.top.iss_op = OP_FCHS;
                    u.top.iss_fmt = 0;
                    u.top.iss_first = 1;
                    u.top.iss_last = 1;
                    u.top.iss_part = 0;
                    u.top.eval();
                    gap = !u.top.hold;
                    u.top.iss = u.top.iss_first = u.top.iss_last = 0;
                    u.top.eval();
                } else {
                    u.tick();
                }
            }
            if (!idle_seen) bad("latch-idle-edge", "the unit never went idle");
            else if (gap) bad("latch-idle-edge", "an ESC was free the cycle the unit went idle");
            else ok("latch-idle-edge");
        }
        u.reg_form(OP_FNINIT);
        u.clear_latch();

        // The same window on FLDCW's own apply: the flag is already set and
        // masked, and the word being loaded unmasks it, so ES follows the
        // control word this apply writes rather than any flag it raises.
        // The latch has to be set against that word, not from the ES edge a
        // cycle later (Codex P1; 387 PRM section 2.3.1).
        u.reg_form(OP_FNINIT);
        u.reg_form(OP_FCHS);             // an empty ST(0): IE and SF, masked
        u.present(OP_FLDCW, FMT_F32, true, true, 0);
        u.commit(0, 0x037e, true);       // the same word with IM cleared
        {
            bool idle_seen = false, gap = false;
            for (int n = 0; n < 64 && !idle_seen; ++n) {
                if (!u.top.busy_o) {
                    idle_seen = true;
                    u.top.iss = 1;
                    u.top.iss_wait = 0;
                    u.top.iss_op = OP_FCHS;
                    u.top.iss_fmt = 0;
                    u.top.iss_first = 1;
                    u.top.iss_last = 1;
                    u.top.iss_part = 0;
                    u.top.eval();
                    gap = !u.top.hold;
                    u.top.iss = u.top.iss_first = u.top.iss_last = 0;
                    u.top.eval();
                } else {
                    u.tick();
                }
            }
            if (!idle_seen) bad("latch-ldcw-unmask", "the unit never went idle");
            else if (gap) bad("latch-ldcw-unmask", "an ESC was free the cycle FLDCW unmasked");
            else ok("latch-ldcw-unmask");
        }
        u.reg_form(OP_FNINIT);
        u.clear_latch();

        // FSTP ST(0) with an empty stack and the invalid exception masked:
        // the masked result writes the real indefinite to the destination
        // and the pop then empties that same register, so the tag word must
        // read empty for it and TOP must have moved on (Codex P2; 387 PRM
        // section 2.3.6, the masked stack-underflow response).
        u.reg_form(OP_FNINIT);
        u.reg_form(op_fstp_st(0));
        {
            const unsigned top_now = (u.sw() >> 11) & 7;
            if (top_now != 1) bad("fstp-st0-empty", "top=" + hex(top_now, 1));
            else if (u.tw() != 0xffff) bad("fstp-st0-empty", "tw=" + hex(u.tw(), 4) + " want ffff");
            else if ((u.sw() & 0x41) != 0x41) bad("fstp-st0-empty", "sw=" + hex(u.sw(), 4) + " want IE and SF");
            else ok("fstp-st0-empty");
        }
        u.reg_form(OP_FNINIT);
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

        // A flush DURING a committed divide must not lose it either, and
        // must not strand the unit. This is the case a straight-through
        // flush mapping would have broken on the DE25 backend: arithmetic is
        // entered after the instruction commits, the envelope deliberately
        // lets a retired instruction run on through a later squash, and a
        // backend cancelled mid-recurrence would never return a result. A
        // load's flush case cannot reach it, because a load has no multi-
        // cycle arithmetic to cancel.
        {
            uint8_t got2[10];
            const uint8_t two[10]  = {0,0,0,0,0,0,0,0x80,0x00,0x40};  // 2.0
            const uint8_t four[10] = {0,0,0,0,0,0,0,0x80,0x01,0x40};  // 4.0
            u.clear_latch();
            u.reg_form(OP_FNINIT);
            u.load(OP_FLD_M80, FMT_F80, two);    // ST(1) <- 2.0
            u.load(OP_FLD_M80, FMT_F80, four);   // ST(0) <- 4.0
            // FDIV ST(0),ST(1): D8 F1. Commit it, then squash a younger
            // instruction while its arithmetic is still running.
            // The flush is SWEPT across the operation rather than pulsed at one
            // offset. A divide takes tens of clocks and the offset at which the
            // backend is actually running is not something the bench should
            // assume: the first version of this case pulsed at 4 clocks, which
            // is still in the envelope's dispatch, and a deliberately wrong
            // `.flush(flush)` mapping passed it. Sweeping means at least one
            // offset lands inside the recurrence, and that mapping now fails.
            int flush_bad = 0;
            std::string flush_first;
            for (int at : {2, 4, 8, 12, 16, 24, 32, 40, 48, 56, 64, 72, 80}) {
                u.clear_latch();
                u.reg_form(OP_FNINIT);
                u.load(OP_FLD_M80, FMT_F80, two);
                u.load(OP_FLD_M80, FMT_F80, four);
                u.present(0x0f1, 0, true, true, 0);
                u.commit(0, 0, true);
                for (int n = 0; n < at; ++n) u.tick();
                u.top.flush = 1;
                u.tick();
                u.top.flush = 0;
                u.settle();
                u.st0(got2);
                const bool wrong = u.top.busy_o || got2[9] != 0x40 || got2[8] != 0x00;
                if (wrong && !flush_bad++)
                    flush_first = "at " + std::to_string(at) + " clocks: " +
                                  (u.top.busy_o ? std::string("the unit stayed busy")
                                                : hex80(got2));
            }
            if (flush_bad) bad("flush-during-divide",
                               std::to_string(flush_bad) + " offsets, first " + flush_first);
            else ok("flush-during-divide");
            u.clear_latch();
            u.reg_form(OP_FNINIT);
            u.load(OP_FLD_M80, FMT_F80, two);
            u.load(OP_FLD_M80, FMT_F80, four);
            u.reg_form(0x0f1);
            // And the unit takes a fresh operation afterwards.
            u.reg_form(OP_FCHS);
            u.st0(got2);
            if (got2[9] == 0xc0) ok("flush-during-divide-next");   // FCHS of 2.0
            else bad("flush-during-divide-next", hex80(got2));
        }

        // The port F1h reset while a divide is in flight: the unit comes back
        // in its RESET state and answers the next operation, rather than
        // waiting for a result that was thrown away.
        {
            uint8_t got3[10];
            const uint8_t two[10]  = {0,0,0,0,0,0,0,0x80,0x00,0x40};
            const uint8_t four[10] = {0,0,0,0,0,0,0,0x80,0x01,0x40};
            u.clear_latch();
            u.reg_form(OP_FNINIT);
            u.load(OP_FLD_M80, FMT_F80, two);
            u.load(OP_FLD_M80, FMT_F80, four);
            // Swept, with a FAST form chained immediately after, which is
            // what a stale result corrupts. With `f1` removed from both the
            // backend's reset and the cancel condition this fails at nine
            // offsets, first at 36 clocks, returning the abandoned divide's
            // quotient as the sum -- so the `f1` cancel is load-bearing, not
            // belt-and-braces. An earlier version of this case chained a
            // divide and could not fail; see the record.
            int f1_bad = 0;
            std::string f1_first;
            // Every clock across the divide, not a sample: the offsets at
            // which this can go wrong are a narrow window.
            for (int at = 1; at < 85; ++at) {
                u.clear_latch();
                u.reg_form(OP_FNINIT);
                u.load(OP_FLD_M80, FMT_F80, two);
                u.load(OP_FLD_M80, FMT_F80, four);
                u.present(0x0f1, 0, true, true, 0);   // FDIV ST,ST(1): 4.0/2.0
                u.commit(0, 0, true);
                for (int n = 0; n < at; ++n) u.tick();
                u.top.f1 = 1;
                u.tick();
                u.top.f1 = 0;
                u.top.eval();
                // Straight into a second divide with a different answer.
                u.clear_latch();
                u.load(OP_FLD_M80, FMT_F80, four);
                u.load(OP_FLD_M80, FMT_F80, four);
                // An FADD, deliberately, NOT a second divide. A divide is
                // held in B_ISSUE by the slow lane's own admission interlock
                // (`slow_ready && !slow_reserved`) until the abandoned one
                // drains, so chaining a divide cannot see a stale result and
                // this case passed with the cancel broken. A fast form is
                // accepted at once, and `raw = fast_valid ? fast_raw :
                // slow_raw` hands B_WAIT the old quotient if the abandoned
                // divide completes first. 4.0+4.0 is 8.0 and the abandoned
                // quotient is 2.0, so a stale take is visible.
                u.reg_form(0x0c1);                    // FADD ST,ST(1): 8.0
                u.st0(got3);
                if ((got3[9] != 0x40 || got3[8] != 0x02) && !f1_bad++)
                    f1_first = "at " + std::to_string(at) + " clocks: " + hex80(got3);
            }
            if (f1_bad) bad("f1-during-divide",
                            std::to_string(f1_bad) + " offsets, first " + f1_first);
            else ok("f1-during-divide");
        }

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

        // Eight pushes back to back with no settling between them, which is
        // what a program does: every one must land, and the stack must come
        // back to TOP 0 with all eight registers written.
        u.clear_latch();
        u.reg_form(OP_FNINIT);
        uint8_t v[8][10];
        for (int n = 0; n < 8; ++n) {
            std::memset(v[n], 0, 10);
            v[n][7] = 0x80;
            v[n][8] = uint8_t(0xff);
            v[n][9] = uint8_t(0x3f + n);   // 1.0, 2.0, 4.0, ... by exponent
            // Each load's three parts, with no settle() after the last.
            for (int part = 2; part >= 0; --part) {
                u.present(OP_FLD_M80, FMT_F80, part == 2, part == 0, unsigned(part));
                uint32_t w = 0;
                std::memcpy(&w, v[n] + 4 * part, part == 2 ? 2 : 4);
                u.commit(unsigned(part), w, part == 0);
            }
        }
        u.settle();
        {
            bool ok8 = ((u.sw() >> 11) & 7) == 0 && u.tw() == 0x0000;
            std::string what = "top=" + hex((u.sw() >> 11) & 7, 1) + " tw=" + hex(u.tw(), 4);
            for (int n = 0; n < 8 && ok8; ++n) {
                uint8_t got[10];
                u.sti(unsigned(7 - n), got);   // ST(7) is the first pushed
                if (std::memcmp(got, v[n], 10) != 0) {
                    ok8 = false;
                    what += " st" + std::to_string(7 - n) + "=" + hex80(got) + " want " + hex80(v[n]);
                }
            }
            if (ok8) ok("eight-pushes");
            else bad("eight-pushes", what);
        }

        // The next instruction presented before the one before it commits,
        // which is what the CPU does and what the identity-on run tripped
        // on: FNINIT and FNCLEX skip the board's latch, not the unit, so
        // each of these must leave both instructions' work done.
        u.clear_latch();
        // `top` is where TOP must end up, counting the pushes after the
        // last FNINIT in the pair, from the FNINIT before it.
        struct Overlap { const char *label; unsigned a, b, top; };
        const Overlap overlaps[] = {
            {"const-then-init", op_const(0), OP_FNINIT, 0},
            {"init-then-init", OP_FNINIT, OP_FNINIT, 0},
            {"clex-then-const", OP_FNCLEX, op_const(0), 7},
            {"init-then-const", OP_FNINIT, op_const(0), 7},
            {"const-then-const", op_const(0), op_const(3), 6},
        };
        for (const Overlap &o : overlaps) {
            u.reg_form(OP_FNINIT);
            u.overlap(o.a, o.b);
            // Whatever the pair, the unit must be idle and its state must be
            // the second instruction's: FNINIT leaves the stack empty, a
            // constant leaves one pushed.
            const bool pushed = o.b != OP_FNINIT && o.b != OP_FNCLEX;
            const unsigned want_top = o.top;
            if (u.top.busy_o) bad(std::string("overlap.") + o.label, "the unit stayed busy");
            else if (((u.sw() >> 11) & 7) != want_top)
                bad(std::string("overlap.") + o.label, "top=" + hex((u.sw() >> 11) & 7, 1));
            else if (pushed && (u.tw() & 0xc000) == 0xc000)
                bad(std::string("overlap.") + o.label, "tw=" + hex(u.tw(), 4));
            else ok(std::string("overlap.") + o.label);
        }

        // A store presented before the instruction before it commits, which
        // is what the CPU does: the store must still take and answer.
        u.clear_latch();
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, one);
        {
            // FLD ST(0) commits while FSTP m32 waits in D.
            u.present(op_fld_st(0), 0, true, true, 0);
            top_push_overlap_store(u);
        }

        // The governor holds D at an instruction's first uop, so a form is
        // presented for cycles before it issues: FNINIT and FNCLEX take a
        // slot of their own when they are taken, and must not then wait on
        // it. The lockstep at a governor preset deadlocked here.
        u.clear_latch();
        u.reg_form(OP_FNINIT);
        u.load(OP_FLD_M80, FMT_F80, one);
        u.present_held(OP_FNINIT, 0, 6);
        if (u.cw() != 0x037f || u.sw() != 0 || u.tw() != 0xffff)
            bad("held.fninit", "cw=" + hex(u.cw(), 4) + " sw=" + hex(u.sw(), 4) + " tw=" + hex(u.tw(), 4));
        else ok("held.fninit");
        u.load(OP_FLD_M80, FMT_F80, one);
        u.present_held(OP_FNCLEX, 0, 4);
        u.present_held(op_const(0), 0, 5);
        if (((u.sw() >> 11) & 7) == 6) ok("held.forms");
        else bad("held.forms", "top=" + hex((u.sw() >> 11) & 7, 1));

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



    // ---- A write to port F0h in the cycle the error summary rises. ----
    // The apply that raises an exception sets the board's latch itself, so
    // the status word's own edge fires the cycle after it. A write to F0h
    // landing in that one cycle has to win: the port is what clears the
    // latch, and letting the edge undo the clear left the latch set and
    // every later ESC held for ever, which is how 40 comparisons in this
    // bench once ran against a unit still in its FNINIT state. The write is
    // made at each of the first few cycles after the apply, so whichever one
    // the edge falls in is covered.
    {
        bool stuck = false, raised = true;
        for (int delay = 0; delay < 4 && !stuck && raised; ++delay) {
            Unit u;
            u.clear_latch();
            u.reg_form(OP_FNINIT);
            u.present(OP_FLDCW, FMT_I16, true, true, 0);
            u.commit(0, 0x037e, true);   // invalid unmasked
            u.settle();
            u.clear_latch();
            // FCHS on an empty ST(0) is an unmasked stack fault.
            u.present(OP_FCHS, 0, true, true, 0);
            u.commit(0, 0, true);
            // The apply is the cycle the unit goes idle.
            for (int n = 0; n < X87_WAIT && u.top.busy_o; ++n) u.tick();
            raised = u.top.latched != 0;
            for (int n = 0; n < delay; ++n) u.tick();
            u.clear_latch();
            if (u.top.latched) stuck = true;
        }
        if (!raised) bad("latch-f0-edge", "the exception did not latch BUSY#");
        else if (stuck) bad("latch-f0-edge", "port F0h did not clear the latch beside the summary's edge");
        else ok("latch-f0-edge");
    }

    // ---- Slice 3: add, subtract, reverse subtract and multiply. ----
    // Each form runs over the operand classes the 387 PRM names, as pairs, in
    // every rounding mode, and the whole architectural state is compared with
    // the host's. The status word's mask leaves out C0, C2 and C3, which are
    // undefined for the arithmetic forms, and keeps C1, which says which way
    // a rounding went.
    {
        StubPage page;
        const uint16_t ARITH_SW = 0xfaff;   // everything but C0, C2 and C3
        const uint16_t CMP_SW = 0xffff;     // the compares define all of them
        struct Form {
            const char *name;
            unsigned    esc;      // ESC's low three bits
            unsigned    modrm;    // ... and the ModRM byte
            unsigned    pushes;
            unsigned    fmt;      // 0 for a register form
            uint16_t    swmask;
        };
        const Form forms[] = {
            // D8: the destination is ST(0) and the other operand ST(1).
            {"fadd.st0",   0x0, 0xc1, 2, 0, ARITH_SW},
            {"fmul.st0",   0x0, 0xc9, 2, 0, ARITH_SW},
            {"fsub.st0",   0x0, 0xe1, 2, 0, ARITH_SW},
            {"fsubr.st0",  0x0, 0xe9, 2, 0, ARITH_SW},
            // DC: the destination is ST(1), and E0h and E8h swap sense.
            {"fadd.sti",   0x4, 0xc1, 2, 0, ARITH_SW},
            {"fmul.sti",   0x4, 0xc9, 2, 0, ARITH_SW},
            {"fsub.sti",   0x4, 0xe1, 2, 0, ARITH_SW},
            {"fsubr.sti",  0x4, 0xe9, 2, 0, ARITH_SW},
            // DE: the same, and then ST(0) is popped.
            {"faddp",      0x6, 0xc1, 2, 0, ARITH_SW},
            {"fmulp",      0x6, 0xc9, 2, 0, ARITH_SW},
            {"fsubp",      0x6, 0xe1, 2, 0, ARITH_SW},
            {"fsubrp",     0x6, 0xe9, 2, 0, ARITH_SW},
            // Slice 4's divides. D8 F1 is FDIV ST,ST(1) and D8 F9 FDIVR;
            // DC F1 is FDIVR ST(1),ST and DC F9 FDIV, the E0/E8 swap again;
            // DE F1 and F9 are those two with the pop.
            {"fdiv.st0",   0x0, 0xf1, 2, 0, ARITH_SW},
            {"fdivr.st0",  0x0, 0xf9, 2, 0, ARITH_SW},
            {"fdivr.sti",  0x4, 0xf1, 2, 0, ARITH_SW},
            {"fdiv.sti",   0x4, 0xf9, 2, 0, ARITH_SW},
            {"fdivrp",     0x6, 0xf1, 2, 0, ARITH_SW},
            {"fdivp",      0x6, 0xf9, 2, 0, ARITH_SW},
            // The compares, ordered and unordered, with and without the pops.
            {"fcom",       0x0, 0xd1, 2, 0, CMP_SW},
            {"fcomp",      0x0, 0xd9, 2, 0, CMP_SW},
            {"fcompp",     0x6, 0xd9, 2, 0, CMP_SW},
            {"fucom",      0x5, 0xe1, 2, 0, CMP_SW},
            {"fucomp",     0x5, 0xe9, 2, 0, CMP_SW},
            {"fucompp",    0x2, 0xe9, 2, 0, CMP_SW},
            // Slice 4's remainders, with CMP_SW and not ARITH_SW: FAFFh
            // masks out C0, C2 and C3, which are exactly the bits a remainder
            // reports -- C2 for a partial step and C0, C3 and C1 for the
            // quotient -- so a remainder row under ARITH_SW would check none
            // of what this slice is for.
            {"fprem",      0x1, 0xf8, 2, 0, CMP_SW},
            {"fprem1",     0x1, 0xf5, 2, 0, CMP_SW},
            // The one-operand forms. FSCALE takes ST(1) as its scale.
            {"fsqrt",      0x1, 0xfa, 1, 0, ARITH_SW},
            {"frndint",    0x1, 0xfc, 1, 0, ARITH_SW},
            {"fxtract",    0x1, 0xf4, 1, 0, ARITH_SW},
            {"fscale",     0x1, 0xfd, 2, 0, ARITH_SW},
        };

        // The pairs: every named pattern against every named pattern, and
        // then random ones. `named` is the count at the front of `ext` that
        // the store section put there by hand.
        const size_t named = 22;
        std::vector<std::pair<size_t, size_t>> pairs;
        for (size_t ia = 0; ia < named && ia < ext.size(); ++ia)
            for (size_t ib = 0; ib < named && ib < ext.size(); ++ib) pairs.emplace_back(ia, ib);
        for (int n = 0; n < 200; ++n)
            pairs.emplace_back(rng() % ext.size(), rng() % ext.size());

        for (const Form &f : forms) {
            for (unsigned rc = 0; rc < 4; ++rc) {
                const uint16_t cw = host_cw(rc);
                const uint8_t ops[2] = {uint8_t(0xd8 | f.esc), uint8_t(f.modrm)};
                int diffs = 0, ran = 0;
                std::string first;
                Unit u;
                for (const auto &pr : pairs) {
                    const uint8_t *a = ext[pr.first].data();   // ST(0)
                    const uint8_t *b = ext[pr.second].data();  // ST(1)
                    Image want{}, got{};
                    uint8_t stack[2][10];
                    std::memcpy(stack[0], a, 10);
                    std::memcpy(stack[1], b, 10);
                    if (!host_form(page, cw, stack, f.pushes, nullptr, ops, 2, want)) {
                        bad(std::string(f.name) + "-rc" + std::to_string(rc), "no stub page");
                        break;
                    }
                    unit_form(u, cw, stack, f.pushes, nullptr, (f.esc << 8) | f.modrm, f.fmt, got);
                    ++ran;
                    ++compared;
                    if (!same_state(want, got, f.swmask)) {
                        const std::string what = "st0=" + hex80(a) + " st1=" + hex80(b) +
                                                 " got " + state_str(got, f.swmask) +
                                                 " want " + state_str(want, f.swmask);
                        if (!diffs) first = what;
                        // X87_TB_VERBOSE names one form, or "all", and prints
                        // every mismatch rather than only the first.
                        const char *vb = std::getenv("X87_TB_VERBOSE");
                        if (vb && (std::string(vb) == "all" || std::string(vb) == f.name))
                            std::fprintf(stderr, "DIFF %s-rc%u %s\n", f.name, rc, what.c_str());
                        ++diffs;
                    }
                }
                const std::string name = std::string(f.name) + "-rc" + std::to_string(rc);
                if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(ran) + " " + first);
                else ok(name);
            }
        }
    }


    // ---- Back-to-back arithmetic pairs. ----
    // The registered-read hazard is this unit's recurring defect: anything
    // derived from a REGISTERED value is a cycle late relative to a
    // sequencer that transitions in one edge, so work begun in the assigning
    // state sees the PREVIOUS operation's value. It has appeared in every
    // slice, and because the stale value is usually plausible the symptom is
    // a wrong result rather than a crash. A single operation hides the class
    // entirely: the working registers start from a settled unit, so a late
    // read and a timely one give the same answer.
    //
    // So every pair below runs the second instruction through `overlap`,
    // which presents it while the first is still in flight -- the CPU's own
    // timing, and the only way the bench reaches the state a stale read
    // would corrupt. The pairs deliberately cross the two arithmetic paths:
    // FSCALE and FRNDINT stay on the sequencer in both configurations, and
    // FADD, FSUB, FMUL, FDIV and FSQRT move to fes_x87_core when the DE25
    // backend is selected, so an op-A/op-B pair tests a handover in each
    // direction as well as each path against itself. The operands are chosen
    // so that the FIRST operation leaves each kind of working state the
    // second could inherit: an exact cancellation to zero, a tiny result, an
    // overflow, a saturating scale and an exact normal.
    {
        StubPage page;
        const uint16_t ARITH_SW = 0xfaff;   // everything but C0, C2 and C3
        struct Op { const char *name; uint8_t esc, modrm; };
        // The five forms the DE25 backend answers, then the two that stay on
        // the sequencer whichever backend is selected.
        const Op ops[] = {
            {"fadd",  0xd8, 0xc1}, {"fsub",  0xd8, 0xe1}, {"fmul", 0xd8, 0xc9},
            {"fdiv",  0xd8, 0xf1}, {"fsqrt", 0xd9, 0xfa},
            {"fscale", 0xd9, 0xfd}, {"frndint", 0xd9, 0xfc},
        };
        struct Pair { const char *name; uint8_t a[10], b[10]; };
        const Pair cases[] = {
            // 1.0 and -1.0: FADD cancels exactly to zero, which is the one
            // state the backend's raw result cannot pack by itself.
            {"cancel", {0,0,0,0,0,0,0,0x80,0xff,0x3f}, {0,0,0,0,0,0,0,0x80,0xff,0xbf}},
            // The smallest normal over a large value: a tiny result, so the
            // first operation leaves wtiny set.
            {"tiny",   {0,0,0,0,0,0,0,0x80,0x01,0x00}, {0,0,0,0,0,0,0,0x80,0xfe,0x7f}},
            // Two values near the top of the range: a product overflows.
            {"over",   {0,0,0,0,0,0,0,0x80,0xfe,0x7f}, {0,0,0,0,0,0,0,0x80,0xfe,0x7f}},
            // A denormal operand, which raises DE on either path.
            {"denorm", {1,0,0,0,0,0,0,0x00,0x00,0x00}, {0,0,0,0,0,0,0,0x80,0xff,0x3f}},
            // Exact normals, where nothing rounds and nothing is flagged.
            {"exact",  {0,0,0,0,0,0,0,0x80,0x00,0x40}, {0,0,0,0,0,0,0,0x80,0xff,0x3f}},
        };
        for (const Pair &c : cases) {
            for (unsigned rc = 0; rc < 4; ++rc) {
                const uint16_t cw = host_cw(rc);
                int diffs = 0, ran = 0;
                std::string first;
                Unit u;
                for (const Op &a : ops) {
                    for (const Op &b : ops) {
                        const uint8_t bytes[4] = {a.esc, a.modrm, b.esc, b.modrm};
                        uint8_t stack[2][10];
                        std::memcpy(stack[0], c.a, 10);
                        std::memcpy(stack[1], c.b, 10);
                        Image want{}, got{};
                        if (!host_form(page, cw, stack, 2, nullptr, bytes, 4, want)) {
                            bad(std::string("b2b-") + c.name, "no stub page");
                            break;
                        }
                        // The unit side: the same two instructions, with the
                        // second presented before the first commits.
                        u.clear_latch();
                        u.reg_form(OP_FNINIT);
                        u.present(OP_FLDCW, FMT_I16, true, true, 0);
                        u.commit(0, cw, true);
                        u.settle();
                        got.setup_ok = u.cw() == cw;
                        for (unsigned n = 2; n-- > 0;) u.load(OP_FLD_M80, FMT_F80, stack[n]);
                        u.overlap(((a.esc & 7) << 8) | a.modrm, ((b.esc & 7) << 8) | b.modrm);
                        got.cw = u.cw();
                        got.sw = u.sw();
                        got.tw = u.tw();
                        for (unsigned n = 0; n < 8; ++n) u.sti(n, got.reg[n]);
                        ++ran;
                        ++compared;
                        if (!same_state(want, got, ARITH_SW)) {
                            const std::string what = std::string(a.name) + "/" + b.name +
                                                     " st0=" + hex80(c.a) + " st1=" + hex80(c.b) +
                                                     " got " + state_str(got, ARITH_SW) +
                                                     " want " + state_str(want, ARITH_SW);
                            if (!diffs) first = what;
                            const char *vb = std::getenv("X87_TB_VERBOSE");
                            if (vb && (std::string(vb) == "all" || std::string(vb) == "b2b"))
                                std::fprintf(stderr, "DIFF b2b-%s-rc%u %s\n", c.name, rc, what.c_str());
                            ++diffs;
                        }
                    }
                }
                const std::string name = std::string("b2b-") + c.name + "-rc" + std::to_string(rc);
                if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(ran) + " " + first);
                else ok(name);
            }
        }
    }

    // ---- The remainders' partial steps, chained. ----
    // The plan names this gate: directed FPREM and FPREM1 cases over large
    // exponent differences, checking each partial step's C2 and the final
    // C0, C3 and C1 quotient bits.
    //
    // A single instruction cannot check it, because the behaviour IS the
    // sequence: while the exponent difference is 64 or more the step returns
    // a partial remainder with C2 set and software loops. So each case runs
    // the instruction repeatedly on the host and on the unit, comparing the
    // whole state every step, and feeds the step's result back in as the next
    // dividend -- which is what a program using FPREM does.
    //
    // A partial step reduces n = (expDiff & 31) | 32 quotient bits, so the
    // step COUNT is data-dependent (cancellation shrinks the remainder
    // unpredictably) and the chain runs until the host clears C2 rather than
    // for a fixed number of steps.
    {
        StubPage page;
        struct Chain { const char *name; uint16_t ea; uint64_t sa; uint16_t eb; uint64_t sb; };
        const Chain chains[] = {
            // Differences at and around the boundary: 63 completes in one
            // step, 64 and 65 take two.
            {"d63",   uint16_t(0x3fff + 63), 0x9357bdf000000000ull, 0x3fff, 0x8000000000000000ull},
            {"d64",   uint16_t(0x3fff + 64), 0x9357bdf000000000ull, 0x3fff, 0x8000000000000000ull},
            {"d65",   uint16_t(0x3fff + 65), 0x9357bdf000000000ull, 0x3fff, 0x8000000000000000ull},
            // ... and far beyond it, where the chain runs many steps.
            {"d200",  uint16_t(0x3fff + 200), 0x923456789abcdef0ull, 0x3fff, 0x8000000000000000ull},
            {"d299",  uint16_t(0x3fff + 299), 0xaaaaaaaaaaaaaaaaull, 0x4000, 0xc000000000000000ull},
            {"d359",  uint16_t(0x3fff + 359), 0xfedcba9876543210ull, 0x4000, 0xc000000000000000ull},
            {"d999",  uint16_t(0x3fff + 999), 0xfedcba9876543210ull, 0x4000, 0xc000000000000000ull},
            // A divisor that is not a power of two and a dividend that is,
            // so the quotient's low bits are not all zero at the end.
            {"d130",  uint16_t(0x3fff + 130), 0x8000000000000000ull, 0x3fff, 0xb504f333f9de6484ull},
            // Negative differences: the quotient is zero and the dividend
            // comes back, except for FPREM1 at exactly -1.
            {"dm1",   0x3ffe, 0xc000000000000000ull, 0x3fff, 0x8000000000000000ull},
            {"dm1eq", 0x3ffe, 0x8000000000000000ull, 0x3fff, 0x8000000000000000ull},
            {"dm5",   uint16_t(0x3fff - 5), 0x9000000000000000ull, 0x3fff, 0x8000000000000000ull},
            // A difference of zero, where FPREM truncates the quotient to one
            // and FPREM1 rounds it to two.
            {"d0",    0x4000, 0xc000000000000000ull, 0x4000, 0x8000000000000000ull},
            // An ODD divisor near the tie. Comparing rem with B >> 1 drops
            // the divisor's low bit, so a remainder strictly below half reads
            // as a tie and FPREM1 rounds the quotient up where the host does
            // not. These are the cases that separate `2*rem vs B` from
            // `rem vs B/2`.
            {"odd-lo", 0x3fff, 0xc000000000000001ull, 0x3fff, 0x8000000000000001ull},
            {"odd-hi", 0x3fff, 0xc000000000000002ull, 0x3fff, 0x8000000000000001ull},
            {"odd-eq", 0x3fff, 0xc000000000000000ull, 0x3fff, 0x8000000000000001ull},
            {"odd-b",  0x4001, 0xffffffffffffffffull, 0x3fff, 0x8000000000000003ull},
        };
        for (unsigned one = 0; one < 2; ++one) {
            const uint8_t ops[2] = {0xd9, uint8_t(one ? 0xf5 : 0xf8)};
            const unsigned uop = (0x1u << 8) | (one ? 0xf5u : 0xf8u);
            for (const Chain &c : chains) {
                Unit u;
                std::array<uint8_t, 10> a{}, b{};
                std::memcpy(a.data(), &c.sa, 8);
                std::memcpy(a.data() + 8, &c.ea, 2);
                std::memcpy(b.data(), &c.sb, 8);
                std::memcpy(b.data() + 8, &c.eb, 2);
                int steps = 0, diffs = 0;
                bool partial_seen = false, complete_seen = false;
                std::string first;
                for (; steps < 40; ++steps) {
                    uint8_t stack[2][10];
                    std::memcpy(stack[0], a.data(), 10);
                    std::memcpy(stack[1], b.data(), 10);
                    Image want{}, got{};
                    if (!host_form(page, host_cw(0), stack, 2, nullptr, ops, 2, want)) break;
                    unit_form(u, host_cw(0), stack, 2, nullptr, uop, 0, got);
                    ++compared;
                    // The full mask: C2 says whether to loop and C0, C3 and
                    // C1 carry the quotient, so none of them may be masked.
                    if (!same_state(want, got, 0xffff)) {
                        if (!diffs)
                            first = "step " + std::to_string(steps) +
                                    " st0=" + hex80(stack[0]) + " st1=" + hex80(stack[1]) +
                                    " got " + state_str(got, 0xffff) +
                                    " want " + state_str(want, 0xffff);
                        ++diffs;
                        break;      // the chain has diverged; later steps say nothing
                    }
                    const bool c2 = (want.sw >> 10) & 1;
                    if (c2) partial_seen = true; else complete_seen = true;
                    if (!c2) break;
                    std::memcpy(a.data(), want.reg[0], 10);   // feed the step back
                }
                const std::string name = std::string(one ? "prem1-chain-" : "prem-chain-") + c.name;
                if (diffs) bad(name, std::to_string(diffs) + " " + first);
                else if (!complete_seen) bad(name, "the chain never cleared C2 in 40 steps");
                else ok(name + "-" + std::to_string(steps + 1) +
                        (partial_seen ? "-partial" : "-direct"));
            }
        }
    }

    // ---- QEMU's fprem test, as the plan names it. ----
    // qemu/tests/tcg/i386/test-i386-fprem.c runs FPREM and FPREM1 over a
    // systematic cross product of operand fields and prints each result and
    // status word; its own header says to "run this on real hardware, then
    // under QEMU, and diff the outputs". That is exactly what this bench
    // does, except that it compares the whole architectural state rather than
    // printed fields, so the test is ported as its operand grid rather than
    // run as a program.
    //
    // The grid is QEMU's own, field for field: the sign, nine exponents
    // including 0, 1, 2, the bias either side, 7FFDh, 7FFEh and 7FFFh, the
    // explicit integer bit either way, the quiet-NaN bit either way, and six
    // mantissas at both ends of the range. 432 values an operand.
    {
        StubPage page;
        const unsigned exps[] = {0, 1, 2, 0x3ffe, 0x3fff, 0x4000, 0x7ffd, 0x7ffe, 0x7fff};
        const uint64_t mants[] = {0, 1, 2,
                                  0x3ffffffffffffffdull, 0x3ffffffffffffffeull,
                                  0x3fffffffffffffffull};
        std::vector<std::array<uint8_t, 10>> grid;
        for (unsigned neg = 0; neg < 2; ++neg)
            for (unsigned e : exps)
                for (unsigned one = 0; one < 2; ++one)
                    for (unsigned qnan = 0; qnan < 2; ++qnan)
                        for (uint64_t mant : mants) {
                            // The fields as QEMU's union lays them out: the
                            // mantissa's top bit is the quiet-NaN flag and the
                            // one above it the explicit integer bit.
                            const uint64_t sig = mant | (uint64_t(qnan) << 62)
                                               | (uint64_t(one) << 63);
                            std::array<uint8_t, 10> b{};
                            std::memcpy(b.data(), &sig, 8);
                            const uint16_t se = uint16_t(e | (neg << 15));
                            std::memcpy(b.data() + 8, &se, 2);
                            grid.push_back(b);
                        }
        for (unsigned one = 0; one < 2; ++one) {
            const uint8_t ops[2] = {0xd9, uint8_t(one ? 0xf5 : 0xf8)};
            const unsigned uop = (0x1u << 8) | (one ? 0xf5u : 0xf8u);
            int diffs = 0, ran = 0;
            bool stub_failed = false;
            std::string first;
            Unit u;
            // Every A against every eighth B: the full 432 x 432 is 186,624
            // pairs an instruction and the stride keeps the run to a few
            // minutes while still crossing every field of A with every field
            // of B at least once.
            for (size_t ia = 0; ia < grid.size(); ++ia)
                for (size_t ib = ia % 8; ib < grid.size(); ib += 8) {
                    uint8_t stack[2][10];
                    std::memcpy(stack[0], grid[ia].data(), 10);
                    std::memcpy(stack[1], grid[ib].data(), 10);
                    Image want{}, got{};
                    if (!host_form(page, host_cw(0), stack, 2, nullptr, ops, 2, want)) {
                        // A stub page that cannot be made executable is a
                        // FAILURE, not a reason to stop: breaking out left
                        // `diffs` at zero and the row reported success having
                        // compared nothing. Codex found it on PR #1889.
                        stub_failed = true;
                        break;
                    }
                    unit_form(u, host_cw(0), stack, 2, nullptr, uop, 0, got);
                    ++ran;
                    ++compared;
                    if (!same_state(want, got, 0xffff)) {
                        if (!diffs)
                            first = "st0=" + hex80(stack[0]) + " st1=" + hex80(stack[1]) +
                                    " got " + state_str(got, 0xffff) +
                                    " want " + state_str(want, 0xffff);
                        ++diffs;
                    }
                }
            const std::string name = std::string("qemu-fprem") + (one ? "1" : "");
            if (stub_failed) bad(name, "no stub page: " + std::to_string(ran) + " of the grid ran");
            else if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(ran) + " " + first);
            else ok(name + "-" + std::to_string(ran));
        }
    }

    // ---- Precision control. ----
    // PC applies to add, subtract, multiply, divide and square root and to
    // nothing else (387 PRM section 2.3.2), so FRNDINT, FSCALE, FXTRACT and
    // the compares run here too: the point is that their results do not
    // change with it.
    {
        StubPage page;
        const uint16_t ARITH_SW = 0xfaff;
        struct PForm { const char *name; unsigned esc, modrm, pushes; uint16_t swmask; };
        const PForm pforms[] = {
            {"fadd",    0x0, 0xc1, 2, ARITH_SW}, {"fmul",   0x0, 0xc9, 2, ARITH_SW},
            {"fsub",    0x0, 0xe1, 2, ARITH_SW}, {"fsubr",  0x0, 0xe9, 2, ARITH_SW},
            {"fsub.p",  0x6, 0xe1, 2, ARITH_SW}, {"fcom",   0x0, 0xd1, 2, 0xffff},
            {"frndint", 0x1, 0xfc, 1, ARITH_SW}, {"fscale", 0x1, 0xfd, 2, ARITH_SW},
            {"fxtract", 0x1, 0xf4, 1, ARITH_SW},
            // Slice 4's: the divide rounds at PC like the multiply does, and
            // its iteration count is chosen by PC, so this row is what says
            // the early termination did not change a result.
            {"fdiv",    0x0, 0xf1, 2, ARITH_SW}, {"fdivr",  0x0, 0xf9, 2, ARITH_SW},
            {"fdiv.p",  0x6, 0xf9, 2, ARITH_SW},
            // FSQRT rounds at PC as well, and its iteration count is chosen
            // by PC, so this row says the early termination changed nothing.
            {"fsqrt",   0x1, 0xfa, 1, ARITH_SW},
            // The remainders ignore precision control entirely, so these rows
            // say their results do not move with it.
            {"fprem",   0x1, 0xf8, 2, 0xffff}, {"fprem1", 0x1, 0xf5, 2, 0xffff},
        };
        const size_t named = 22;
        // PC 01 is reserved. The unit reads it as the extended format; this
        // row is what says whether a real x87 agrees.
        for (unsigned pc = 0; pc < 4; ++pc) {
            const unsigned pcbits = pc == 0 ? 0 : pc == 1 ? 2 : pc == 2 ? 3 : 1;
            for (const PForm &f : pforms) {
                for (unsigned rc = 0; rc < 4; ++rc) {
                    const uint16_t cw = cw_of(rc, pcbits);
                    const uint8_t ops[2] = {uint8_t(0xd8 | f.esc), uint8_t(f.modrm)};
                    int diffs = 0, ran = 0;
                    std::string first;
                    Unit u;
                    for (size_t ia = 0; ia < named && ia < ext.size(); ++ia)
                        for (size_t ib = 0; ib < named && ib < ext.size(); ++ib) {
                            uint8_t stack[2][10];
                            std::memcpy(stack[0], ext[ia].data(), 10);
                            std::memcpy(stack[1], ext[ib].data(), 10);
                            Image want{}, got{};
                            if (!host_form(page, cw, stack, f.pushes, nullptr, ops, 2, want)) break;
                            unit_form(u, cw, stack, f.pushes, nullptr, (f.esc << 8) | f.modrm, 0, got);
                            ++ran;
                            ++compared;
                    if (!same_state(want, got, f.swmask)) {
                                if (!diffs)
                                    first = "st0=" + hex80(stack[0]) + " st1=" + hex80(stack[1]) +
                                            " got " + state_str(got, f.swmask) +
                                            " want " + state_str(want, f.swmask);
                                ++diffs;
                            }
                        }
                    const std::string name = std::string("pc") +
                                             (pc == 0 ? "24" : pc == 1 ? "53" : pc == 2 ? "64" : "01") +
                                             "-" + f.name + "-rc" + std::to_string(rc);
                    if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(ran) + " " + first);
                    else ok(name);
                }
            }
        }
    }

    // ---- The memory forms. ----
    // D8 and DC take a 32- or 64-bit real, DA and DE a 32- or 16-bit integer,
    // and reg fields 0 to 5 are add, multiply, compare, compare-and-pop,
    // subtract and reverse subtract. The operand goes through the same load
    // conversion as FLD and FILD, so a denormal real raises the denormal
    // exception before the arithmetic sees it.
    {
        StubPage page;
        const uint16_t ARITH_SW = 0xfaff;
        struct MForm { const char *name; unsigned esc, fmt, bytes; };
        const MForm mforms[] = {
            {"m32",  0x0, FMT_F32, 4}, {"m64", 0x4, FMT_F64, 8},
            {"m32i", 0x2, FMT_I32, 4}, {"m16i", 0x6, FMT_I16, 2},
        };
        // Reg fields 0 to 5 are slice 3's and 6 and 7 are slice 4's divides,
        // so the loop runs the whole field.
        const char *rfname[8] = {"add", "mul", "com", "comp", "sub", "subr", "div", "divr"};
        for (const MForm &m : mforms) {
            for (unsigned rf = 0; rf < 8; ++rf) {
                for (unsigned rc = 0; rc < 4; ++rc) {
                    const uint16_t cw = host_cw(rc);
                    // The host addresses the operand through %rcx, so its
                    // ModRM byte is mod 00 rm 001; the unit never looks at
                    // the rm field, and takes the same reg field.
                    const uint8_t ops[2] = {uint8_t(0xd8 | m.esc), uint8_t((rf << 3) | 0x01)};
                    const unsigned uop = (m.esc << 8) | (rf << 3) | 0x05;
                    const uint16_t swmask = (rf == 2 || rf == 3) ? 0xffff : ARITH_SW;
                    int diffs = 0, ran = 0;
                    std::string first;
                    Unit u;
                    for (size_t ia = 0; ia < 22 && ia < ext.size(); ++ia) {
                        // The memory operands: the patterns the load section
                        // uses for this format, then random bits.
                        std::vector<uint64_t> mem;
                        if (m.fmt == FMT_F32) for (uint32_t v : f32s) mem.push_back(v);
                        else if (m.fmt == FMT_F64) for (uint64_t v : f64s) mem.push_back(v);
                        else for (int64_t v : ints) mem.push_back(uint64_t(v));
                        for (int n = 0; n < 6; ++n) mem.push_back(rng());
                        for (uint64_t raw : mem) {
                            uint8_t stack[1][10];
                            std::memcpy(stack[0], ext[ia].data(), 10);
                            alignas(8) uint8_t mbytes[8] = {0};
                            std::memcpy(mbytes, &raw, m.bytes);
                            Image want{}, got{};
                            if (!host_form(page, cw, stack, 1, mbytes, ops, 2, want)) break;
                            unit_form(u, cw, stack, 1, mbytes, uop, m.fmt, got);
                            ++ran;
                            ++compared;
                            if (!same_state(want, got, swmask)) {
                                if (!diffs)
                                    first = "st0=" + hex80(stack[0]) + " mem=" + hex(raw, 2 * m.bytes) +
                                            " got " + state_str(got, swmask) +
                                            " want " + state_str(want, swmask);
                                ++diffs;
                            }
                        }
                    }
                    const std::string name = std::string("mem-") + rfname[rf] + "-" + m.name +
                                             "-rc" + std::to_string(rc);
                    if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(ran) + " " + first);
                    else ok(name);
                }
            }
        }
    }

    // ---- ST(i) for every i. ----
    // Eight distinct registers on the stack, so each ST(i) is its own value,
    // and the forms that name one are run for all eight. ST(0) as the other
    // operand is the case where the destination and the source are the same
    // register, and for the popping forms the one where the register written
    // is the one popped.
    {
        StubPage page;
        const uint16_t ARITH_SW = 0xfaff;
        struct IForm { const char *name; unsigned esc, base; uint16_t swmask; };
        const IForm iforms[] = {
            {"fadd.st0",  0x0, 0xc0, ARITH_SW}, {"fmul.st0",  0x0, 0xc8, ARITH_SW},
            {"fsub.st0",  0x0, 0xe0, ARITH_SW}, {"fsubr.st0", 0x0, 0xe8, ARITH_SW},
            {"fadd.sti",  0x4, 0xc0, ARITH_SW}, {"fsub.sti",  0x4, 0xe0, ARITH_SW},
            {"fsubr.sti", 0x4, 0xe8, ARITH_SW}, {"fmul.sti",  0x4, 0xc8, ARITH_SW},
            {"faddp",     0x6, 0xc0, ARITH_SW}, {"fsubp",     0x6, 0xe0, ARITH_SW},
            {"fsubrp",    0x6, 0xe8, ARITH_SW}, {"fmulp",     0x6, 0xc8, ARITH_SW},
            {"fcom",      0x0, 0xd0, 0xffff},   {"fcomp",     0x0, 0xd8, 0xffff},
            {"fucom",     0x5, 0xe0, 0xffff},   {"fucomp",    0x5, 0xe8, 0xffff},
        };
        // Eight values that are all different and none of them special, plus
        // a run with a NaN and a denormal among them.
        uint8_t stacks[2][8][10];
        for (unsigned which = 0; which < 2; ++which)
            for (unsigned n = 0; n < 8; ++n) {
                const uint64_t sig = 0x8000000000000000ull | (uint64_t(n + 1) << 40);
                const uint16_t se = uint16_t(0x3fff + n);
                std::memcpy(stacks[which][n], &sig, 8);
                std::memcpy(stacks[which][n] + 8, &se, 2);
            }
        {
            const uint64_t nan = 0xc000000000000000ull, den = 0x0000000000000003ull;
            const uint16_t snan = 0x7fff, zero = 0x0000;
            std::memcpy(stacks[1][3], &nan, 8);
            std::memcpy(stacks[1][3] + 8, &snan, 2);
            std::memcpy(stacks[1][5], &den, 8);
            std::memcpy(stacks[1][5] + 8, &zero, 2);
        }
        for (unsigned which = 0; which < 2; ++which) {
            for (const IForm &f : iforms) {
                int diffs = 0, ran = 0;
                std::string first;
                Unit u;
                for (unsigned i = 0; i < 8; ++i) {
                    const uint8_t ops[2] = {uint8_t(0xd8 | f.esc), uint8_t(f.base + i)};
                    Image want{}, got{};
                    if (!host_form(page, host_cw(0), stacks[which], 8, nullptr, ops, 2, want)) break;
                    unit_form(u, host_cw(0), stacks[which], 8, nullptr,
                              (f.esc << 8) | (f.base + i), 0, got);
                    ++ran;
                    ++compared;
                    if (!same_state(want, got, f.swmask)) {
                        if (!diffs)
                            first = "i=" + std::to_string(i) + " got " + state_str(got, f.swmask) +
                                    " want " + state_str(want, f.swmask);
                        ++diffs;
                    }
                }
                const std::string name = std::string("sti-") + f.name + (which ? "-mixed" : "");
                if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(ran) + " " + first);
                else ok(name);
            }
        }
    }



    // ---- FXTRACT's push, on a full stack. ----
    // The only push this slice makes. A full stack is a stack overflow: the
    // masked result is the real indefinite in both registers with C1 set,
    // which says overflow rather than underflow, and unmasked it leaves the
    // stack alone. Nothing else in the bench reaches those writes.
    {
        StubPage page;
        uint8_t full[8][10];
        for (unsigned n = 0; n < 8; ++n) {
            const uint64_t sig = 0x8000000000000000ull | (uint64_t(n + 1) << 40);
            const uint16_t se = uint16_t(0x4000 + n);
            std::memcpy(full[n], &sig, 8);
            std::memcpy(full[n] + 8, &se, 2);
        }
        const uint8_t ops[2] = {0xd9, 0xf4};
        for (unsigned which = 0; which < 2; ++which) {
            const uint16_t cw = uint16_t(which ? (host_cw(0) & ~0x0001) : host_cw(0));
            Image want{}, got{};
            Unit u;
            bool ok_stub = host_form(page, cw, full, 8, nullptr, ops, 2, want);
            unit_form(u, cw, full, 8, nullptr, (1u << 8) | 0xf4, 0, got);
            const std::string name = std::string("xtract-overflow-") + (which ? "unmasked" : "masked");
            if (!ok_stub) bad(name, "no stub page");
            else if (!same_state(want, got, 0xfaff))
                bad(name, "got " + state_str(got, 0xfaff) + " want " + state_str(want, 0xfaff));
            else ok(name);
        }
    }

    // ---- The exception edges, masked and unmasked. ----
    // The operand pairs the grid above never reaches: an overflowing product,
    // an underflow into the denormals and past them, the tininess boundary,
    // and an inexact result. Each runs with everything masked and then with
    // one exception unmasked at a time, and the host settles what a real x87
    // does -- which is not the same for the two halves of the exception set.
    // Invalid, denormal and divide-by-zero are detected before the result
    // exists, so unmasked they leave the operands and TOP alone; overflow and
    // underflow are detected after it, and unmasked they store the result
    // with its exponent moved by 24576; precision never holds a result back.
    {
        StubPage page;
        const uint16_t ARITH_SW = 0xfaff;
        auto pat = [](uint16_t se, uint64_t sig) {
            std::array<uint8_t, 10> b{};
            std::memcpy(b.data(), &sig, 8);
            std::memcpy(b.data() + 8, &se, 2);
            return b;
        };
        struct Edge {
            const char *name;
            std::array<uint8_t, 10> a, b;
        };
        const std::array<uint8_t, 10> one = pat(0x3fff, 0x8000000000000000ull);
        const std::vector<Edge> edges = {
            // 2^9000 squared, which overflows the extended format.
            {"overflow", pat(0x3fff + 9000, 0x8000000000000000ull), pat(0x3fff + 9000, 0x8000000000000000ull)},
            // 2^-8200 squared: tiny, and exactly a denormal, so nothing is
            // lost and a real x87 raises nothing at all when it is masked.
            {"tiny-exact", pat(0x3fff - 8200, 0x8000000000000000ull), pat(0x3fff - 8200, 0x8000000000000000ull)},
            // 2^-8300 squared, which is past the smallest denormal.
            {"tiny-gone", pat(0x3fff - 8300, 0x8000000000000000ull), pat(0x3fff - 8300, 0x8000000000000000ull)},
            // The largest denormal times just over one: tiny, inexact, and
            // on the edge of rounding up to the smallest normal, which is
            // where tininess before and after rounding part company.
            {"tiny-edge", pat(0x0000, 0x7fffffffffffffffull), pat(0x3fff, 0x8000000000000002ull)},
            {"tiny-edge2", pat(0x0001, 0x8000000000000000ull), pat(0x3ffe, 0xfffffffffffffffeull)},
            {"tiny-edge3", pat(0x0001, 0x8000000000000000ull), pat(0x3ffe, 0xffffffffffffffffull)},
            // An inexact sum and an inexact product.
            {"inexact-add", one, pat(0x3fff - 70, 0x8000000000000000ull)},
            {"inexact-mul", pat(0x3fff, 0xb504f333f9de6484ull), pat(0x3fff, 0xb504f333f9de6484ull)},
            // An overflow that is also inexact, which is where OE and PE
            // could disagree about who reports.
            {"overflow-inexact", pat(0x3fff + 9000, 0xb504f333f9de6484ull),
                                 pat(0x3fff + 9000, 0xb504f333f9de6484ull)},
            // A subtract that cancels almost everything, and one that
            // cancels exactly.
            {"cancel", pat(0x4000, 0x8000000000000000ull), pat(0x3fff, 0xffffffffffffffffull)},
            {"cancel-exact", one, one},
            // A denormal operand, which raises the denormal exception.
            {"denormal-op", pat(0x0000, 0x4000000000000000ull), one},
            // Two values an exponent apart, where the alignment shift drops
            // the guard bit and nothing else.
            {"align1", pat(0x4000, 0x8000000000000000ull), pat(0x3fff, 0x8000000000000001ull)},
            // And the shifts around the point where the round bit is the
            // operand's own top bit: 64, 65, 66 and 67 places.
            {"align64", pat(0x3fff + 64, 0x8000000000000000ull), one},
            {"align65", pat(0x3fff + 65, 0x8000000000000000ull), one},
            {"align66", pat(0x3fff + 66, 0x8000000000000000ull), one},
            {"align67", pat(0x3fff + 67, 0x8000000000000000ull), one},
            {"align64b", pat(0x3fff + 64, 0x8000000000000000ull), pat(0x3fff, 0xffffffffffffffffull)},
            {"align65b", pat(0x3fff + 65, 0x8000000000000000ull), pat(0x3fff, 0xffffffffffffffffull)},
            {"align66b", pat(0x3fff + 66, 0x8000000000000000ull), pat(0x3fff, 0xffffffffffffffffull)},
            // An empty ST(1), which is a stack underflow.
            {"empty", one, pat(0, 0)},
            // More of the tininess boundary, where the rounding's own carry
            // decides whether the result is tiny at all. The first is one
            // step above the smallest normal times (1 - 2^-63) and the
            // second is a product whose PC 24 rounding carries it back into
            // range; between them they separate tininess before the
            // rounding from tininess after it.
            {"tiny-edge4", pat(0x0001, 0x8000000000000001ull), pat(0x3ffe, 0xfffffffffffffffeull)},
            {"tiny-edge5", pat(0x0001, 0x8000000000000000ull), pat(0x3ffe, 0xffffff0000000000ull)},
            {"tiny-edge6", pat(0x0001, 0x8000000000000001ull), pat(0x3ffe, 0xffffffffffffffffull)},
            {"tiny-edge7", pat(0x0002, 0x8000000000000000ull), pat(0x3ffe, 0xffffffffffffffffull)},
            {"tiny-edge8", pat(0x0000, 0x0000000000000001ull), pat(0x3fff, 0x8000000000000001ull)},
            // Two quiet NaNs with different payloads, with equal payloads
            // and opposite signs, and a signalling pair: the 387 delivers
            // the one with the larger significand, and the tie has to go
            // somewhere. `a_rev` swaps the two operands, so the reversed
            // subtract runs each pair the other way round as well.
            {"qnan-pair", pat(0x7fff, 0xc000000000000001ull), pat(0x7fff, 0xc000000000000002ull)},
            {"qnan-tie", pat(0x7fff, 0xc000000000000003ull), pat(0xffff, 0xc000000000000003ull)},
            {"qnan-tie2", pat(0xffff, 0xc000000000000003ull), pat(0x7fff, 0xc000000000000003ull)},
            {"snan-pair", pat(0x7fff, 0xa000000000000001ull), pat(0x7fff, 0xa000000000000002ull)},
            {"snan-qnan", pat(0x7fff, 0xa000000000000009ull), pat(0x7fff, 0xc000000000000001ull)},
            {"qnan-snan", pat(0x7fff, 0xc000000000000001ull), pat(0x7fff, 0xa000000000000009ull)},
            {"snan-tie", pat(0x7fff, 0xa000000000000003ull), pat(0xffff, 0xa000000000000003ull)},
        };
        struct Mask { const char *name; uint16_t clear; };
        const Mask masks[] = {
            {"masked", 0x0000}, {"im", 0x0001}, {"dm", 0x0002}, {"zm", 0x0004},
            {"om", 0x0008},     {"um", 0x0010}, {"pm", 0x0020},
        };
        struct EForm { const char *name; unsigned esc, modrm, pushes; uint16_t swmask; };
        const EForm eforms[] = {
            {"fadd",  0x0, 0xc1, 2, ARITH_SW}, {"fmul",   0x0, 0xc9, 2, ARITH_SW},
            {"fsub",  0x0, 0xe1, 2, ARITH_SW}, {"fsubr",  0x0, 0xe9, 2, ARITH_SW},
            {"fmulp", 0x6, 0xc9, 2, ARITH_SW}, {"fscale", 0x1, 0xfd, 2, ARITH_SW},
            {"fadd.sti", 0x4, 0xc1, 2, ARITH_SW}, {"fsubr.sti", 0x4, 0xe9, 2, ARITH_SW},
            {"frndint", 0x1, 0xfc, 1, ARITH_SW}, {"fxtract", 0x1, 0xf4, 1, ARITH_SW},
            {"fcom",  0x0, 0xd1, 2, 0xffff}, {"fcompp", 0x6, 0xd9, 2, 0xffff},
            {"fucomp", 0x5, 0xe9, 2, 0xffff},
            // Slice 4's, under every mask setting. The divide is the only
            // form in this unit that can raise ZE, and unmasked ZE on the
            // commit edge is the hazard this slice's brief names, so these
            // rows are what exercise it.
            //
            // **C1 means something different in the remainder rows.** For the
            // arithmetic and the divide it is the rounding's direction and
            // comes from `pc1`; for FPREM and FPREM1 it is the QUOTIENT'S BIT
            // 0. Both reach the status word through `pc1`, because S_APPLY
            // writes the codes as {nc[3], nc[2], c1, nc[0]}. A row copied
            // from a divide row and read as a rounding direction is wrong.
            {"fdiv",   0x0, 0xf1, 2, ARITH_SW}, {"fdivr", 0x0, 0xf9, 2, ARITH_SW},
            {"fdiv.sti", 0x4, 0xf9, 2, ARITH_SW}, {"fdivp", 0x6, 0xf9, 2, ARITH_SW},
            {"fsqrt",  0x1, 0xfa, 1, ARITH_SW},
            {"fprem",  0x1, 0xf8, 2, 0xffff}, {"fprem1", 0x1, 0xf5, 2, 0xffff},
        };
        for (const Mask &m : masks) {
            for (unsigned rc = 0; rc < 4; ++rc) {
                for (unsigned pc = 0; pc < 3; ++pc) {
                    const unsigned pcbits = pc == 0 ? 0 : pc == 1 ? 2 : 3;
                    const uint16_t cw = uint16_t(cw_of(rc, pcbits) & ~m.clear);
                    int diffs = 0, ran = 0;
                    std::string first;
                    Unit u;
                    for (const EForm &f : eforms) {
                        const uint8_t ops[2] = {uint8_t(0xd8 | f.esc), uint8_t(f.modrm)};
                        for (const Edge &e : edges) {
                            // Both operand orders, so each form sees the edge
                            // as its destination and as its source.
                            for (unsigned flip = 0; flip < 2; ++flip) {
                                uint8_t stack[2][10];
                                std::memcpy(stack[0], (flip ? e.b : e.a).data(), 10);
                                std::memcpy(stack[1], (flip ? e.a : e.b).data(), 10);
                                Image want{}, got{};
                                if (!host_form(page, cw, stack, f.pushes, nullptr, ops, 2, want)) break;
                                unit_form(u, cw, stack, f.pushes, nullptr,
                                          (f.esc << 8) | f.modrm, 0, got);
                                ++ran;
                                ++compared;
                    if (!same_state(want, got, f.swmask)) {
                                    if (!diffs)
                                        first = std::string(f.name) + " " + e.name +
                                                (flip ? " flipped" : "") +
                                                " st0=" + hex80(stack[0]) + " st1=" + hex80(stack[1]) +
                                                " got " + state_str(got, f.swmask) +
                                                " want " + state_str(want, f.swmask);
                                    ++diffs;
                                }
                            }
                        }
                    }
                    const std::string name = std::string("edge-") + m.name + "-rc" + std::to_string(rc) +
                                             "-pc" + (pc == 0 ? "24" : pc == 1 ? "53" : "64");
                    if (diffs) bad(name, std::to_string(diffs) + " of " + std::to_string(ran) + " " + first);
                    else ok(name);
                }
            }
        }
    }

    // ---- The arithmetic's own ERROR# and BUSY# latch. ----
    // The flags an operation raises and the board's BUSY# latch have to go up
    // in the same cycle: the apply that writes the flags is the cycle the
    // unit goes idle, so a latch driven from an edge of the status word is a
    // cycle late and leaves an ESC free to run. This is the shape of all
    // three faults slice 2 had, so it is checked here for each exception the
    // arithmetic raises -- including the ones that do not hold the result
    // back, where `um` is false and the latch still has to rise.
    {
        struct Trial { const char *name; uint16_t cw; uint16_t se_a; uint64_t sig_a; uint16_t se_b; uint64_t sig_b;
                       unsigned esc, modrm; };
        const Trial trials[] = {
            // invalid: 0 times infinity, with IM clear
            {"ie", 0x037e, 0x0000, 0, 0x7fff, 0x8000000000000000ull, 0x0, 0xc9},
            // denormal operand, with DM clear
            {"de", 0x037d, 0x0000, 0x4000000000000000ull, 0x3fff, 0x8000000000000000ull, 0x0, 0xc1},
            // overflow, with OM clear
            {"oe", 0x0377, uint16_t(0x3fff + 9000), 0x8000000000000000ull,
                           uint16_t(0x3fff + 9000), 0x8000000000000000ull, 0x0, 0xc9},
            // underflow, with UM clear
            {"ue", 0x036f, uint16_t(0x3fff - 8300), 0x8000000000000000ull,
                           uint16_t(0x3fff - 8300), 0x8000000000000000ull, 0x0, 0xc9},
            // precision, with PM clear: the result is written and the latch
            // still rises, which is the case `um` alone would miss.
            {"pe", 0x035f, 0x3fff, 0x8000000000000000ull,
                           uint16_t(0x3fff - 70), 0x8000000000000000ull, 0x0, 0xc1},
            // divide-by-zero, which only FXTRACT raised before slice 4
            {"ze", 0x037b, 0x0000, 0, 0x0000, 0, 0x1, 0xf4},
            // Slice 4's. ZE from a real divide is the exception this slice
            // adds and the one its brief names: 1 / 0 with ZM clear, where
            // divide-by-zero is a PRE-operation exception, so the operands
            // and TOP are left alone and the latch still has to rise on the
            // commit edge.
            {"ze-div", 0x037b, 0x3fff, 0x8000000000000000ull, 0x0000, 0, 0x0, 0xf1},
            // invalid from a divide: 0 / 0 with IM clear.
            {"ie-div", 0x037e, 0x0000, 0, 0x0000, 0, 0x0, 0xf1},
            // invalid from a square root: the root of a negative, IM clear.
            {"ie-sqrt", 0x037e, 0xbfff, 0x8000000000000000ull, 0x3fff,
                        0x8000000000000000ull, 0x1, 0xfa},
            // precision from a divide, PM clear: the result is written and
            // the latch still rises, the case `um` alone would miss.
            {"pe-div", 0x035f, 0x3fff, 0x8000000000000000ull, 0x4000,
                       0xc000000000000000ull, 0x0, 0xf1},
            // invalid from a remainder: an infinite dividend, IM clear.
            {"ie-prem", 0x037e, 0x7fff, 0x8000000000000000ull, 0x4000,
                        0x8000000000000000ull, 0x1, 0xf8},
        };
        for (const Trial &t : trials) {
            Unit u;
            u.clear_latch();
            u.reg_form(OP_FNINIT);
            uint8_t a[10] = {0}, b[10] = {0};
            std::memcpy(a, &t.sig_a, 8);
            std::memcpy(a + 8, &t.se_a, 2);
            std::memcpy(b, &t.sig_b, 8);
            std::memcpy(b + 8, &t.se_b, 2);
            u.present(OP_FLDCW, FMT_I16, true, true, 0);
            u.commit(0, t.cw, true);
            u.settle();
            u.load(OP_FLD_M80, FMT_F80, b);
            u.load(OP_FLD_M80, FMT_F80, a);
            u.clear_latch();            // so the latch below is this form's
            // The form, and then the first cycle the unit is idle.
            u.present((t.esc << 8) | t.modrm, 0, true, true, 0);
            u.commit(0, 0, true);
            bool idle_seen = false, gap = false, flagged = false;
            for (int n = 0; n < 256 && !idle_seen; ++n) {
                if (!u.top.busy_o) {
                    idle_seen = true;
                    flagged = u.top.error != 0;
                    u.top.iss = 1;
                    u.top.iss_wait = 0;
                    u.top.iss_op = OP_FCHS;
                    u.top.iss_fmt = 0;
                    u.top.iss_first = 1;
                    u.top.iss_last = 1;
                    u.top.iss_part = 0;
                    u.top.eval();
                    gap = !u.top.hold;
                    u.top.iss = u.top.iss_first = u.top.iss_last = 0;
                    u.top.eval();
                } else {
                    u.tick();
                }
            }
            const std::string name = std::string("arith-latch-") + t.name;
            if (!idle_seen) bad(name, "the unit never went idle");
            else if (!flagged) bad(name, "ERROR# was not up the cycle the unit went idle, sw=" + hex(u.sw(), 4));
            else if (gap) bad(name, "an ESC was free the cycle the unit went idle, sw=" + hex(u.sw(), 4));
            else ok(name);
            u.reg_form(OP_FNINIT);
            u.clear_latch();
        }
    }

    if (!testfloat_manifest.empty()) {
        const TestFloatManifest testfloat = run_testfloat_manifest(testfloat_manifest);
        std::printf("TESTFLOAT %u %u\n", testfloat.vectors, testfloat.configs);
    }
    // A bench that compared nothing is not a bench that passed. Both of
    // these are failures of the harness rather than of the unit, and both
    // would otherwise be invisible: the run already printed COMPARED 0 and
    // nothing gated on it.
    if (stub_failures)
        bad("protocol.stub", std::to_string(stub_failures)
            + " host stubs were unavailable, so that many comparisons never ran");
    if (compared == 0)
        bad("protocol.compared", "no host comparison ran at all");
    if (held_for_ever) bad("protocol.hold", std::to_string(held_for_ever) + " uops held past the bench's wait");
    else ok("protocol.hold");
    std::printf("COMPARED %ld\n", compared);
    // ---- Per-form latency, in CPU clocks ----
    // Measured, not inferred. The claim this checks is that the DE25
    // backend's pipeline cannot be amortised by a consumer that holds one ESC
    // instruction in flight, so its DEPENDENT latency is what matters and may
    // be worse than the sequencer's. Which forms it is worse on, and by how
    // much, is a measurement: the first guess -- that more ESCs in flight
    // would be the remedy -- is only sound if the adds and multiplies
    // dominate, and x87 code chains through ST(0), so it might instead be the
    // divide and square root, where 76 clocks of digit recurrence meet
    // Goldschmidt.
    //
    // The number is clocks from the instruction's commit to `busy_o` falling,
    // which is what the next ESC waits for.
    {
        struct LForm { const char *name; unsigned op; };
        const LForm forms[] = {
            {"fadd",  0x0c1}, {"fsub",  0x0e1}, {"fmul", 0x0c9}, {"fdiv", 0x0f1},
            {"fsqrt", 0x1fa}, {"fscale", 0x1fd}, {"frndint", 0x1fc}, {"fprem", 0x1f8},
        };
        const uint8_t two[10]  = {0,0,0,0,0,0,0,0x80,0x00,0x40};
        const uint8_t three[10] = {0,0,0,0,0,0,0,0xc0,0x00,0x40};
        Unit u;
        // Over all three precisions, because the divide and the square root
        // are precision-dependent on BOTH sides and in different ways: the
        // backend's digit recurrence stops at 25, 54 or 65 clocks by
        // precision, while the sequencer's Goldschmidt drops an iteration
        // only at PC24. A PC64-only table would have made "the divide gap is
        // algorithmic" look larger and more uniform than it is.
        for (unsigned pc : {0u, 2u, 3u}) {
            const unsigned bits = pc == 0 ? 24 : pc == 2 ? 53 : 64;
            for (const LForm &f : forms) {
                u.clear_latch();
                u.reg_form(OP_FNINIT);
                u.present(OP_FLDCW, FMT_I16, true, true, 0);
                u.commit(0, cw_of(0, pc), true);
                u.settle();
                u.load(OP_FLD_M80, FMT_F80, two);
                u.load(OP_FLD_M80, FMT_F80, three);
                u.present(f.op, 0, true, true, 0);
                u.commit(0, 0, true);
                int clocks = 0;
                while (u.top.busy_o && clocks < 4000) { u.tick(); ++clocks; }
                std::printf("LATENCY %s pc%u %d\n", f.name, bits, clocks);
            }
        }
    }
    std::printf("SUMMARY %d %d\n", passed, failed);
    return failed ? 1 : 0;
}
