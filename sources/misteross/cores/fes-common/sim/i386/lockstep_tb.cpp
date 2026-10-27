// SPDX-License-Identifier: MIT
// Whole-CPU retirement lockstep: fes_i386_cpu beside the Phase 0 oracle.
//
// The oracle steps one instruction; the RTL runs until it retires one, and the
// two architectural states are compared: EIP, the GPRs, EFLAGS bits 0-17 and
// the six segment registers (selector, base, limit, attributes). Memory writes
// are compared as each side reports them: the bytes each instruction writes
// must be the same bytes, with the same values, on both sides. So are I/O
// accesses: each instruction's, in order, with their ports, lengths and data,
// on an I/O space each side keeps (IoSpace). An instruction
// that faults in the oracle must fault in the RTL with the same vector, with
// nothing of it retired; both deliver it, and the delivery, its frame
// included, is compared as a retirement. A fault while an exception is
// delivered (a double fault, then shutdown) stops the RTL as not built,
// with nothing written. HLT ends a run on both sides.
//
// Where the oracle and SingleStepTests/80386 disagree, divergences.toml names
// the side the FES CPU follows. For an entry that follows the corpus, the
// fields it names are left out of the comparison with the oracle at that
// retirement, and the oracle takes the RTL's values so both run on from the
// same state; the corpus itself checks those values (below). The runner
// writes the opcode-class entries to FES_I386_DIVERGENCES, one per line:
//   <id> <corpus|oracle> <stem,...|*> <predicate,...|-> <field,...>
//
// The memory model delays each fetch block, each load's bytes and each I/O
// access's completion by a random 0-12 cycles when FES_I386_LATENCY=1 (the
// default); latency must never change a result. A load's bytes are memory as it was on the edge M1
// sent the address, read before the store retiring on that edge writes.
//
// Modes (FES_I386_MODE):
//   corpus  SingleStepTests/80386 states on stdin, one per line, in the format
//           of vectors_tb.cpp followed by two lists the runner works out from
//           the label-scoped entries: <n> <field>... compared with the corpus
//           only (follow corpus), <n> <field>... with the oracle only (follow
//           oracle). A state that faults, or takes a software interrupt, runs
//           on into its handler, to its HLT. After the run the RTL's final
//           state must equal the corpus's final state, undefined flags and
//           the pushed frame included.
//   random  FES_I386_PROGRAMS random programs from FES_I386_SEED: register,
//           immediate and 16-bit-addressed memory forms of the slice's
//           instructions with prefixes, forward branches, bounded LOOPs,
//           backward jumps, pushes and pops, CALLs to subroutines that RET
//           and PUSH BP / MOV BP, SP / LEAVE frames, IN, OUT, INS and OUTS
//           on a few ports, so results forward between instructions and
//           taken branches kill younger producers. Each port holds a byte
//           (IoSpace); in corpus mode every port reads all ones. INTR and NMI
//           are injected (Irq), and a HLT before the program's last wakes.
//           FES_I386_FIRST skips the programs before it (they are still
//           generated), to run one again.
//           A run stops at its last HLT, once it has delivered an exception,
//           after FES_I386_RETIRE_LIMIT retirements, at a store into its own
//           code, which needs self-modifying-code detection the RTL does not
//           have yet, or at an interrupt through a vector entry it rewrote.
//   unbuilt encodings this build does not execute, one per line on stdin as
//           hex bytes: each must stop the RTL with FAULT_UNIMPLEMENTED before
//           anything retires. The RTL runs alone; the oracle is not stepped.
//   reset-store a store waiting in WB must not write memory on a reset edge.
//   directed  out-of-limit CALL rel and r/m, RET and JMP faults, and divide errors, delivered to a handler that
//             HLTs, then a check that fetch, issue and data reads stay inactive
//             after the HLT; and an INT and a fault whose delivery faults,
//             which must stop the RTL with nothing written.
// Output: "FAIL <label> <field> <expected> <actual>..." per failing run, then
// one JSON summary line. Exit status 1 on any failure.
#include "Vfes_i386_cpu.h"
#include "i386_oracle.h"
#include "verilated.h"

#include <algorithm>
#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <map>
#include <memory>
#include <random>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

using fes_i386::State;

constexpr uint32_t RAM_BYTES = 16u << 20;
constexpr uint32_t EFLAGS_MASK = 0x0003ffff;
constexpr int EVENT_CYCLES = 4000;  // an RTL retirement or fault must come within this many cycles
                                    // (a REP string of 63 elements with the data port's latency)

std::string hex(uint64_t value) {
    char text[24];
    std::snprintf(text, sizeof text, "%llx", (unsigned long long)value);
    return text;
}

// ---- Buses -------------------------------------------------------------------

// One I/O access: a write (OUT) or a read (IN), its port, its length in
// bytes and its data.
struct IoAccess {
    bool write = false;
    uint16_t port = 0;
    unsigned length = 0;
    uint32_t data = 0;
    bool operator==(const IoAccess &o) const {
        return write == o.write && port == o.port && length == o.length && data == o.data;
    }
    bool operator!=(const IoAccess &o) const { return !(*this == o); }
    uint64_t key() const { return uint64_t(write) << 60 | uint64_t(length) << 52 | uint64_t(port) << 32 | data; }
};

// An I/O space, one per side. Open (corpus mode), every port reads all ones
// and ignores writes, as the corpus's test bus did. Otherwise each port holds
// a byte, which an OUT writes and an IN reads and then increments, so a
// repeated IN reads a new value and an OUT's data reaches the INs after it. A
// word or dword access is its bytes at port, port + 1, and on, wrapping at
// 64K. Each side logs its accesses, and the lockstep compares each
// instruction's, in order.
struct IoSpace {
    bool open = true;
    std::vector<uint8_t> port = std::vector<uint8_t>(65536);
    std::vector<IoAccess> log;
    void reset(bool open_bus) {
        open = open_bus;
        for (uint32_t i = 0; i < 65536; ++i) port[i] = uint8_t(i * 37 + (i >> 8));
        log.clear();
    }
    uint32_t in(uint16_t at, unsigned length) {
        uint32_t value = 0;
        for (unsigned i = 0; i < length; ++i) {
            uint8_t &b = port[uint16_t(at + i)];
            value |= uint32_t(open ? 0xff : b) << (8 * i);
            if (!open) ++b;
        }
        log.push_back({false, at, length, value});
        return value;
    }
    void out(uint16_t at, unsigned length, uint32_t value) {
        if (length < 4) value &= (1u << (8 * length)) - 1;
        if (!open)
            for (unsigned i = 0; i < length; ++i) port[uint16_t(at + i)] = uint8_t(value >> (8 * i));
        log.push_back({true, at, length, value});
    }
};

// The machine's interrupt lines, one set per side: INTR, with the vector the
// PIC returns on INTA, which drops INTR; and NMI, which the RTL sees as a
// one-cycle pulse. acks counts INTAs, unexpected those with INTR low.
struct Pic {
    bool intr = false, nmi = false;
    uint8_t vector = 0;
    uint64_t acks = 0, unexpected = 0;
    uint8_t acknowledge() {
        unexpected += !intr;
        intr = false;
        ++acks;
        return vector;
    }
};

// The oracle's memory: 16 MiB of RAM, its I/O space and its PIC.
class OracleBus final : public fes_i386::Bus {
public:
    fes_i386::PageMemory ram{RAM_BYTES};
    IoSpace io;
    Pic pic;
    uint8_t *page(uint32_t paddr, bool) override { return paddr < RAM_BYTES ? &ram[paddr & ~0xfffu] : nullptr; }
    void read(uint32_t, unsigned length, uint8_t *data) override { std::memset(data, 0xff, length); }
    void write(uint32_t, unsigned, const uint8_t *) override {}
    uint32_t in(uint16_t port, unsigned length) override { return io.in(port, length); }
    void out(uint16_t port, unsigned length, uint32_t value) override { io.out(port, length, value); }
    uint8_t interrupt_acknowledge() override { return pic.acknowledge(); }
};

// ---- The RTL and its memory model ------------------------------------------------

// A memory port with random latency: a request made on one edge completes 0-12
// cycles later. The fetch, data and I/O ports take one each.
struct Latency {
    std::mt19937_64 *random = nullptr;
    unsigned waiting = 0;
    void request() { waiting = random && (*random)() % 2 ? unsigned((*random)() % 12) + 1 : 0; }
    void tick() {
        if (waiting) --waiting;
    }
    bool ready() const { return waiting == 0; }
};

class Rtl {
public:
    explicit Rtl(VerilatedContext &context) : top(&context), ram(RAM_BYTES, 0) {}

    Vfes_i386_cpu top;
    std::vector<uint8_t> ram;
    IoSpace io;
    Pic pic;
    Latency fetch, data, port, inta;
    uint64_t cycles = 0;
    uint64_t load_waits = 0;  // cycles a load's bytes were late
    uint64_t md_waits = 0;    // cycles D to WB waited for a multiply or divide, since the last retirement
    std::set<uint32_t> step_writes;  // bytes written since the last next()
    std::set<uint32_t> written;      // bytes either side wrote since the run began, to clear after it
    uint64_t io_unstable = 0;        // I/O requests whose port, size or data changed before their ack

    // Reset with this architectural state.
    void start(const State &s) {
        for (int n = 0; n < 8; ++n) top.init_gpr[n] = s.gpr[n];
        top.init_eflags = s.eflags;
        top.init_eip = s.eip;
        for (int n = 0; n < 6; ++n) {
            set16(top.init_sel, n, s.seg[n].selector);
            top.init_base[n] = s.seg[n].base;
            top.init_limit[n] = s.seg[n].limit;
            set16(top.init_ar, n, s.seg[n].ar);
        }
        fetch.waiting = 0;
        data.waiting = 0;
        port.waiting = 0;
        io_waiting = false;
        top.intr = 0;
        top.nmi = 0;
        top.inta_ack = 0;
        top.mem_valid = 1;
        top.dmem_valid = 1;
        top.io_ack = 0;
        step_writes.clear();
        io.log.clear();
        top.reset = 1;
        for (int n = 0; n < 3; ++n) clock();
        top.reset = 0;
        // The lines and INTA from after the reset: the last run may have left
        // an INTA pending, which the reset's edges answer.
        inta.waiting = 0;
        inta_waiting = false;
        pic = Pic{};
    }

    void clock() {
        top.clk = 0;
        top.eval();
        const bool load = top.mem_load;
        const uint32_t block = top.mem_block;
        const bool dread = top.dmem_read, dwrite = top.dmem_write;
        const uint32_t daddr = top.dmem_addr & ~3u, waddr = top.dmem_waddr & ~3u;
        const uint64_t wdata = top.dmem_wdata;
        const uint8_t wmask = top.dmem_wmask;
        md_waits += top.trace_md_wait;  // this cycle, before the edge that ends it
        // The I/O port: a request completes after its latency, counted from
        // the cycle it rises, the access made on this side's I/O space then,
        // with io_ack (and IN's data) on this edge. The request must hold its
        // port, size and data until then. Otherwise io_rdata is garbage.
        top.io_ack = 0;
        top.io_rdata = uint32_t(cycles * 0x9e3779b9u);
        if (top.io_req) {
            const IoAccess now{bool(top.io_write), uint16_t(top.io_port), 1u << top.io_size, top.io_wdata};
            if (!io_waiting) {
                io_waiting = true;
                io_request = now;
                port.request();
            } else {
                port.tick();
                if (now != io_request) ++io_unstable;
            }
            if (port.ready()) {
                io_waiting = false;
                if (now.write) io.out(now.port, now.length, now.data);
                else top.io_rdata = io.in(now.port, now.length);
                top.io_ack = 1;
            }
        } else {
            io_waiting = false;
        }
        // The interrupt lines, and INTA as the I/O port completes an access:
        // the PIC returns its vector, and drops INTR, on the acknowledging
        // edge.
        top.intr = pic.intr;
        top.nmi = pic.nmi;
        pic.nmi = false;  // a pulse of one cycle, so the next is an edge too
        top.inta_ack = 0;
        top.inta_vector = uint8_t(cycles * 0x9du);
        if (top.inta_req) {
            if (!inta_waiting) {
                inta_waiting = true;
                inta.request();
            } else {
                inta.tick();
            }
            if (inta.ready()) {
                inta_waiting = false;
                top.inta_vector = pic.acknowledge();
                top.inta_ack = 1;
            }
        } else {
            inta_waiting = false;
        }
        top.clk = 1;
        top.eval();
        // The data port reads, then the retiring store writes. Until a load's
        // bytes arrive the port carries garbage.
        if (dread) {
            uint64_t value = 0;
            for (int i = 0; i < 8; ++i) {
                const uint64_t at = uint64_t(daddr) + i;
                value |= uint64_t(at < RAM_BYTES ? ram[at] : 0xff) << (8 * i);
            }
            loaded = value;
            data.request();
            if (std::getenv("FES_I386_VERBOSE"))
                std::fprintf(stderr, "  read %x -> %016llx\n", daddr, (unsigned long long)value);
        }
        else {
            data.tick();
        }
        top.dmem_valid = data.ready();
        load_waits += !data.ready();
        top.dmem_rdata = data.ready() ? loaded : (uint64_t((*data.random)()) << 32 | uint32_t((*data.random)()));
        if (dwrite && std::getenv("FES_I386_VERBOSE"))
            std::fprintf(stderr, "  write %x mask %02x data %016llx\n", waddr, wmask, (unsigned long long)wdata);
        if (dwrite) {
            for (int i = 0; i < 8; ++i) {
                if (!(wmask >> i & 1)) continue;
                const uint32_t at = waddr + i;
                if (at < RAM_BYTES) ram[at] = uint8_t(wdata >> (8 * i));
                step_writes.insert(at);
                written.insert(at);
            }
        }
        if (load) {
            address = block;
            fetch.request();
        } else {
            fetch.tick();
        }
        top.mem_valid = fetch.ready();
        // Until the block arrives the port carries garbage.
        for (int word = 0; word < 4; ++word) {
            uint32_t value = 0;
            for (int i = 0; i < 4; ++i) {
                uint64_t at = (uint64_t(address) << 4) + 4 * word + i;
                value |= uint32_t(at < RAM_BYTES ? ram[at] : 0xff) << (8 * i);
            }
            top.mem_data[word] = fetch.ready() ? value : uint32_t((*fetch.random)());
        }
        ++cycles;
    }

    enum class Event { Retired, Unimplemented, Timeout };

    // Run until the next retirement, an exception's or interrupt's delivery
    // included (trace_exception), or stop. In HLT the RTL runs on only while
    // an interrupt line is up, which may wake it, or once it has taken INTR
    // (its INTA drops the line before the delivery starts).
    Event next() {
        step_writes.clear();
        io.log.clear();
        const uint64_t acks = pic.acks;
        for (int n = 0; n < EVENT_CYCLES; ++n) {
            if (top.trace_unimplemented || (top.trace_halt && !pic.intr && !pic.nmi && pic.acks == acks)) break;
            clock();
            if (top.trace_retire) return Event::Retired;
        }
        if (top.trace_unimplemented) return Event::Unimplemented;
        return Event::Timeout;
    }

    bool stopped() const { return top.trace_halt; }

    State state() const {
        State s;
        for (int n = 0; n < 8; ++n) s.gpr[n] = top.arch_gpr[n];
        s.eflags = top.arch_eflags & EFLAGS_MASK;
        s.eip = top.arch_eip;
        for (int n = 0; n < 6; ++n) {
            s.seg[n].selector = get16(top.arch_sel, n);
            s.seg[n].base = top.arch_base[n];
            s.seg[n].limit = top.arch_limit[n];
            s.seg[n].ar = get16(top.arch_ar, n);
        }
        return s;
    }

private:
    uint32_t address = 0;
    uint64_t loaded = 0;  // the bytes the last load read
    bool io_waiting = false;  // an I/O request is waiting for its ack
    IoAccess io_request;      // ... as it was when it rose
    bool inta_waiting = false;  // an INTA is waiting for the PIC

    template <typename W> static void set16(W &wide, int n, uint16_t value) {
        uint32_t &word = wide[n / 2];
        const int shift = 16 * (n % 2);
        word = (word & ~(0xffffu << shift)) | (uint32_t(value) << shift);
    }
    template <typename W> static uint16_t get16(const W &wide, int n) { return uint16_t(wide[n / 2] >> (16 * (n % 2))); }
};

bool reset_store_regression(Rtl &rtl) {
    State state;
    for (int n = 0; n < 6; ++n) state.seg[n] = fes_i386::real_mode_segment(0);
    state.gpr[fes_i386::REG_EAX] = 0x5aa5;
    state.gpr[fes_i386::REG_EBX] = 0x100;

    const std::array<uint8_t, 3> code = {0x89, 0x07, 0xf4};  // MOV [BX+DI], AX; HLT
    std::copy(code.begin(), code.end(), rtl.ram.begin());
    rtl.ram[0x100] = 0x12;
    rtl.ram[0x101] = 0x34;
    rtl.start(state);

    bool pending = false;
    for (int n = 0; n < EVENT_CYCLES; ++n) {
        rtl.top.clk = 0;
        rtl.top.eval();
        if (rtl.top.dmem_write) {
            pending = rtl.top.dmem_waddr == 0x100 && rtl.top.dmem_wmask == 0x03;
            break;
        }
        rtl.clock();
    }
    if (!pending) return false;

    rtl.top.reset = 1;
    rtl.top.clk = 0;
    rtl.top.eval();
    const bool write_on_reset = rtl.top.dmem_write;
    rtl.clock();
    return !write_on_reset && rtl.ram[0x100] == 0x12 && rtl.ram[0x101] == 0x34 && rtl.step_writes.empty();
}

// ---- Divergences -------------------------------------------------------------------

// Opcodes with a ModRM byte (as scripts/test_fes_i386_vectors.py's MODRM_ONE
// and MODRM_TWO), and the ones whose MOO stems carry the reg field.
bool modrm_one(uint8_t op) {
    if (op < 0x40) return (op & 7) < 4;
    switch (op) {
    case 0x62: case 0x63: case 0x69: case 0x6b: case 0xc0: case 0xc1: case 0xc4: case 0xc5: case 0xc6: case 0xc7:
    case 0xf6: case 0xf7: case 0xfe: case 0xff:
        return true;
    default:
        return (op >= 0x80 && op < 0x90) || (op >= 0xd0 && op < 0xd4) || (op >= 0xd8 && op < 0xe0);
    }
}
bool modrm_two(uint8_t op) {
    return op <= 0x03 || (op >= 0x20 && op <= 0x26) || (op >= 0x90 && op < 0xa0) || op == 0xa3 || op == 0xa4
        || op == 0xa5 || op == 0xab || op == 0xac || op == 0xad || op == 0xaf || (op >= 0xb2 && op < 0xb8)
        || (op >= 0xba && op < 0xc0);
}
bool grouped(bool two, uint8_t op) {
    if (two) return op == 0xba;
    return (op >= 0x80 && op <= 0x83) || op == 0xc0 || op == 0xc1 || (op >= 0xd0 && op <= 0xd3) || op == 0xf6
        || op == 0xf7 || op == 0xfe || op == 0xff;
}
bool is_prefix(uint8_t b) {
    switch (b) {
    case 0x26: case 0x2e: case 0x36: case 0x3e: case 0x64: case 0x65: case 0x66: case 0x67: case 0xf0: case 0xf2:
    case 0xf3:
        return true;
    default:
        return false;
    }
}

// The instruction's MOO stem without 66h/67h ("C0.4", "0FA4") and the
// predicates divergence entries scope by.
struct Facts {
    std::string stem;
    std::set<std::string> predicates;
};

// Whether the 386EX raises #DE for IDIV of the 2n-bit dividend d by the
// n-bit divisor m (fes_i386_muldiv): it divides a negative dividend's one's
// complement by the divisor's magnitude, a bit per step with no bit out at
// the top, and checks the quotient, with the one more the complement owes,
// for its sign's range. An overflowing dividend leaves a garbage quotient.
bool idiv386_faults(uint64_t d, uint32_t m, unsigned n) {
    const uint64_t mask = n == 32 ? 0xffffffffull : (1ull << n) - 1;
    const bool dn = d >> (2 * n - 1) & 1, mn = m >> (n - 1) & 1;
    const uint64_t am = mn ? (0 - uint64_t(m)) & mask : m;
    const uint64_t a = dn ? ~d : d;
    uint64_t r = a >> n & mask, lo = a & mask, q = 0;
    for (unsigned i = 0; i < n; ++i) {
        r = ((r << 1) | (lo >> (n - 1) & 1)) & mask;
        lo = (lo << 1) & mask;
        const bool big = r >= am;
        q = (q << 1) | big;
        if (big) r = (r - am) & mask;
    }
    if (dn && ((r + 1) & mask) == am) ++q;
    const uint64_t half = 1ull << (n - 1);
    return am == 0 || (dn != mn ? q > half : q >= half);
}

// Whether the quotient of IDIV is outside the signed range (or the divisor
// zero), as the 386 manual and the oracle have it.
bool idiv_overflows(uint64_t d, uint32_t m, unsigned n) {
    const uint64_t mask = n == 32 ? 0xffffffffull : (1ull << n) - 1;
    if ((m & mask) == 0) return true;
    const int64_t dividend = n == 32 ? int64_t(d) : int64_t(d << (64 - 2 * n)) >> (64 - 2 * n);
    const int64_t divisor = int64_t(uint64_t(m) << (64 - n)) >> (64 - n);
    if (n == 32 && dividend == INT64_MIN && divisor == -1) return true;
    const int64_t q = dividend / divisor;
    return q < -int64_t(1ull << (n - 1)) || q >= int64_t(1ull << (n - 1));
}

// The memory operand's linear address for the ModRM at code[at] (real mode,
// the 386EX's scaled base for an invalid SIB included); seg is the override
// or -1.
uint32_t modrm_linear(const uint8_t *code, unsigned at, bool as32, int seg, const State &s) {
    const uint8_t modrm = code[at];
    const unsigned mod = modrm >> 6, rm = modrm & 7;
    uint32_t offset = 0;
    bool stack = false;
    unsigned end = at + 1;
    if (as32) {
        int base = int(rm), index = -1;
        unsigned scale = 0;
        if (rm == 4) {
            const uint8_t sib = code[end++];
            scale = sib >> 6;
            index = (sib >> 3) & 7;
            base = sib & 7;
            if (index == 4) index = -1;
            if (base == 5 && mod == 0) base = -1;
        } else if (rm == 5 && mod == 0) {
            base = -1;
        }
        int32_t disp = 0;
        if (base == -1 || mod == 2) {
            std::memcpy(&disp, code + end, 4);
        } else if (mod == 1) {
            disp = int8_t(code[end]);
        }
        offset = uint32_t(disp);
        if (base >= 0) offset += s.gpr[base] * (index < 0 && rm == 4 ? 1u << scale : 1u);
        if (index >= 0) offset += s.gpr[index] << scale;
        stack = base == 4 || base == 5;
    } else {
        static const int bases[8][2] = {{3, 6}, {3, 7}, {5, 6}, {5, 7}, {6, -1}, {7, -1}, {5, -1}, {3, -1}};
        uint32_t disp = 0;
        int b0 = bases[rm][0], b1 = bases[rm][1];
        if (mod == 0 && rm == 6) {
            b0 = -1;
            disp = code[end] | uint32_t(code[end + 1]) << 8;
        } else if (mod == 1) {
            disp = uint32_t(int32_t(int8_t(code[end])));
        } else if (mod == 2) {
            disp = code[end] | uint32_t(code[end + 1]) << 8;
        }
        offset = disp;
        if (b0 >= 0) offset += s.gpr[b0];
        if (b1 >= 0) offset += s.gpr[b1];
        offset &= 0xffff;
        stack = b0 == 5;
    }
    const int segment = seg >= 0 ? seg : stack ? fes_i386::SEG_SS : fes_i386::SEG_DS;
    return s.seg[segment].base + offset;
}

Facts facts(const uint8_t *code, bool code32, const State &before, int vector, OracleBus &bus) {
    const uint32_t ecx = before.gpr[fes_i386::REG_ECX];
    Facts f;
    unsigned i = 0;
    bool os32 = code32, as32 = code32;
    int seg = -1;
    while (i < 15 && is_prefix(code[i])) {
        static const int overrides[8] = {fes_i386::SEG_ES, fes_i386::SEG_CS, fes_i386::SEG_SS, fes_i386::SEG_DS};
        if (code[i] == 0x66) os32 = !code32;
        if (code[i] == 0x67) as32 = !code32;
        if ((code[i] & 0xe7) == 0x26) seg = overrides[(code[i] >> 3) & 3];
        if (code[i] == 0x64) seg = fes_i386::SEG_FS;
        if (code[i] == 0x65) seg = fes_i386::SEG_GS;
        ++i;
    }
    const bool two = code[i] == 0x0f;
    const uint8_t op = two ? code[i + 1] : code[i];
    unsigned at = i + (two ? 2 : 1);
    char text[16];
    std::snprintf(text, sizeof text, two ? "0F%02X" : "%02X", op);
    f.stem = text;
    if (os32) f.predicates.insert("opsize32");
    if (two ? modrm_two(op) : modrm_one(op)) {
        const uint8_t modrm = code[at];
        const unsigned mod = modrm >> 6, rm = modrm & 7;
        if (grouped(two, op)) f.stem += "." + std::to_string((modrm >> 3) & 7);
        unsigned end = at + 1;
        if (mod != 3 && as32) {
            if (rm == 4) {
                const uint8_t sib = code[end++];
                if (((sib >> 3) & 7) == 4 && (sib >> 6)) f.predicates.insert("invalid-sib");
                if (mod == 0 && (sib & 7) == 5) end += 4;
            }
            end += mod == 0 ? (rm == 5 ? 4 : 0) : mod == 1 ? 1 : 4;
        } else if (mod != 3) {
            end += mod == 0 ? (rm == 6 ? 2 : 0) : mod == 1 ? 1 : 2;
        }
        int count = -1;
        if (!two && (op == 0xd0 || op == 0xd1)) count = 1;
        else if ((!two && (op == 0xd2 || op == 0xd3)) || (two && (op == 0xa5 || op == 0xad))) count = ecx & 0x1f;
        else if ((!two && (op == 0xc0 || op == 0xc1)) || (two && (op == 0xa4 || op == 0xac))) count = code[end] & 0x1f;
        if (count >= 0) {
            const int width = !two && (op == 0xc0 || op == 0xd0 || op == 0xd2) ? 8 : os32 ? 32 : 16;
            if (count > 0) f.predicates.insert("count>0");
            if (count > 1) f.predicates.insert("count>1");
            if (count >= width) f.predicates.insert("count>=width");
            if (count > width) f.predicates.insert("count>width");
        }
        // IDIV whose quotient overflows, which the 386EX's check lets through.
        if (!two && (op == 0xf6 || op == 0xf7) && ((modrm >> 3) & 7) == 7) {
            const unsigned n = op == 0xf6 ? 8 : os32 ? 32 : 16;
            uint32_t m = 0;
            if (mod == 3) {
                m = n == 8 ? before.gpr[rm & 3] >> (rm & 4 ? 8 : 0) : before.gpr[rm];
            } else {
                const uint32_t linear = modrm_linear(code, at, as32, seg, before);
                for (unsigned k = 0; k < n / 8; ++k)
                    m |= uint32_t(linear + k < RAM_BYTES ? bus.ram[linear + k] : 0xff) << (8 * k);
            }
            m &= n == 32 ? ~0u : (1u << n) - 1;
            const uint32_t eax = before.gpr[fes_i386::REG_EAX], edx = before.gpr[fes_i386::REG_EDX];
            const uint64_t d = n == 8 ? eax & 0xffff : n == 16 ? (edx & 0xffff) << 16 | (eax & 0xffff)
                                                             : uint64_t(edx) << 32 | eax;
            if (idiv_overflows(d, m, n) && !idiv386_faults(d, m, n)) f.predicates.insert("idiv-unchecked-overflow");
        }
    } else if (!two && (op == 0xd4 || op == 0xd5) && code[at] == 0) {
        f.predicates.insert("imm8=0");
    }
    // PUSHA's or POPA's eight slots wrap the 16-bit stack misaligned, so one
    // straddles FFFFh.
    if (!two && (op == 0x60 || op == 0x61) && !(before.seg[fes_i386::SEG_SS].ar & 0x4000)) {
        const uint32_t size = os32 ? 4 : 2, sp = before.gpr[fes_i386::REG_ESP] & 0xffff;
        const uint32_t start = op == 0x60 ? (sp - 8 * size) & 0xffff : sp;
        if (start + 8 * size > 0x10000 && start % size) f.predicates.insert("slot-straddles-wrap");
    }
    // The frame of the exception or interrupt the step takes (vector), its
    // three words below ESP, overlaps the vector's entry.
    if (vector >= 0) {
        const auto &ss = before.seg[fes_i386::SEG_SS];
        const uint32_t esp = before.gpr[fes_i386::REG_ESP];
        for (uint32_t k = 1; k <= 6; ++k) {
            const uint32_t at = ss.base + ((ss.ar & 0x4000) ? esp - k : (esp - k) & 0xffff);
            if (at >= uint32_t(vector) * 4 && at < uint32_t(vector) * 4 + 4) f.predicates.insert("frame-over-vector");
        }
    }
    return f;
}

std::vector<std::string> split(const std::string &text, char by) {
    std::vector<std::string> out;
    std::string item;
    std::istringstream input(text);
    while (std::getline(input, item, by))
        if (!item.empty()) out.push_back(item);
    return out;
}

struct Entry {
    std::string id;
    bool corpus;  // follow
    std::set<std::string> stems;  // empty: any
    std::vector<std::string> when;
    std::vector<std::string> fields;
};

std::vector<Entry> load_entries(const char *path) {
    std::vector<Entry> entries;
    if (!path) return entries;
    std::ifstream input(path);
    if (!input) throw std::runtime_error(std::string("cannot read ") + path);
    std::string line;
    while (std::getline(input, line)) {
        std::istringstream words(line);
        std::string id, follow, stems, when, fields;
        if (!(words >> id >> follow >> stems >> when >> fields)) continue;
        Entry e;
        e.id = id;
        e.corpus = follow == "corpus";
        if (stems != "*")
            for (auto &s : split(stems, ',')) e.stems.insert(s);
        if (when != "-") e.when = split(when, ',');
        e.fields = split(fields, ',');
        entries.push_back(e);
    }
    return entries;
}

// Fields some divergence leaves to one side, at one retirement or for a run.
// stack_flags: flags in the FLAGS image an exception's delivery pushes.
struct Fields {
    bool all = false;
    uint32_t eflags = 0, stack_flags = 0;
    std::set<std::string> registers;

    void add(const std::string &field) {
        static const std::map<std::string, int> flags = {{"CF", 0}, {"PF", 2}, {"AF", 4}, {"ZF", 6}, {"SF", 7},
                                                         {"TF", 8}, {"IF", 9}, {"DF", 10}, {"OF", 11}};
        if (field == "*") {
            all = true;
        } else if (field.rfind("eflags.", 0) == 0) {
            auto it = flags.find(field.substr(7));
            eflags |= it != flags.end() ? 1u << it->second : 0;
        } else if (field.rfind("stack.flags.", 0) == 0) {
            auto it = flags.find(field.substr(12));
            stack_flags |= it != flags.end() ? 1u << it->second : 0;
        } else if (field.rfind("stack.", 0) != 0) {
            registers.insert(field);  // a register, "ram", "unlisted-write" or "exception"
        }
    }
    bool has(const std::string &name) const { return all || registers.count(name); }
    bool empty() const { return !all && !eflags && !stack_flags && registers.empty(); }
};

// ---- Comparison ----------------------------------------------------------------------

const char *const GPR_NAMES[8] = {"eax", "ecx", "edx", "ebx", "esp", "ebp", "esi", "edi"};
const char *const SEG_NAMES[6] = {"es", "cs", "ss", "ds", "fs", "gs"};

struct Mismatches {
    std::vector<std::string> items;
    void add(const std::string &field, uint64_t expected, uint64_t actual) {
        items.push_back(field + " " + hex(expected) + " " + hex(actual));
    }
};

// RTL against the oracle, leaving out skip's fields.
void compare(const State &want, const State &got, const Fields &skip, const std::string &at, Mismatches &out) {
    if (skip.all) return;
    if (!skip.has("eip") && want.eip != got.eip) out.add(at + "eip", want.eip, got.eip);
    for (int n = 0; n < 8; ++n)
        if (!skip.has(GPR_NAMES[n]) && want.gpr[n] != got.gpr[n]) out.add(at + GPR_NAMES[n], want.gpr[n], got.gpr[n]);
    const uint32_t mask = EFLAGS_MASK & ~skip.eflags;
    if ((want.eflags & mask) != (got.eflags & mask)) out.add(at + "eflags", want.eflags & mask, got.eflags & mask);
    for (int n = 0; n < 6; ++n) {
        if (skip.has(SEG_NAMES[n])) continue;
        const auto &w = want.seg[n], &g = got.seg[n];
        const std::string name = at + SEG_NAMES[n];
        if (w.selector != g.selector) out.add(name, w.selector, g.selector);
        if (w.base != g.base) out.add(name + ".base", w.base, g.base);
        if (w.limit != g.limit) out.add(name + ".limit", w.limit, g.limit);
        if (w.ar != g.ar) out.add(name + ".ar", w.ar, g.ar);
    }
}

// The oracle takes the RTL's values of fields that follow the corpus.
void adopt(State &oracle_state, const State &rtl, const Fields &fields) {
    if (fields.all) {
        for (int n = 0; n < 8; ++n) oracle_state.gpr[n] = rtl.gpr[n];
        oracle_state.eflags = (oracle_state.eflags & ~EFLAGS_MASK) | (rtl.eflags & EFLAGS_MASK);
        oracle_state.eip = rtl.eip;
        for (int n = 0; n < 6; ++n) oracle_state.seg[n] = rtl.seg[n];
        return;
    }
    oracle_state.eflags = (oracle_state.eflags & ~fields.eflags) | (rtl.eflags & fields.eflags);
    for (int n = 0; n < 8; ++n)
        if (fields.has(GPR_NAMES[n])) oracle_state.gpr[n] = rtl.gpr[n];
}

// ---- One lockstep run ---------------------------------------------------------------

struct Summary {
    uint64_t runs = 0, passed = 0, failed = 0, retirements = 0, cycles = 0, halts = 0, limits = 0;
    uint64_t interrupts = 0;  // corpus runs that took a software interrupt, then halted in its handler
    uint64_t external_interrupts = 0, nmis = 0;  // INTRs and NMIs delivered
    // Runs that delivered an exception: in the corpus, then halted in its
    // handler; in random programs, stopped there. double_faults: stopped at a
    // fault in a delivery.
    uint64_t faults = 0, double_faults = 0;
    uint64_t trace_hash = 1469598103934665603ull;
    std::map<int, uint64_t> fault_vectors;
    std::map<std::string, uint64_t> divergences;  // retirements an entry left to the corpus
    // Cycles from one retirement to the next, by what retired: after a
    // branch, taken or not; otherwise an instruction with more than one
    // prefix (each but the last costs Q a cycle), an LEA, a selector read
    // (MOV r/m, Sreg); after a shift or rotate, an instruction that consumes
    // the flags (D holds it a cycle) or one that does not; or any other.
    // An instruction with a memory operand is an address one (as LEA, its
    // address may wait in D) unless it reads memory right after an
    // instruction that writes it (store_load: D holds it at least a cycle),
    // or closely follows a segment load (it waits for the base). A push or
    // pop right after one is stack_stack: one cycle, as its ESP is early
    // from AG. Left out: CALL rel, r/m and ptr, PUSH and POP r/m and RETF
    // (several uops), LEAVE (on a 16-bit stack it waits for ESP's high
    // half), PUSH and POP of (E)SP, which read ESP as data, and anything
    // after POP (E)SP or POPA, which load ESP (POPAD's high word). An
    // exception's delivery is no instruction: neither it nor the handler's
    // first is counted. XCHG of registers, three uops, is several, and so is
    // a multiply or divide, which freezes D to WB while it works, and any
    // retirement that waited for one (the uop in WB waits too). Any
    // instruction within five after a string is after_string: one that reads
    // ESI, EDI or ECX waits in D until the string's last element retires.
    // IN and OUT wait in WB for their access, so are several; the instruction
    // after an I/O one (a string's included) is after_io: it waits in D until
    // that retires.
    // Any other instruction right after a push or pop is after_stack: it
    // waits in D if it reads ESP. Any other after a store, not after a shift,
    // is after_store: one cycle, as D's store hold must not catch it. A store
    // counts only as an instruction's last uop, which XCHG's and POP m's are
    // not.
    std::map<uint64_t, uint64_t> gap_sequential, gap_prefixed, gap_lea, gap_selector, gap_after_taken,
        gap_after_not_taken, gap_shift_consumer, gap_after_shift, gap_store_load, gap_after_store,
        gap_address, gap_after_segment, gap_after_stack, gap_stack_stack, gap_several, gap_after_string,
        gap_after_io;
    uint64_t smc = 0;  // runs stopped at a store into their own code
    uint64_t vector_moved = 0;  // runs stopped at an interrupt whose vector they had rewritten
    uint64_t load_waits = 0;
    uint64_t io_accesses = 0;  // I/O accesses compared
};

void hash_state(Summary &summary, const State &s) {
    auto mix = [&](uint32_t v) {
        for (int i = 0; i < 4; ++i) summary.trace_hash = (summary.trace_hash ^ ((v >> (8 * i)) & 0xff)) * 1099511628211ull;
    };
    mix(s.eip);
    for (uint32_t r : s.gpr) mix(r);
    mix(s.eflags);
}

// What an instruction does with memory in this build: its ModRM memory
// operand, or the stack. Reads, writes, or both.
struct MemUse {
    bool reads = false, writes = false;
    bool any() const { return reads || writes; }
};

// MOVS, CMPS, STOS, LODS, SCAS, INS or OUTS.
bool is_string(uint8_t op) { return (op >= 0xa4 && op <= 0xa7) || (op >= 0xaa && op <= 0xaf) || (op >= 0x6c && op <= 0x6f); }

// IN, OUT, INS or OUTS.
bool is_io(uint8_t op) { return (op >= 0xe4 && op <= 0xe7) || (op >= 0xec && op <= 0xef) || (op >= 0x6c && op <= 0x6f); }

// A push (PUSH r, r/m, imm or Sreg, CALL rel, r/m or ptr) or a pop (POP r,
// r/m or Sreg, RET, RETF, LEAVE).
MemUse stack_use(const uint8_t *code) {
    unsigned i = 0;
    while (i < 15 && is_prefix(code[i])) ++i;
    MemUse m;
    const uint8_t op = code[i];
    const unsigned reg = (code[i + 1] >> 3) & 7;
    if (op == 0xff && (reg == 2 || reg == 6)) {
        m.writes = true;
    } else if (op == 0x8f) {
        m.reads = true;
    } else if (op == 0x0f) {
        m.writes = code[i + 1] == 0xa0 || code[i + 1] == 0xa8;
        m.reads = code[i + 1] == 0xa1 || code[i + 1] == 0xa9;
    } else {
        m.writes = (op >= 0x50 && op < 0x58) || op == 0x68 || op == 0x6a || op == 0x06 || op == 0x0e ||
                   op == 0x16 || op == 0x1e || op == 0xe8 || op == 0x9a || op == 0x60 || op == 0x9c ||
                   op == 0xcc || op == 0xcd || op == 0xce || op == 0xc8;
        m.reads = (op >= 0x58 && op < 0x60) || op == 0x07 || op == 0x17 || op == 0x1f || op == 0xc2 ||
                  op == 0xc3 || op == 0xc9 || op == 0xca || op == 0xcb || op == 0x61 || op == 0x9d || op == 0xcf;
    }
    return m;
}

MemUse mem_use(const uint8_t *code) {
    unsigned i = 0;
    while (i < 15 && is_prefix(code[i])) ++i;
    MemUse m = stack_use(code);
    const bool two = code[i] == 0x0f;
    const uint8_t op = two ? code[i + 1] : code[i];
    const uint8_t modrm = code[i + (two ? 2 : 1)];
    const unsigned reg = (modrm >> 3) & 7;
    // PUSH, POP and CALL of r/m have a ModRM operand besides the stack.
    if (m.any() && !(op == 0x8f || (op == 0xff && (reg == 2 || reg == 6)))) return m;
    if (!two && op >= 0xa0 && op <= 0xa3) {  // MOV with a direct offset
        m.reads = !(op & 2);
        m.writes = op & 2;
        return m;
    }
    if (!two && op == 0xd7) {  // XLAT
        m.reads = true;
        return m;
    }
    if (!two && is_string(op)) {  // MOVS, CMPS, STOS, LODS, SCAS, INS, OUTS
        m.reads = op != 0xaa && op != 0xab && op != 0x6c && op != 0x6d;
        m.writes = (op >= 0xa4 && op <= 0xa5) || op == 0xaa || op == 0xab || op == 0x6c || op == 0x6d;
        return m;
    }
    if ((modrm >> 6) == 3) return m;
    if (two) {
        if ((op & 0xf0) == 0x90) m.writes = true;
        else if (op == 0xa4 || op == 0xa5 || op == 0xac || op == 0xad) m.reads = m.writes = true;
        else if (op == 0xb6 || op == 0xb7 || op == 0xbe || op == 0xbf || op == 0xb2 || op == 0xb4 || op == 0xb5 ||
                 op == 0xaf)
            m.reads = true;
        return m;
    }
    if (op < 0x40 && (op & 7) < 4) {
        m.reads = true;
        m.writes = !(op & 2) && ((op >> 3) & 7) != 7;
    } else if (op >= 0x80 && op <= 0x83) {
        m.reads = true;
        m.writes = reg != 7;
    } else if (op == 0x84 || op == 0x85 || op == 0x8a || op == 0x8b || op == 0x8e || op == 0xc4 || op == 0xc5 ||
               op == 0x69 || op == 0x6b) {
        m.reads = true;
    } else if (op == 0x88 || op == 0x89 || op == 0x8c || op == 0xc6 || op == 0xc7) {
        m.writes = true;
    } else if (op == 0xff && (reg == 2 || reg == 4 || reg == 5 || reg == 6)) {  // CALL, JMP, PUSH r/m, JMP m16:16/32
        m.reads = true;
    } else if (op == 0x8f) {  // POP r/m
        m.writes = true;
    } else if (op == 0x86 || op == 0x87) {  // XCHG
        m.reads = m.writes = true;
    } else if (op == 0xff && reg == 3) {  // CALL m16:16/32: the pointer, then the stack
        m.reads = m.writes = true;
    } else if (op == 0xc0 || op == 0xc1 || (op >= 0xd0 && op <= 0xd3) || op == 0xfe || op == 0xff) {
        m.reads = m.writes = true;
    } else if (op == 0xf6 || op == 0xf7) {
        m.reads = true;
        m.writes = reg == 2 || reg == 3;
    }
    return m;
}

// A segment load (MOV Sreg, r/m, POP Sreg, or a far pointer's: LES and its
// kin).
bool loads_segment(const uint8_t *code) {
    unsigned i = 0;
    while (i < 15 && is_prefix(code[i])) ++i;
    if (code[i] == 0x0f)
        return code[i + 1] == 0xa1 || code[i + 1] == 0xa9 || code[i + 1] == 0xb2 || code[i + 1] == 0xb4 ||
               code[i + 1] == 0xb5;
    return code[i] == 0x8e || code[i] == 0x07 || code[i] == 0x17 || code[i] == 0x1f || code[i] == 0xc4 ||
           code[i] == 0xc5;
}

// MUL, IMUL (all three forms), DIV, IDIV, AAM or AAD: EX's multiply and
// divide unit, which works over several cycles.
bool is_muldiv(const uint8_t *code) {
    unsigned i = 0;
    while (i < 15 && is_prefix(code[i])) ++i;
    const uint8_t op = code[i];
    if (op == 0x0f) return code[i + 1] == 0xaf;
    return ((op == 0xf6 || op == 0xf7) && ((code[i + 1] >> 3) & 7) >= 4) || op == 0x69 || op == 0x6b ||
           op == 0xd4 || op == 0xd5;
}

// A shift or rotate: group 2, SHLD or SHRD.
bool is_shift(const uint8_t *code) {
    unsigned i = 0;
    while (i < 15 && is_prefix(code[i])) ++i;
    const uint8_t op = code[i];
    if (op == 0x0f) return code[i + 1] == 0xa4 || code[i + 1] == 0xa5 || code[i + 1] == 0xac || code[i + 1] == 0xad;
    return op == 0xc0 || op == 0xc1 || (op >= 0xd0 && op <= 0xd3);
}

// An instruction whose result depends on the old flags: ADC, SBB, Jcc,
// SETcc, LAHF, CMC, LOOPE, LOOPNE, RCL, RCR, SALC.
bool consumes_flags(const uint8_t *code) {
    unsigned i = 0;
    while (i < 15 && is_prefix(code[i])) ++i;
    const uint8_t op = code[i], reg = (code[i + 1] >> 3) & 7;
    if (op == 0x0f) return (code[i + 1] & 0xe0) == 0x80;  // Jcc rel16/32, SETcc
    if (op < 0x40 && (op & 7) < 6) return ((op >> 3) & 7) == 2 || ((op >> 3) & 7) == 3;
    if (op >= 0x80 && op <= 0x83) return reg == 2 || reg == 3;
    if (op == 0xc0 || op == 0xc1 || (op >= 0xd0 && op <= 0xd3)) return reg == 2 || reg == 3;
    return (op >= 0x70 && op <= 0x7f) || op == 0x9f || op == 0xf5 || op == 0xe0 || op == 0xe1 || op == 0xd6;
}

// A branch's opcode and whether it is taken from this state, as the 386
// decides: Jcc by its condition, LOOPcc and JCXZ by CX or ECX (address size).
// CALL, RET, JMP r/m and the far transfers are always taken.
bool branch_taken(const uint8_t *code, const State &s, bool code32, bool &taken) {
    unsigned i = 0;
    bool as32 = code32;
    while (i < 15 && is_prefix(code[i])) {
        if (code[i] == 0x67) as32 = !code32;
        ++i;
    }
    const uint8_t op = code[i];
    auto cond = [&](unsigned cc) {
        const uint32_t f = s.eflags;
        const bool cf = f & 1, pf = f & 4, zf = f & 0x40, sf = f & 0x80, of = f & 0x800;
        bool t = false;
        switch (cc >> 1) {
        case 0: t = of; break;
        case 1: t = cf; break;
        case 2: t = zf; break;
        case 3: t = cf || zf; break;
        case 4: t = sf; break;
        case 5: t = pf; break;
        case 6: t = sf != of; break;
        default: t = zf || sf != of; break;
        }
        return t != bool(cc & 1);
    };
    const uint32_t count = as32 ? s.gpr[fes_i386::REG_ECX] : s.gpr[fes_i386::REG_ECX] & 0xffff;
    const bool zf = s.eflags & 0x40;
    if (op >= 0x70 && op < 0x80) taken = cond(op & 15);
    else if (op == 0x0f && code[i + 1] >= 0x80 && code[i + 1] < 0x90) taken = cond(code[i + 1] & 15);
    else if (op == 0xe9 || op == 0xeb || op == 0xe8 || op == 0xc2 || op == 0xc3 || op == 0xea || op == 0x9a ||
             op == 0xca || op == 0xcb || op == 0xcc || op == 0xcd || op == 0xce || op == 0xcf)
        taken = true;
    else if (op == 0xff && ((code[i + 1] >> 3) & 7) >= 2 && ((code[i + 1] >> 3) & 7) <= 5) taken = true;
    else if (op == 0xe2) taken = count != 1;
    else if (op == 0xe1) taken = count != 1 && zf;
    else if (op == 0xe0) taken = count != 1 && !zf;
    else if (op == 0xe3) taken = count == 0;
    else return false;
    return true;
}

struct Outcome {
    bool halted = false;   // both sides reached HLT
    int exception = -1;    // the last exception both sides delivered
    bool double_fault = false;  // stopped at a fault in a delivery (no double fault is built)
    bool limit = false;    // stopped at the retirement limit
    bool smc = false;      // stopped at a store into [code_lo, code_hi)
    bool vector_moved = false;  // stopped at an injected interrupt whose vector entry or handler the program rewrote
    int interrupt = -1;    // the last software interrupt both sides took (INT n, INT3, INTO)
    std::string code;      // the last instruction's first bytes, for a failure's line
};

// Interrupts injected into a run. The RTL samples INTR and NMI as an
// instruction retires, the oracle before the next one starts: so an
// interrupt for the boundary after a step reaches the RTL before its window
// for that step, and the oracle before the next. INTR stays up on each side
// until that side's INTA; NMI is an edge. Each step raises INTR now and then,
// more often after the instructions whose boundary is special (STI, CLI, MOV
// SS, POP SS, POPF, IRET), and NMI rarely; and a HLT that is not the
// program's last wakes on INTR.
struct Irq {
    std::mt19937_64 *random = nullptr;
    uint32_t handler = 0;  // the linear address every injected vector's entry points at ...
    std::array<uint8_t, 4> entry{};  // ... as this IP and CS
    bool intr_next = false, nmi_next = false;  // raised on the RTL, for the oracle's next step
    bool nmi_last = false;  // raised for the step before: NMI is not raised twice in a row, so the
                            // RTL's line is low between its one-cycle pulses
    uint8_t vector = 0;
};

// Run from the state both sides hold now. label_corpus and label_oracle hold
// label-scoped divergence fields.
Outcome lockstep(fes_i386::Oracle &oracle, OracleBus &bus, Rtl &rtl, const std::vector<Entry> &entries,
                 const Fields &label_corpus, uint64_t retire_limit, bool stop_at_exception, Summary &summary,
                 Mismatches &out, uint32_t code_lo = 0, uint32_t code_hi = 0, Irq *irq = nullptr) {
    Outcome outcome;
    // Raise INTR (or NMI) for the boundary after the instruction at code, on
    // the RTL now; the oracle takes it before its next step.
    auto raise = [&](const uint8_t *code, bool wake) {
        unsigned i = 0;
        while (i < 15 && is_prefix(code[i])) ++i;
        const uint8_t op = code[i];
        const bool edge = op == 0xfb || op == 0xfa || op == 0x17 || op == 0x9d || op == 0xcf ||
                          (op == 0x8e && ((code[i + 1] >> 3) & 7) == 2);
        if (!rtl.pic.intr && (wake || (*irq->random)() % (edge ? 2 : 32) == 0)) {
            irq->vector = uint8_t(8 + (*irq->random)() % 8);
            rtl.pic.intr = true;
            rtl.pic.vector = irq->vector;
            irq->intr_next = true;
        }
        const bool nmi = !wake && !irq->nmi_last && (*irq->random)() % (edge ? 8 : 256) == 0;
        if (nmi) {
            rtl.pic.nmi = true;
            irq->nmi_next = true;
        }
        irq->nmi_last = nmi;
    };
    uint64_t last_retirement = 0;
    bool retired_before = false, branch_before = false, taken_before = false, shift_before = false;
    bool store_before = false, stack_before = false, pop_sp_before = false;
    unsigned since_segment = 99;  // retirements since a segment load
    unsigned since_string = 99;   // ... since a string instruction
    bool io_before = false;       // the last retirement was an I/O instruction
    for (uint64_t step = 0;; ++step) {
        if (step == retire_limit) {
            outcome.limit = true;
            return outcome;
        }
        const State before = oracle.get_state();
        const uint32_t linear = before.seg[fes_i386::SEG_CS].base + before.eip;
        uint8_t code[16];
        for (int i = 0; i < 16; ++i) code[i] = linear + i < RAM_BYTES ? bus.ram[linear + i] : 0xff;
        outcome.code.clear();
        for (int i = 0; i < 12; ++i) outcome.code += (i ? " " : "") + hex(code[i] | 0x100).substr(1);
        if (std::getenv("FES_I386_VERBOSE"))
            std::fprintf(stderr, "step %llu eip %x [%s] esp %x ebp %x ss.base %x\n", (unsigned long long)step,
                         before.eip, outcome.code.c_str(), before.gpr[fes_i386::REG_ESP], before.gpr[fes_i386::REG_EBP],
                         before.seg[fes_i386::SEG_SS].base);
        const bool code32 = before.seg[fes_i386::SEG_CS].ar & 0x4000;

        if (irq) {
            if (irq->intr_next) {
                bus.pic.intr = true;
                bus.pic.vector = irq->vector;
                oracle.set_intr(true);
            }
            if (irq->nmi_next) oracle.nmi();
            irq->intr_next = irq->nmi_next = false;
        }
        // Whether the vectors an injected interrupt may take still point at
        // the handler, which is still there.
        auto intact = [&](unsigned v) {
            if (!irq || bus.ram[irq->handler] != 0xcf) return false;
            for (unsigned i = 0; i < 4; ++i)
                if (bus.ram[v * 4 + i] != irq->entry[i]) return false;
            return true;
        };
        const bool intr_intact = intact(bus.pic.vector), nmi_intact = intact(2);
        bus.io.log.clear();
        fes_i386::StepResult result = oracle.step();
        if (std::getenv("FES_I386_VERBOSE"))
            for (const auto &e : result.events)
                std::fprintf(stderr, "  event %d vector %x writes_before %u\n", int(e.kind), e.vector, e.writes_before);
        if (irq && !bus.pic.intr) oracle.set_intr(false);  // its INTA dropped it
        // Not while the RTL is in HLT: the wake it takes now is this step's,
        // and a line raised for the next would reach it at the same boundary.
        if (irq && !rtl.top.trace_halt) raise(code, false);
        const Rtl::Event event = rtl.next();
        const std::string at = "@" + hex(before.eip) + ".";

        // The oracle's first exception, and the vector the step takes: that,
        // or a software interrupt's. external: an INTR's or NMI's, which the
        // step takes before its instruction. interrupted: the step's first
        // event is that one, so it runs no instruction; what follows is a
        // fault of its delivery.
        unsigned oracle_exceptions = 0;
        int vector = -1, taken = -1, external = -1;
        for (const auto &e : result.events) {
            if (e.kind == fes_i386::EventKind::Exception && oracle_exceptions++ == 0) vector = e.vector;
            if (e.kind == fes_i386::EventKind::SoftwareInterrupt && taken < 0) taken = e.vector;
            if ((e.kind == fes_i386::EventKind::ExternalInterrupt || e.kind == fes_i386::EventKind::Nmi) &&
                external < 0)
                external = e.vector;
        }
        const bool interrupted = !result.events.empty() &&
                                 (result.events[0].kind == fes_i386::EventKind::ExternalInterrupt ||
                                  result.events[0].kind == fes_i386::EventKind::Nmi);
        if (interrupted) taken = external;
        else if (vector >= 0) taken = vector;

        // Fields this instruction leaves to the corpus. An entry that leaves it
        // every field also decides whether it faults: the RTL's outcome stands,
        // and the oracle takes the RTL's state and memory.
        // An interrupt's step runs no instruction, so only the entries for
        // every opcode apply (interrupt-vector-before-push).
        Fields skip = label_corpus;
        const Facts fact = facts(code, code32, before, taken, bus);
        for (const Entry &e : entries) {
            if (!e.corpus || (!e.stems.empty() && (interrupted || !e.stems.count(fact.stem)))) continue;
            bool holds = true;
            for (const auto &p : e.when) holds = holds && fact.predicates.count(p);
            if (!holds) continue;
            for (const auto &field : e.fields) skip.add(field);
            ++summary.divergences[e.id];
        }

        // Every byte the oracle writes is cleared after the run.
        for (const auto &w : result.writes)
            for (unsigned i = 0; i < w.length; ++i) rtl.written.insert(w.physical + i);
        // An injected interrupt through a vector entry the program rewrote,
        // or to a handler it wrote over, runs data from here: stop. (Bochs's
        // step also takes a fault of what it fetches there.)
        if (external >= 0 && !(external == 2 ? nmi_intact : intr_intact)) {
            outcome.vector_moved = true;
            return outcome;
        }
        const bool delivered = event == Rtl::Event::Retired && rtl.top.trace_exception;
        if (oracle_exceptions > 1 || result.shutdown) {
            // A fault in an exception's delivery (or in an INT's, then in
            // the exception's): a double fault, then shutdown. The RTL stops
            // at the second fault, as not built, with nothing of the
            // instruction retired and nothing of the delivery written. An
            // entry that leaves every field to the corpus may let the
            // instruction transfer part way first (a PUSHA or POPA whose
            // slots straddle the wrap); flags an entry leaves to the corpus
            // may have changed first (a divide error's).
            if (event != Rtl::Event::Unimplemented || rtl.top.trace_vector != 8)
                out.add(at + "double-fault", 8, event == Rtl::Event::Unimplemented ? rtl.top.trace_vector : 0xffu);
            if (!skip.all) {
                // What the instruction did before its fault may stand:
                // ENTER's pushes, a REP string's elements with their steps of
                // ESI, EDI and ECX, LODS's eAX and CMPS's and SCAS's flags,
                // which the oracle's state shows. Each byte the RTL wrote,
                // the oracle wrote before its first delivery, a fault's or an
                // INT's, and with the same value unless the oracle wrote it
                // again after (the part of a frame it pushed). The RTL may
                // write less: a far CALL or INT that checks its slots before
                // its pushes, as the 386EX does, writes none of them. Nothing
                // of a delivery is written.
                const size_t own = result.events.empty() ? result.writes.size() : result.events[0].writes_before;
                std::set<uint32_t> own_writes, later_writes;
                for (size_t k = 0; k < result.writes.size(); ++k)
                    for (unsigned i = 0; i < result.writes[k].length; ++i)
                        (k < own ? own_writes : later_writes).insert(result.writes[k].physical + i);
                for (uint32_t address : rtl.step_writes) {
                    if (!own_writes.count(address))
                        out.add(at + "fault-write", address, address < RAM_BYTES ? rtl.ram[address] : 0);
                    else if (!later_writes.count(address) && address < RAM_BYTES && bus.ram[address] != rtl.ram[address])
                        out.add(at + "fault-write." + hex(address), bus.ram[address], rtl.ram[address]);
                }
                State want = before;
                const State oracle_now = oracle.get_state();
                for (int r : {fes_i386::REG_EAX, fes_i386::REG_ECX, fes_i386::REG_ESI, fes_i386::REG_EDI})
                    want.gpr[r] = oracle_now.gpr[r];
                // The arithmetic flags: the oracle's delivery sets RF and
                // clears IF before its second fault.
                want.eflags = (before.eflags & ~0x8d5u) | (oracle_now.eflags & 0x8d5u);
                compare(want, rtl.state(), skip, at, out);
            }
            outcome.double_fault = true;
            return outcome;
        }
        // An entry that leaves every field to the corpus decides whether the
        // instruction faults: the RTL's outcome, a delivery or a retirement,
        // stands, and the oracle takes its state and memory below.
        if (vector >= 0 && !skip.all) {
            if (!delivered) {
                out.add(at + (event == Rtl::Event::Unimplemented ? "unimplemented" : "exception"), uint32_t(vector),
                        event == Rtl::Event::Retired ? 0xffu : 0xfeu);
                return outcome;
            }
            if (rtl.top.trace_vector != vector) out.add(at + "vector", uint32_t(vector), rtl.top.trace_vector);
        } else if (interrupted) {
            if (!delivered) {
                out.add(at + "interrupt", uint32_t(external), event == Rtl::Event::Retired ? 0xffu : 0xfeu);
                return outcome;
            }
            if (rtl.top.trace_vector != external) out.add(at + "vector", uint32_t(external), rtl.top.trace_vector);
            ++(external == 2 ? summary.nmis : summary.external_interrupts);
        } else if (delivered && !skip.all) {
            out.add(at + "unexpected-exception", 0, rtl.top.trace_vector);
            return outcome;
        }
        if (event == Rtl::Event::Unimplemented) {
            out.add(at + "unimplemented", 0, 1);
            return outcome;
        }
        if (event != Rtl::Event::Retired) {
            out.add(at + "timeout", 0, 0);
            return outcome;
        }
        ++summary.retirements;
        if (delivered) {
            retired_before = false;
        } else {
            unsigned prefixes = 0;
            while (prefixes < 15 && is_prefix(code[prefixes])) ++prefixes;
            const uint8_t op = code[prefixes];
            const MemUse use = mem_use(code);
            const bool xchg = op == 0x86 || op == 0x87 || (op >= 0x91 && op <= 0x97);
            const bool muldiv = is_muldiv(code);
            if (retired_before) {
                auto &gaps = branch_before ? (taken_before ? summary.gap_after_taken : summary.gap_after_not_taken)
                           : io_before ? summary.gap_after_io
                           : since_string < 6 ? summary.gap_after_string
                           : prefixes > 1 ? summary.gap_prefixed
                           : use.any() && since_segment < 6 ? summary.gap_after_segment
                           : op == 0x8c ? summary.gap_selector
                           : use.reads && store_before ? summary.gap_store_load
                           : stack_before && !pop_sp_before && stack_use(code).any() && op != 0xe8 && op != 0x9a &&
                                     op != 0xca && op != 0xcb && op != 0xc9 && op != 0x54 && op != 0x5c &&
                                     op != 0x60 && op != 0x61 && (op < 0xcc || op > 0xcf) && op != 0xff &&
                                     op != 0x8f && op != 0xc8
                               ? summary.gap_stack_stack
                           : op == 0x8d || use.any() ? summary.gap_address
                           : xchg || muldiv || is_io(op) ? summary.gap_several
                           : stack_before ? summary.gap_after_stack
                           : shift_before ? (consumes_flags(code) ? summary.gap_shift_consumer : summary.gap_after_shift)
                           : store_before ? summary.gap_after_store
                                          : summary.gap_sequential;
                ++(rtl.md_waits ? summary.gap_several : gaps)[rtl.cycles - last_retirement];
            }
            // A string's or ENTER's last store is one of several uops, and
            // what follows a string may wait for its ESI, EDI and ECX.
            store_before = use.writes && !xchg && op != 0x8f && !is_string(op) && op != 0xc8;
            stack_before = stack_use(code).any();
            pop_sp_before = op == 0x5c || op == 0x61 || (op == 0x8f && code[prefixes + 1] == 0xc4);
            since_segment = loads_segment(code) ? 0 : since_segment + 1;
            since_string = is_string(op) ? 0 : since_string + 1;
            io_before = is_io(op);
            retired_before = true;
            branch_before = branch_taken(code, before, code32, taken_before);
            shift_before = is_shift(code);
            last_retirement = rtl.cycles;
        }
        rtl.md_waits = 0;

        {
            // The bytes each side wrote, and their values, unless an entry
            // leaves memory to the corpus: then the oracle takes the RTL's.
            std::set<uint32_t> oracle_writes;
            for (const auto &w : result.writes)
                for (unsigned i = 0; i < w.length; ++i) oracle_writes.insert(w.physical + i);
            if (skip.has("ram")) {
                std::set<uint32_t> both = oracle_writes;
                both.insert(rtl.step_writes.begin(), rtl.step_writes.end());
                for (uint32_t address : both) {
                    if (address < RAM_BYTES) bus.ram[address] = rtl.ram[address];
                    oracle.invalidate(address, 1);
                }
            } else {
                // A delivery's pushed FLAGS image, at SS:SP - 2 before the
                // instruction, may differ in the flags an entry leaves to the
                // corpus (stack.flags): the oracle takes the RTL's.
                if (delivered && skip.stack_flags) {
                    const auto &ss = before.seg[fes_i386::SEG_SS];
                    const uint32_t esp = before.gpr[fes_i386::REG_ESP];
                    for (uint32_t k = 0; k < 2; ++k) {
                        const uint32_t at = ss.base + ((ss.ar & 0x4000) ? esp - 2 + k : (esp - 2 + k) & 0xffff);
                        const uint8_t mask = uint8_t(skip.stack_flags >> (8 * k));
                        if (at < RAM_BYTES && oracle_writes.count(at) && !((bus.ram[at] ^ rtl.ram[at]) & ~mask)) {
                            bus.ram[at] = rtl.ram[at];
                            oracle.invalidate(at, 1);
                        }
                    }
                }
                if (oracle_writes != rtl.step_writes) {
                    const uint32_t first = !oracle_writes.empty() ? *oracle_writes.begin() : 0;
                    const uint32_t got = !rtl.step_writes.empty() ? *rtl.step_writes.begin() : 0;
                    out.add(at + "write-bytes", (uint64_t(oracle_writes.size()) << 32) | first,
                            (uint64_t(rtl.step_writes.size()) << 32) | got);
                }
                for (uint32_t address : oracle_writes)
                    if (address < RAM_BYTES && bus.ram[address] != rtl.ram[address])
                        out.add(at + "write." + hex(address), bus.ram[address], rtl.ram[address]);
            }
            for (uint32_t address : oracle_writes)
                if (address >= code_lo && address < code_hi) outcome.smc = true;
        }
        // The I/O accesses, in order; a request must hold until its ack.
        summary.io_accesses += bus.io.log.size();
        if (bus.io.log != rtl.io.log) {
            size_t k = 0;
            while (k < bus.io.log.size() && k < rtl.io.log.size() && bus.io.log[k] == rtl.io.log[k]) ++k;
            out.add(at + "io." + std::to_string(k), k < bus.io.log.size() ? bus.io.log[k].key() : ~0ull,
                    k < rtl.io.log.size() ? rtl.io.log[k].key() : ~0ull);
        }
        if (rtl.io_unstable) {
            out.add(at + "io-unstable", 0, rtl.io_unstable);
            rtl.io_unstable = 0;
        }
        // Each side acknowledges each INTR it takes once, with INTR up.
        if (bus.pic.acks != rtl.pic.acks || bus.pic.unexpected || rtl.pic.unexpected)
            out.add(at + "inta", bus.pic.acks | bus.pic.unexpected << 32, rtl.pic.acks | rtl.pic.unexpected << 32);
        State oracle_state = oracle.get_state();
        const State rtl_state = rtl.state();
        compare(oracle_state, rtl_state, skip, at, out);
        hash_state(summary, rtl_state);
        if (!out.items.empty()) return outcome;
        if (!skip.empty()) {
            adopt(oracle_state, rtl_state, skip);
            oracle.set_state(oracle_state);
        }
        // An entry that leaves every field to the corpus decides whether the
        // RTL halts too: a REP that stores over the HLT after it, which the
        // RTL has fetched, as the 386EX had.
        if (result.halted || rtl.stopped()) {
            if (!(result.halted && rtl.stopped()) && !(skip.all && rtl.stopped()))
                out.add(at + "halt", result.halted, rtl.stopped());
            // A HLT before the program's last, with IF set, wakes on INTR.
            const State now = oracle.get_state();
            if (irq && result.halted && rtl.top.trace_halt && !rtl.top.trace_unimplemented && (now.eflags & 0x200) &&
                now.seg[fes_i386::SEG_CS].base + now.eip != code_hi) {
                raise(code, true);
                continue;
            }
            outcome.halted = true;
            return outcome;
        }
        for (const auto &e : result.events)
            if (e.kind == fes_i386::EventKind::SoftwareInterrupt) outcome.interrupt = e.vector;
        if (delivered && !interrupted) {
            outcome.exception = rtl.top.trace_vector;
            if (stop_at_exception) return outcome;
        }
        // ... or one whose frame wrote over the handler.
        if (interrupted && bus.ram[irq->handler] != 0xcf) {
            outcome.vector_moved = true;
            return outcome;
        }
        if (outcome.smc) return outcome;
    }
}

// After a HLT the CPU stays stopped: no fetch request, D issue or data read.
void check_halt_quiet(Rtl &rtl, const std::string &label, Mismatches &out) {
    bool resumed = false, fetched = false, issued = false, read = false;
    for (unsigned cycle = 0; cycle < 32; ++cycle) {
        rtl.top.clk = 0;
        rtl.top.eval();
        resumed = resumed || !rtl.top.trace_halt;
        fetched = fetched || rtl.top.mem_load;
        issued = issued || rtl.top.trace_issue;
        read = read || rtl.top.dmem_read;
        rtl.clock();
    }
    if (resumed) out.add(label + ".halt-held", 1, 0);
    if (fetched) out.add(label + ".fetch-after-halt", 0, 1);
    if (issued) out.add(label + ".issue-after-halt", 0, 1);
    if (read) out.add(label + ".dmem-read-after-halt", 0, 1);
}

// A directed case runs its code with the vector table's #DE, #SS and #GP
// entries at a HLT. A fault case must deliver its vector (#GP(0) unless
// given), pushing the faulting instruction's IP and CS, then halt in the
// handler and stay stopped. A nested case must stop at a fault in its
// delivery, with nothing written. The divergence list applies, so a #DE's
// pushed FLAGS follow the 386EX.
constexpr uint32_t HANDLER = 0x800;  // 0000:0800

void run_fault_case(fes_i386::Oracle &oracle, OracleBus &bus, Rtl &rtl, const std::vector<Entry> &entries,
                    Summary &summary, const std::string &label, const std::vector<uint8_t> &code, State state,
                    bool nested, uint32_t stack_address = 0, const std::array<uint8_t, 4> *stack_value = nullptr,
                    int expected = 13) {
    const uint32_t code_base = state.seg[fes_i386::SEG_CS].base + state.eip;
    std::vector<std::pair<uint32_t, uint8_t>> image;
    for (size_t i = 0; i < code.size(); ++i) image.emplace_back(code_base + uint32_t(i), code[i]);
    image.emplace_back(HANDLER, 0xf4);
    for (uint32_t vector : {0u, 12u, 13u}) {
        image.emplace_back(vector * 4, uint8_t(HANDLER));
        image.emplace_back(vector * 4 + 1, uint8_t(HANDLER >> 8));
    }
    if (stack_value)
        for (unsigned i = 0; i < stack_value->size(); ++i) image.emplace_back(stack_address + i, (*stack_value)[i]);
    for (auto [address, value] : image) {
        bus.ram[address] = rtl.ram[address] = value;
        oracle.invalidate(address, 1);
    }
    oracle.set_state(state);
    rtl.start(state);

    Mismatches out;
    Outcome outcome;
    try {
        outcome = lockstep(oracle, bus, rtl, entries, Fields{}, 64, false, summary, out,
                           code_base, code_base + uint32_t(code.size()));
    } catch (const std::exception &error) {
        out.add(label + ".oracle-error:" + error.what(), 0, 1);
        oracle.reset();
    }
    if (nested) {
        if (!outcome.double_fault) out.add(label + ".double-fault", 1, 0);
    } else {
        if (outcome.exception != expected)
            out.add(label + ".exception", uint32_t(expected), outcome.exception < 0 ? 0xffu : uint32_t(outcome.exception));
        if (!outcome.halted) out.add(label + ".halt", 1, 0);
        // The frame below the faulting instruction's SP: its IP, then CS.
        const uint32_t sp = (state.gpr[fes_i386::REG_ESP] - 6) & 0xffff;
        const uint32_t frame = state.seg[fes_i386::SEG_SS].base + sp;
        const uint32_t ip = rtl.ram[frame] | uint32_t(rtl.ram[frame + 1]) << 8;
        const uint32_t cs = rtl.ram[frame + 2] | uint32_t(rtl.ram[frame + 3]) << 8;
        if (ip != (state.eip & 0xffff)) out.add(label + ".pushed-ip", state.eip & 0xffff, ip);
        if (cs != state.seg[fes_i386::SEG_CS].selector) out.add(label + ".pushed-cs", state.seg[fes_i386::SEG_CS].selector, cs);
        if ((rtl.state().gpr[fes_i386::REG_ESP] & 0xffff) != sp)
            out.add(label + ".esp", sp, rtl.state().gpr[fes_i386::REG_ESP] & 0xffff);
        if (rtl.top.trace_halt) check_halt_quiet(rtl, label, out);
    }

    summary.cycles += rtl.cycles;
    rtl.cycles = 0;
    rtl.load_waits = 0;
    for (auto [address, value] : image) {
        bus.ram[address] = rtl.ram[address] = 0;
        oracle.invalidate(address, 1);
    }
    for (uint32_t address : rtl.written) {
        if (address < RAM_BYTES) bus.ram[address] = rtl.ram[address] = 0;
        oracle.invalidate(address, 1);
    }
    rtl.written.clear();
    ++summary.runs;
    if (out.items.empty()) {
        ++summary.passed;
    } else {
        ++summary.failed;
        std::cout << "FAIL directed:" << label;
        for (const auto &item : out.items) std::cout << ' ' << item;
        std::cout << '\n';
    }
}

void run_fault_regressions(fes_i386::Oracle &oracle, OracleBus &bus, Rtl &rtl, const std::vector<Entry> &entries,
                           Summary &summary) {
    State call;
    for (int k = 0; k < 6; ++k) call.seg[k] = fes_i386::real_mode_segment(0);
    call.seg[fes_i386::SEG_CS] = fes_i386::real_mode_segment(0x100);
    call.eip = 0;
    // 66 E8 disp32=10000h: CALL's instruction fits, but its full-width target
    // is outside the 16-bit CS limit. Bochs rolls back the speculative push.
    run_fault_case(oracle, bus, rtl, entries, summary, "call-rel32-cs-limit",
                   {0x66, 0xe8, 0x00, 0x00, 0x01, 0x00}, call, false);

    State ret = call;
    ret.gpr[fes_i386::REG_ESP] = 0x200;
    ret.gpr[fes_i386::REG_ESI] = 0x300;
    const std::array<uint8_t, 4> target = {0x00, 0x00, 0x01, 0x00};
    // RET's 32-bit pop is outside CS; the following MOV [SI] is a younger
    // data read, which the delivery discards.
    run_fault_case(oracle, bus, rtl, entries, summary, "ret-rel32-cs-limit",
                   {0x66, 0xc3, 0x8b, 0x04, 0xf4}, ret, false, 0x200, &target);

    State jump = call;
    jump.gpr[fes_i386::REG_EBX] = 0x10000;
    jump.gpr[fes_i386::REG_ESI] = 0x300;
    // JMP EBX has the same rejected target; MOV [SI] follows it in memory.
    run_fault_case(oracle, bus, rtl, entries, summary, "jmp-rm-cs-limit",
                   {0x66, 0xff, 0xe3, 0x8b, 0x04, 0xf4}, jump, false);

    // CALL EBX to the same target: #GP(0) before its push.
    run_fault_case(oracle, bus, rtl, entries, summary, "call-rm32-cs-limit", {0x66, 0xff, 0xd3, 0xf4}, jump, false);

    // INT 20h with SP 1: its probe raises #SS, whose delivery's probe faults
    // the same way.
    State wrap = call;
    wrap.gpr[fes_i386::REG_ESP] = 1;
    run_fault_case(oracle, bus, rtl, entries, summary, "int-stack-wrap", {0xcd, 0x20, 0xf4}, wrap, true);

    // The CALL's #GP with SP 3: the delivery's push of CS would straddle
    // the 64 KiB wrap.
    State gp_wrap = call;
    gp_wrap.gpr[fes_i386::REG_ESP] = 3;
    run_fault_case(oracle, bus, rtl, entries, summary, "gp-stack-wrap",
                   {0x66, 0xe8, 0x00, 0x00, 0x01, 0x00}, gp_wrap, true);

    // Divide errors: #DE with nothing of the instruction written but the
    // flags the 386EX changes first, which its pushed FLAGS show.
    State de = call;
    de.gpr[fes_i386::REG_ESP] = 0x200;
    de.gpr[fes_i386::REG_EAX] = 0x1234;
    de.gpr[fes_i386::REG_EDX] = 0x8765;
    de.gpr[fes_i386::REG_EBX] = 0x4300;  // BL 0
    de.gpr[fes_i386::REG_ESI] = 0x300;
    run_fault_case(oracle, bus, rtl, entries, summary, "div8-by-zero", {0xf6, 0xf3, 0xf4}, de, false, 0, nullptr, 0);
    // DIV BX: DX not below BX, so the quotient overflows.
    run_fault_case(oracle, bus, rtl, entries, summary, "div16-overflow", {0xf7, 0xf3, 0xf4}, de, false, 0, nullptr, 0);
    // DIV WORD [SI] by zero: its LOAD uop reads the operand too.
    const std::array<uint8_t, 4> zero = {0, 0, 0, 0};
    run_fault_case(oracle, bus, rtl, entries, summary, "div16-memory-zero", {0xf7, 0x34, 0xf4}, de, false, 0x300, &zero,
                   0);
    // IDIV EBX: 87651234h:00001234h by 4300h overflows.
    run_fault_case(oracle, bus, rtl, entries, summary, "idiv32-overflow", {0x66, 0xf7, 0xfb, 0xf4}, de, false, 0,
                   nullptr, 0);
    // AAM 0.
    run_fault_case(oracle, bus, rtl, entries, summary, "aam-zero", {0xd4, 0x00, 0xf4}, de, false, 0, nullptr, 0);
    // IDIV BL: AX -129 by 1. The 386EX divides AX's one's complement, 128,
    // so the quotient is 80h until the one the complement owes is added; it
    // overflows only when checked after that.
    State exact = de;
    exact.gpr[fes_i386::REG_EAX] = 0xff7f;
    exact.gpr[fes_i386::REG_EBX] = 0x0001;
    run_fault_case(oracle, bus, rtl, entries, summary, "idiv8-exact-overflow", {0xf6, 0xfb, 0xf4}, exact, false, 0,
                   nullptr, 0);

    // ENTER 10Fh, 0 with SP 110h: its push of BP passes, and the check of
    // the final slot, at SP FFFFh, raises #SS with ESP and EBP unmoved.
    State enter = call;
    enter.gpr[fes_i386::REG_ESP] = 0x110;
    enter.gpr[fes_i386::REG_EBP] = 0x400;
    run_fault_case(oracle, bus, rtl, entries, summary, "enter-final-slot", {0xc8, 0x0f, 0x01, 0x00, 0xf4}, enter,
                   false, 0, nullptr, 12);

    // ENTER 0, 2 with SP 5: the pushes of BP and of the copied slot pass,
    // the frame pointer's straddles the wrap, and the #SS delivery's
    // probe faults the same way, the two pushes written.
    State enter_wrap = enter;
    enter_wrap.gpr[fes_i386::REG_ESP] = 5;
    run_fault_case(oracle, bus, rtl, entries, summary, "enter-stack-wrap", {0xc8, 0x00, 0x00, 0x02, 0xf4},
                   enter_wrap, true);
}

// ---- SingleStepTests mode ---------------------------------------------------------------

constexpr std::array<const char *, 20> MOO_REGISTERS = {
    "cr0", "cr3", "eax", "ebx", "ecx", "edx", "esi", "edi", "ebp", "esp",
    "cs", "ds", "es", "fs", "gs", "ss", "eip", "eflags", "dr6", "dr7"};
using Registers = std::array<uint32_t, MOO_REGISTERS.size()>;

struct Test {
    std::string label;
    Registers initial{}, final{};
    uint32_t final_mask = 0;
    std::vector<std::pair<uint32_t, uint8_t>> initial_ram, final_ram;
    int exception = -1;
    Fields corpus_only, oracle_only;
};

uint32_t read_hex(std::istream &input) {
    std::string token;
    if (!(input >> token)) throw std::runtime_error("truncated test line");
    size_t used = 0;
    unsigned long value = std::stoul(token, &used, 16);
    if (used != token.size() || value > 0xffffffffUL) throw std::runtime_error("bad number: " + token);
    return uint32_t(value);
}

Test parse(const std::string &line) {
    std::istringstream input(line);
    Test test;
    if (!(input >> test.label)) throw std::runtime_error("empty test line");
    for (auto &value : test.initial) value = read_hex(input);
    test.final_mask = read_hex(input);
    test.final = test.initial;
    for (size_t n = 0; n < test.final.size(); ++n)
        if (test.final_mask & (1u << n)) test.final[n] = read_hex(input);
    for (auto *ram : {&test.initial_ram, &test.final_ram}) {
        uint32_t count = read_hex(input);
        if (count > 4096) throw std::runtime_error("RAM list too long");
        for (uint32_t n = 0; n < count; ++n) {
            uint32_t address = read_hex(input), value = read_hex(input);
            if (address >= RAM_BYTES || value > 0xff) throw std::runtime_error("bad RAM entry");
            ram->emplace_back(address, uint8_t(value));
        }
    }
    std::string exception;
    if (!(input >> exception)) throw std::runtime_error("missing exception field");
    test.exception = exception == "-1" ? -1 : int(std::stoul(exception, nullptr, 16));
    for (Fields *fields : {&test.corpus_only, &test.oracle_only}) {
        uint32_t count = read_hex(input);
        for (uint32_t n = 0; n < count; ++n) {
            std::string field;
            if (!(input >> field)) throw std::runtime_error("truncated field list");
            fields->add(field);
        }
    }
    return test;
}

State to_state(const Registers &r) {
    State state;
    state.cr0 = r[0];
    state.cr3 = r[1];
    const int gpr_order[] = {fes_i386::REG_EAX, fes_i386::REG_EBX, fes_i386::REG_ECX, fes_i386::REG_EDX,
                             fes_i386::REG_ESI, fes_i386::REG_EDI, fes_i386::REG_EBP, fes_i386::REG_ESP};
    for (int n = 0; n < 8; ++n) state.gpr[gpr_order[n]] = r[2 + n];
    const int seg_order[] = {fes_i386::SEG_CS, fes_i386::SEG_DS, fes_i386::SEG_ES,
                             fes_i386::SEG_FS, fes_i386::SEG_GS, fes_i386::SEG_SS};
    for (int n = 0; n < 6; ++n) state.seg[seg_order[n]] = fes_i386::real_mode_segment(uint16_t(r[10 + n]));
    state.eip = r[16];
    state.eflags = r[17] & EFLAGS_MASK;
    state.dr6 = r[18];
    state.dr7 = r[19];
    return state;
}

// The RTL's final state against the corpus's: every register and RAM byte
// the corpus lists, except fields that follow the oracle.
void compare_corpus(const Test &test, const State &got, const Rtl &rtl, Mismatches &out) {
    const State want = to_state(test.final);
    Fields skip = test.oracle_only;
    compare(want, got, skip, "corpus.", out);
    (void)rtl;
    for (auto [address, value] : test.final_ram)
        if (!skip.has("ram") && rtl.ram[address] != value) out.add("corpus.ram." + hex(address), value, rtl.ram[address]);
}

// Real-mode segment caches in the oracle's state keep only what the corpus
// gives; the RTL loads the same.
int run_corpus(fes_i386::Oracle &oracle, OracleBus &bus, Rtl &rtl, const std::vector<Entry> &entries,
               Summary &summary) {
    std::string line;
    while (std::getline(std::cin, line)) {
        if (line.empty()) continue;
        Test test = parse(line);
        for (auto [address, value] : test.initial_ram) {
            bus.ram[address] = value;
            rtl.ram[address] = value;
        }
        for (auto [address, value] : test.initial_ram) oracle.invalidate(address, 1);
        const State initial = to_state(test.initial);
        oracle.set_state(initial);
        rtl.start(initial);
        bus.io.reset(true);
        rtl.io.reset(true);

        Mismatches out;
        Outcome outcome;
        try {
            outcome = lockstep(oracle, bus, rtl, entries, test.corpus_only, 64, false, summary, out);
        } catch (const std::exception &error) {
            out.add(std::string("oracle-error:") + error.what(), 0, 0);
            oracle.reset();
        }
        if (out.items.empty()) {
            if (!outcome.halted) {
                out.add("halt", 1, 0);
            } else {
                // An exception or a software interrupt is the corpus's
                // exception: the run goes on into the handler, to its HLT.
                const int taken = outcome.exception >= 0 ? outcome.exception : outcome.interrupt;
                if (taken != test.exception)
                    out.add("corpus.exception", test.exception >= 0 ? uint32_t(test.exception) : 0xffffffffu,
                            taken >= 0 ? uint32_t(taken) : 0xffffffffu);
                compare_corpus(test, rtl.state(), rtl, out);
            }
        }
        if (outcome.halted) {
            ++summary.halts;
            summary.interrupts += outcome.interrupt >= 0;
            if (outcome.exception >= 0) {
                ++summary.faults;
                ++summary.fault_vectors[outcome.exception];
            }
        }
        summary.cycles += rtl.cycles;
        rtl.cycles = 0;
        for (auto [address, value] : test.initial_ram) bus.ram[address] = rtl.ram[address] = 0;
        for (auto [address, value] : test.final_ram) bus.ram[address] = rtl.ram[address] = 0;
        for (uint32_t address : rtl.written) {
            if (address < RAM_BYTES) bus.ram[address] = rtl.ram[address] = 0;
            oracle.invalidate(address, 1);
        }
        rtl.written.clear();
        ++summary.runs;
        if (out.items.empty()) {
            ++summary.passed;
        } else {
            ++summary.failed;
            std::cout << "FAIL " << test.label;
            for (auto &item : out.items) std::cout << ' ' << item;
            std::cout << '\n';
        }
    }
    return 0;
}

// ---- Random programs ----------------------------------------------------------------------

// Instructions of the build, laid out with branch targets on instruction
// boundaries: straight-line code, branches, loops, pushes and pops,
// subroutines and stack frames.
class Generator {
public:
    explicit Generator(std::mt19937_64 &random) : rng(random) {}

    struct Item {
        std::vector<uint8_t> bytes;
        int target = -1;     // branch: the item it jumps to
        unsigned rel_at = 0;  // where the displacement starts
        unsigned rel_size = 0;
        // A far transfer to target: rel_at holds a far pointer (its offset,
        // rel_size bytes, then the selector), or the offset or selector a
        // MOV stores for an indirect one. NEAR_ABS: the target's EIP, which a
        // MOV loads or stores for a CALL r/m.
        enum { NEAR, FAR_PTR, FAR_OFF, FAR_SEL, NEAR_ABS } far = NEAR;
    };

    // base: the program's linear address, which far targets need; eip: its
    // first instruction's EIP, which near absolute targets need.
    std::vector<uint8_t> program(unsigned count, uint32_t base, uint32_t eip) {
        items.clear();
        fence = 0;
        cs_moved = false;
        program_base = base;
        program_eip = eip;
        while (items.size() < count) {
            const unsigned kind = pick(100);
            if (kind < 6) forward_branch();
            else if (kind < 9) loop_block();
            else if (kind < 10) backward_jump();
            else if (kind < 16) items.push_back(Item{stack_op(pick(2), pick(3) == 0, false), -1, 0, 0});
            else if (kind < 19) call_block(0);
            else if (kind < 21) frame_block();
            else if (kind < 22) far_jump();
            else if (kind < 25) ucode_block();
            else if (kind < 27) items.push_back(Item{muldiv_pair(), -1, 0, 0});
            else if (kind < 31) string_block();
            else if (kind < 32) enter_block();
            else if (kind < 34) io_block();
            else if (kind < 35) {  // STI, HLT: the HLT wakes on INTR (Irq)
                items.push_back(Item{{0xfb}, -1, 0, 0});
                items.push_back(Item{{0xf4}, -1, 0, 0});
            } else if (kind < 37) shadow_block();
            else items.push_back(Item{instruction(false), -1, 0, 0});
        }
        items.push_back(Item{{0xf4}, -1, 0, 0});
        return layout();
    }

private:
    std::mt19937_64 &rng;
    std::vector<Item> items;
    int fence = 0;  // no backward jump lands before here: inside a subroutine
    // A far transfer has run code under another CS, where a target's EIP
    // is not program_eip plus its offset.
    bool cs_moved = false;
    uint32_t program_base = 0, program_eip = 0;

    unsigned pick(unsigned n) { return unsigned(rng() % n); }
    uint8_t byte() { return uint8_t(rng()); }

    // Up to three segment or REP prefixes, then 66h and 67h at random, and
    // now and then a LOCK, which is #UD on every instruction here.
    void prefixes(std::vector<uint8_t> &out, bool &os32) {
        static const uint8_t harmless[] = {0x26, 0x2e, 0x36, 0x3e, 0x64, 0x65, 0xf2, 0xf3};
        os32 = false;
        for (int n = 0; n < 3 && pick(5) == 0; ++n) out.push_back(harmless[pick(8)]);
        if (pick(400) == 0) out.push_back(0xf0);
        if (pick(3) == 0) {
            out.push_back(0x66);
            os32 = true;
        }
        if (pick(5) == 0) out.push_back(0x67);
    }
    void imm(std::vector<uint8_t> &out, unsigned bytes) {
        for (unsigned i = 0; i < bytes; ++i) out.push_back(byte());
    }
    // One non-branch instruction; keep_ecx avoids writing ECX (a loop body),
    // keep_stack writing ESP, SS or memory (a subroutine's body, which must
    // leave its return address where RET finds it).
    std::vector<uint8_t> instruction(bool keep_ecx, bool keep_stack = false) {
        for (;;) {
            std::vector<uint8_t> out;
            bool os32;
            prefixes(out, os32);
            const unsigned z = os32 ? 4 : 2;
            const unsigned form = pick(26);
            bool memory = pick(3) == 0;  // the r/m operand becomes a memory one
            unsigned dst = 8;  // the GPR it writes (8: none or checked below)
            bool dst_byte = false;
            unsigned dst2 = 8;  // a second GPR it writes (XCHG), of the same size
            switch (form) {
            case 0: case 1: case 2: {  // ALU r/m, r and r, r/m
                const uint8_t op = uint8_t((pick(8) << 3) | pick(4));
                const uint8_t modrm = uint8_t(0xc0 | pick(64));
                out.push_back(op);
                out.push_back(modrm);
                if ((op >> 3) != 7) dst = op & 2 ? (modrm >> 3) & 7 : modrm & 7;
                dst_byte = !(op & 1);
                break;
            }
            case 3: {  // ALU AL/eAX, imm
                const uint8_t op = uint8_t((pick(8) << 3) | (4 + pick(2)));
                out.push_back(op);
                imm(out, op & 1 ? z : 1);
                if ((op >> 3) != 7) dst = 0;
                break;
            }
            case 4: {  // group 1, imm
                const uint8_t op = uint8_t(0x80 + pick(4));
                const uint8_t modrm = uint8_t(0xc0 | pick(64));
                out.push_back(op);
                out.push_back(modrm);
                imm(out, op == 0x81 ? z : 1);
                if (((modrm >> 3) & 7) != 7) dst = modrm & 7;
                dst_byte = op == 0x80 || op == 0x82;
                break;
            }
            case 5: {  // INC, DEC, NOT, NEG, TEST
                const unsigned which = pick(4);
                if (which == 0) {
                    out.push_back(uint8_t(0x40 + pick(16)));
                    dst = out.back() & 7;
                } else if (which == 1) {
                    out.push_back(uint8_t(0xfe + pick(2)));
                    out.push_back(uint8_t(0xc0 | (pick(2) << 3) | pick(8)));
                    dst = out.back() & 7;
                    dst_byte = out[out.size() - 2] == 0xfe;
                } else {
                    const uint8_t op = uint8_t(0xf6 + pick(2));
                    const unsigned reg = pick(4);
                    out.push_back(op);
                    out.push_back(uint8_t(0xc0 | (reg << 3) | pick(8)));
                    if (reg < 2) imm(out, op == 0xf7 ? z : 1);
                    else dst = out[out.size() - 1] & 7;
                    dst_byte = op == 0xf6;
                }
                break;
            }
            case 6: {  // TEST r/m, r and accumulator
                if (pick(2)) {
                    out.push_back(uint8_t(0x84 + pick(2)));
                    out.push_back(uint8_t(0xc0 | pick(64)));
                } else {
                    out.push_back(uint8_t(0xa8 + pick(2)));
                    imm(out, out.back() == 0xa9 ? z : 1);
                }
                break;
            }
            case 7: case 8: {  // MOV
                const unsigned which = pick(3);
                if (which == 0) {
                    const uint8_t op = uint8_t(0x88 + pick(4));
                    const uint8_t modrm = uint8_t(0xc0 | pick(64));
                    out.push_back(op);
                    out.push_back(modrm);
                    dst = op & 2 ? (modrm >> 3) & 7 : modrm & 7;
                    dst_byte = !(op & 1);
                } else if (which == 1) {
                    const uint8_t op = uint8_t(0xb0 + pick(16));
                    out.push_back(op);
                    imm(out, op >= 0xb8 ? z : 1);
                    dst = op & 7;
                    dst_byte = op < 0xb8;
                } else {
                    out.push_back(uint8_t(0xc6 + pick(2)));
                    out.push_back(uint8_t(0xc0 | pick(8)));
                    dst = out.back() & 7;
                    dst_byte = out[out.size() - 2] == 0xc6;
                    imm(out, out[out.size() - 2] == 0xc7 ? z : 1);
                }
                break;
            }
            case 9: {  // MOV r/m, Sreg and MOV Sreg, r/m
                if (pick(2)) {
                    out.push_back(0x8c);
                    out.push_back(uint8_t(0xc0 | (pick(6) << 3) | pick(8)));
                    dst = out.back() & 7;
                } else {
                    static const uint8_t segs[] = {0, 2, 3, 4, 5};
                    out.push_back(0x8e);
                    out.push_back(uint8_t(0xc0 | (segs[pick(5)] << 3) | pick(8)));
                }
                break;
            }
            case 10: {  // LEA
                out.push_back(0x8d);
                const bool as32 = std::count(out.begin(), out.end(), 0x67) != 0;
                const unsigned mod = pick(3), reg = pick(8), rm = pick(8);
                out.push_back(uint8_t((mod << 6) | (reg << 3) | rm));
                dst = reg;
                if (as32) {
                    if (rm == 4) {
                        const uint8_t sib = byte();
                        out.push_back(sib);
                        if (mod == 0 && (sib & 7) == 5) imm(out, 4);
                    }
                    imm(out, mod == 0 ? (rm == 5 ? 4 : 0) : mod == 1 ? 1 : 4);
                } else {
                    imm(out, mod == 0 ? (rm == 6 ? 2 : 0) : mod == 1 ? 1 : 2);
                }
                break;
            }
            case 11: case 12: {  // shifts and rotates
                static const uint8_t ops[] = {0xc0, 0xc1, 0xd0, 0xd1, 0xd2, 0xd3};
                const uint8_t op = ops[pick(6)];
                out.push_back(op);
                out.push_back(uint8_t(0xc0 | pick(64)));
                dst = out.back() & 7;
                dst_byte = !(op & 1);
                if (op <= 0xc1) out.push_back(uint8_t(pick(4) ? pick(18) : pick(256)));
                break;
            }
            case 13: {  // SHLD, SHRD
                static const uint8_t ops[] = {0xa4, 0xa5, 0xac, 0xad};
                const uint8_t op = ops[pick(4)];
                out.push_back(0x0f);
                out.push_back(op);
                out.push_back(uint8_t(0xc0 | pick(64)));
                dst = out.back() & 7;
                if (op == 0xa4 || op == 0xac) out.push_back(uint8_t(pick(4) ? pick(34) : pick(256)));
                break;
            }
            case 14: {  // SETcc
                out.push_back(0x0f);
                out.push_back(uint8_t(0x90 + pick(16)));
                out.push_back(uint8_t(0xc0 | pick(64)));
                dst = out.back() & 7;
                dst_byte = true;
                break;
            }
            case 15: {  // MOVZX, MOVSX
                static const uint8_t ops[] = {0xb6, 0xb7, 0xbe, 0xbf};
                out.push_back(0x0f);
                out.push_back(ops[pick(4)]);
                out.push_back(uint8_t(0xc0 | pick(64)));
                dst = (out.back() >> 3) & 7;
                break;
            }
            case 16: {  // CBW, CWD, SAHF, LAHF
                static const uint8_t ops[] = {0x98, 0x99, 0x9e, 0x9f};
                out.push_back(ops[pick(4)]);
                dst = out.back() == 0x99 ? 2 : out.back() == 0x9e ? 8 : 0;
                break;
            }
            case 17: {  // flags and NOP
                static const uint8_t ops[] = {0xf5, 0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0x90};
                out.push_back(ops[pick(8)]);
                break;
            }
            case 18: {  // LES, LDS, LSS, LFS, LGS: memory only
                static const uint8_t two_byte[] = {0xb2, 0xb4, 0xb5};
                const unsigned which = pick(5);
                if (which < 2) {
                    out.push_back(uint8_t(0xc4 + which));
                } else {
                    out.push_back(0x0f);
                    out.push_back(two_byte[which - 2]);
                }
                dst = pick(8);
                out.push_back(uint8_t(0xc0 | (dst << 3)));
                memory = true;
                break;
            }
            case 19: {  // XCHG r/m, r, and eAX, r
                if (pick(3)) {
                    out.push_back(uint8_t(0x86 + pick(2)));
                    out.push_back(uint8_t(0xc0 | pick(64)));
                    dst = out.back() & 7;
                    dst2 = (out.back() >> 3) & 7;
                    dst_byte = out[out.size() - 2] == 0x86;
                } else {
                    out.push_back(uint8_t(0x91 + pick(7)));
                    dst = 0;
                    dst2 = out.back() & 7;
                }
                break;
            }
            case 20: {  // MOV with a direct offset, XLAT, SALC: 16-bit addressing, no CS override, no ModRM
                out.erase(std::remove_if(out.begin(), out.end(), [](uint8_t b) { return b == 0x67 || b == 0x2e; }),
                          out.end());
                memory = false;
                const unsigned which = pick(4);
                if (which < 2) {
                    out.push_back(uint8_t(0xa0 + pick(4)));
                    if (!(out.back() & 2)) dst = 0;
                    dst_byte = !(out.back() & 1);
                    imm(out, 2);
                } else {
                    out.push_back(which == 2 ? 0xd7 : 0xd6);
                    dst = 0;
                    dst_byte = true;
                }
                break;
            }
            case 21: {  // MUL, IMUL r/m: AX, DX:AX or EDX:EAX
                const uint8_t op = uint8_t(0xf6 + pick(2));
                out.push_back(op);
                out.push_back(uint8_t(0xc0 | ((4 + pick(2)) << 3) | pick(8)));
                dst = 0;
                dst2 = op == 0xf7 ? 2 : 8;
                break;
            }
            case 22: {  // IMUL r, r/m; IMUL r, r/m, imm16/32 or imm8
                const unsigned which = pick(3);
                if (which == 0) {
                    out.push_back(0x0f);
                    out.push_back(0xaf);
                } else {
                    out.push_back(which == 1 ? 0x69 : 0x6b);
                }
                out.push_back(uint8_t(0xc0 | pick(64)));
                dst = (out.back() >> 3) & 7;
                if (which) imm(out, which == 1 ? z : 1);
                break;
            }
            case 23: {  // AAM (now and then AAM 0, a divide error), AAD
                const unsigned which = pick(16);
                out.push_back(uint8_t(0xd4 + (which & 1)));
                out.push_back(which == 0 && pick(64) == 0 ? uint8_t(0) : pick(4) ? uint8_t(10) : uint8_t(1 + pick(255)));
                dst = 0;
                break;
            }
            default: {  // MOV r, imm32 keeps values wide
                out.push_back(uint8_t(0xb8 + pick(8)));
                dst = out.back() & 7;
                imm(out, z);
                break;
            }
            }
            if (memory) to_memory(out, dst);
            auto writes_ecx = [&](unsigned r) { return r != 8 && (dst_byte ? (r & 3) == 1 : r == 1); };
            if (keep_ecx && (writes_ecx(dst) || writes_ecx(dst2))) continue;
            if (keep_stack) {
                unsigned i = 0;
                while (i < out.size() && is_prefix(out[i])) ++i;
                if ((!dst_byte && (dst == 4 || dst2 == 4)) || mem_use(out.data()).writes ||
                    (out[i] == 0x8e && ((out[i + 1] >> 3) & 7) == 2) || (out[i] == 0x0f && out[i + 1] == 0xb2))
                    continue;
            }
            return out;
        }
    }

    // The instruction's register r/m operand made a 16-bit-addressed memory
    // one, where this build has the memory form: a random ModRM mod and r/m
    // and its displacement. 67h prefixes go, since a 32-bit offset from
    // random registers would nearly always pass the limit, and so do CS
    // overrides, which would store into the code segment.
    void to_memory(std::vector<uint8_t> &out, unsigned &dst) {
        unsigned i = 0;
        while (i < out.size() && is_prefix(out[i])) ++i;
        if (i + 1 >= out.size()) return;
        const bool two = out[i] == 0x0f;
        unsigned at = i + (two ? 2 : 1);
        if (at >= out.size() || (out[at] >> 6) != 3) return;
        const uint8_t saved = out[at];
        const unsigned mod = pick(3), rm = pick(8);
        out[at] = uint8_t((mod << 6) | (saved & 0x38) | rm);
        if (!mem_use(out.data()).any()) {
            out[at] = saved;
            return;
        }
        for (unsigned k = 0; k < i;) {
            if (out[k] == 0x67 || out[k] == 0x2e) {
                out.erase(out.begin() + k);
                --i;
                --at;
            } else {
                ++k;
            }
        }
        const unsigned disp = mod == 1 ? 1 : mod == 2 || rm == 6 ? 2 : 0;
        for (unsigned n = 0; n < disp; ++n) out.insert(out.begin() + at + 1, byte());
        if (mem_use(out.data()).writes) dst = 8;
    }

    // DIV or IDIV after the setup that keeps its quotient in range: AH or
    // (E)DX cleared for DIV; AL or AX sign-extended (CBW, CWD, CDQ) for IDIV.
    // The divisor is a register, half the time loaded with a nonzero
    // immediate first and now and then the one the setup wrote, or a direct
    // memory operand a MOV stores first (memory is mostly zero). One item, so
    // no branch lands between them. A few have no setup: their quotient
    // overflows more often than not (#DE).
    std::vector<uint8_t> muldiv_pair() {
        std::vector<uint8_t> out;
        const bool wide = pick(3) != 0, os32 = wide && pick(3) == 0, sgn = pick(2), raw = pick(64) == 0;
        const bool memory = !raw && pick(2);
        const unsigned reg = 6 + unsigned(sgn), setup = wide ? 2 : 4;
        const uint16_t at = uint16_t(rng());
        // Rarely the register the setup wrote: (E)DX, or AH for a byte.
        unsigned rm = pick(8);
        while (rm == setup && pick(64)) rm = pick(8);
        if (!memory && !raw && pick(2)) {
            if (os32) out.push_back(0x66);
            out.push_back(uint8_t((wide ? 0xb8 : 0xb0) + rm));
            out.push_back(uint8_t(1 + pick(255)));
            imm(out, wide ? (os32 ? 3 : 1) : 0);
        }
        if (memory) {
            if (os32) out.push_back(0x66);
            out.insert(out.end(), {uint8_t(wide ? 0xc7 : 0xc6), 0x06, uint8_t(at), uint8_t(at >> 8)});
            imm(out, wide ? (os32 ? 4 : 2) : 1);
        }
        if (!raw) {
            if (os32) out.push_back(0x66);
            if (sgn) out.push_back(wide ? 0x99 : 0x98);
            else if (wide) out.insert(out.end(), {0x31, 0xd2});
            else out.insert(out.end(), {0x30, 0xe4});
        }
        if (os32) out.push_back(0x66);
        out.push_back(wide ? 0xf7 : 0xf6);
        if (memory) out.insert(out.end(), {uint8_t(0x06 | (reg << 3)), uint8_t(at), uint8_t(at >> 8)});
        else out.push_back(uint8_t(0xc0 | (reg << 3) | rm));
        return out;
    }

    // PUSH r, r/m, imm or Sreg, or POP r, r/m or Sreg, with 66h when os32.
    // keep_stack (a subroutine's body) pops into neither ESP, EBP, SS nor
    // memory. A memory r/m is 16-bit addressed, without a CS override.
    std::vector<uint8_t> stack_op(bool push, bool os32, bool keep_stack) {
        static const uint8_t harmless[] = {0x26, 0x2e, 0x36, 0x3e, 0x64, 0x65, 0xf2, 0xf3};
        std::vector<uint8_t> out;
        const unsigned form = pick(5);
        if (pick(6) == 0) {
            const uint8_t prefix = harmless[pick(8)];
            if (form != 4 || prefix != 0x2e) out.push_back(prefix);
        }
        if (pick(6) == 0 && form != 4) out.push_back(0x67);
        if (os32) out.push_back(0x66);
        if (form == 4) {
            // FF /6 or 8F /0: a register, or memory unless a body pops.
            const bool memory = pick(2) && (push || !keep_stack);
            unsigned r = pick(8);
            while (!push && keep_stack && (r == 4 || r == 5)) r = pick(8);
            out.push_back(push ? 0xff : 0x8f);
            if (memory) {
                const unsigned mod = pick(3), rm = pick(8);
                out.push_back(uint8_t((mod << 6) | (push ? 0x30 : 0) | rm));
                imm(out, mod == 1 ? 1 : mod == 2 || (mod == 0 && rm == 6) ? 2 : 0);
            } else {
                out.push_back(uint8_t(0xc0 | (push ? 0x30 : 0) | r));
            }
        } else if (push && form == 2) {
            const bool wide = pick(2);
            out.push_back(wide ? 0x68 : 0x6a);
            imm(out, wide ? (os32 ? 4 : 2) : 1);
        } else if (form == 3) {
            static const uint8_t pushes[] = {0x06, 0x0e, 0x16, 0x1e, 0xa0, 0xa8};
            static const uint8_t pops[] = {0x07, 0x1f, 0xa1, 0xa9, 0x17};
            const uint8_t op = push ? pushes[pick(6)] : pops[pick(keep_stack ? 4 : 5)];
            if (op >= 0xa0) out.push_back(0x0f);
            out.push_back(op);
        } else {
            unsigned r = pick(8);
            while (!push && keep_stack && (r == 4 || r == 5)) r = pick(8);
            out.push_back(uint8_t((push ? 0x50 : 0x58) + r));
        }
        return out;
    }

    // CALL a subroutine laid out past a JMP over it: CALL L; JMP over; L:
    // body; RET or RET imm16 (RETF or RETF imm16 after a far CALL); over:. The body leaves ESP, SS and the return
    // address alone, or pushes and pops the same size back; it may call
    // another, two deep, which returns with a plain RET so the outer return
    // address stays where its RET pops.
    // A quarter are far: CALL ptr, or CALL m16:16/32 through a pointer
    // stored just before, returning with RETF or RETF imm16. A quarter are
    // CALL r/m near, through a register a MOV loads or a word or dword one
    // stores just before, until a far transfer has moved CS.
    void call_block(unsigned depth) {
        const bool os32 = pick(4) == 0;
        const unsigned kind = pick(cs_moved ? 9 : 12);  // 0-5 rel, 6-7 ptr, 8 far indirect, 9-10 register, 11 memory
        const bool far = kind >= 6 && kind <= 8;
        cs_moved = cs_moved || far;
        int call = int(items.size());
        if (kind < 6) {
            items.push_back(rel_item(os32, 0xe8));
        } else if (kind < 8) {
            items.push_back(ptr_item(os32, 0x9a));
        } else if (kind == 8) {
            call = store_pointer(os32);
            items.push_back(indirect_item(os32, 0x1e));
        } else {
            // MOV r, target (not SP) and CALL r; or MOV [disp16], target and
            // CALL [disp16].
            const unsigned r = kind < 11 ? (pick(7) + 5) % 8 : 0;
            Item mov;
            if (os32) mov.bytes.push_back(0x66);
            if (kind < 11) {
                mov.bytes.push_back(uint8_t(0xb8 + r));
            } else {
                pointer = uint16_t(rng());
                mov.bytes.insert(mov.bytes.end(), {0xc7, 0x06, uint8_t(pointer), uint8_t(pointer >> 8)});
            }
            mov.rel_at = unsigned(mov.bytes.size());
            mov.rel_size = os32 ? 4 : 2;
            mov.bytes.insert(mov.bytes.end(), mov.rel_size, 0);
            mov.far = Item::NEAR_ABS;
            items.push_back(mov);
            Item via;
            if (os32) via.bytes.push_back(0x66);
            if (kind < 11) via.bytes.insert(via.bytes.end(), {0xff, uint8_t(0xd0 | r)});
            else via.bytes.insert(via.bytes.end(), {0xff, 0x16, uint8_t(pointer), uint8_t(pointer >> 8)});
            items.push_back(via);
        }
        const int jump = int(items.size());
        items.push_back(rel_item(pick(4) == 0, 0xe9));
        if (kind < 6 || kind > 8) items[size_t(call)].target = int(items.size());
        else set_far_target(call, int(items.size()));
        const unsigned length = pick(5);
        for (unsigned n = 0; n < length; ++n) {
            const unsigned kind = pick(8);
            if (kind == 0 && depth < 2) {
                call_block(depth + 1);
            } else if (kind == 1) {
                const bool pair32 = pick(3) == 0;
                items.push_back(Item{stack_op(true, pair32, true), -1, 0, 0});
                if (pick(2)) items.push_back(Item{instruction(false, true), -1, 0, 0});
                items.push_back(Item{stack_op(false, pair32, true), -1, 0, 0});
            } else {
                items.push_back(Item{instruction(false, true), -1, 0, 0});
            }
        }
        Item ret;
        if (os32) ret.bytes.push_back(0x66);
        const uint8_t ret_op = far ? 0xca : 0xc2;
        if (depth == 0 && pick(3) == 0) {
            const uint16_t n = pick(4) ? uint16_t(2 * pick(5)) : uint16_t(rng());
            ret.bytes.insert(ret.bytes.end(), {ret_op, uint8_t(n), uint8_t(n >> 8)});
        } else {
            ret.bytes.push_back(uint8_t(ret_op + 1));
        }
        items.push_back(ret);
        items[size_t(jump)].target = int(items.size());
        fence = int(items.size());
    }

    // PUSH (E)BP; MOV (E)BP, (E)SP; a body that may push; LEAVE, all of one
    // operand size.
    void frame_block() {
        const bool os32 = pick(3) == 0;
        auto add = [&](std::vector<uint8_t> bytes) {
            if (os32) bytes.insert(bytes.begin(), 0x66);
            items.push_back(Item{bytes, -1, 0, 0});
        };
        add({0x55});
        add(pick(2) ? std::vector<uint8_t>{0x89, 0xe5} : std::vector<uint8_t>{0x8b, 0xec});
        const unsigned length = pick(5);
        for (unsigned n = 0; n < length; ++n)
            items.push_back(Item{pick(3) == 0 ? stack_op(true, pick(3) == 0, false) : instruction(false), -1, 0, 0});
        add({0xc9});
    }

    // A string instruction, or now and then two or three back to back, with
    // SI and DI (ESI and EDI under 67h) set first at random below 64 KiB,
    // CX (ECX) to 0-9 under REP, now and then CLD or STD, a source segment
    // override, and REP, REPE or REPNE. A quarter are a REP MOVS or CMPS
    // over two words of nonzero data at SI, whose copy overlaps: DI = SI - 4
    // to SI + 4 and ES = DS, which PUSH DS and POP ES set just before it, so
    // one element can load what the element before it stores, and the
    // string's first uop waits for ES. No backward jump lands before the
    // block's end, where a REP would run again with whatever CX holds.
    void string_block() {
        string_items();
        fence = int(items.size());
    }
    void string_items() {
        static const uint8_t segs[] = {0x26, 0x2e, 0x36, 0x3e, 0x64, 0x65};
        static const uint8_t ops[] = {0xa4, 0xa5, 0xa6, 0xa7, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf};
        if (pick(4) == 0) {
            const uint16_t si = uint16_t(rng()), w0 = uint16_t(rng() | 1), w1 = uint16_t(rng() | 1);
            items.push_back(Item{{0xbe, uint8_t(si), uint8_t(si >> 8)}, -1, 0, 0});            // MOV SI, imm16
            items.push_back(Item{{0xc7, 0x04, uint8_t(w0), uint8_t(w0 >> 8)}, -1, 0, 0});        // MOV [SI], imm16
            items.push_back(Item{{0xc7, 0x44, 0x02, uint8_t(w1), uint8_t(w1 >> 8)}, -1, 0, 0});  // MOV [SI + 2], imm16
            items.push_back(Item{{0x8d, 0x7c, uint8_t(int(pick(9)) - 4)}, -1, 0, 0});          // LEA DI, [SI + d8]
            items.push_back(Item{{0xb9, uint8_t(pick(10)), 0}, -1, 0, 0});                       // MOV CX, imm16
            if (pick(3) == 0) items.push_back(Item{{uint8_t(pick(2) ? 0xfc : 0xfd)}, -1, 0, 0});
            items.push_back(Item{{0x1e}, -1, 0, 0});  // PUSH DS
            items.push_back(Item{{0x07}, -1, 0, 0});  // POP ES
            std::vector<uint8_t> bytes{uint8_t(pick(2) ? 0xf3 : 0xf2)};
            if (pick(3) == 0) bytes.push_back(0x66);
            bytes.push_back(uint8_t((pick(3) ? 0xa4 : 0xa6) + pick(2)));
            items.push_back(Item{bytes, -1, 0, 0});
            return;
        }
        const bool as32 = pick(4) == 0;
        auto mov = [&](unsigned r, uint32_t value) {
            std::vector<uint8_t> bytes;
            if (as32) bytes.push_back(0x66);
            bytes.push_back(uint8_t(0xb8 + r));
            for (unsigned i = 0; i < (as32 ? 4u : 2u); ++i) bytes.push_back(uint8_t(value >> (8 * i)));
            items.push_back(Item{bytes, -1, 0, 0});
        };
        if (pick(2)) mov(6, uint16_t(rng()));
        if (pick(2)) mov(7, uint16_t(rng()));
        const bool rep = pick(3) != 0;
        if (rep) mov(1, pick(10));
        if (pick(3) == 0) items.push_back(Item{{uint8_t(pick(2) ? 0xfc : 0xfd)}, -1, 0, 0});
        const unsigned count = pick(4) ? 1 : 2 + pick(2);
        for (unsigned n = 0; n < count; ++n) {
            std::vector<uint8_t> bytes;
            if (pick(4) == 0) bytes.push_back(segs[pick(6)]);
            if (rep) bytes.push_back(pick(2) ? 0xf3 : 0xf2);
            if (pick(3) == 0) bytes.push_back(0x66);
            if (as32) bytes.push_back(0x67);
            bytes.push_back(ops[pick(10)]);
            items.push_back(Item{bytes, -1, 0, 0});
        }
    }

    // One to three INs and OUTs, imm8 and DX forms, of a byte, word or
    // dword, on a few ports, so the INs read what the OUTs wrote (and DX =
    // FFFFh, whose word wraps to port 0). Now and then DX, AH or eAX is
    // written just before, so the I/O uop takes its port, its merge or its
    // data from the WB register. Or a REP INS or OUTS (or one without REP)
    // of up to four elements, with ES = DS so OUTS can read what INS stored.
    void io_block() {
        static const uint16_t ports[] = {0x20, 0x21, 0x60, 0x80, 0xed, 0x3f8, 0xfffe, 0xffff};
        auto port = [&]() { return pick(4) ? ports[pick(8)] : uint16_t(rng()); };
        auto mov_dx = [&](uint16_t value) { items.push_back(Item{{0xba, uint8_t(value), uint8_t(value >> 8)}, -1, 0, 0}); };
        if (pick(4) == 0) {
            const bool as32 = pick(4) == 0;
            auto mov = [&](unsigned r, uint32_t value) {
                std::vector<uint8_t> bytes;
                if (as32) bytes.push_back(0x66);
                bytes.push_back(uint8_t(0xb8 + r));
                for (unsigned i = 0; i < (as32 ? 4u : 2u); ++i) bytes.push_back(uint8_t(value >> (8 * i)));
                items.push_back(Item{bytes, -1, 0, 0});
            };
            mov(6, uint16_t(rng()));
            mov(7, uint16_t(rng()));
            const bool rep = pick(4) != 0;
            if (rep) mov(1, pick(5));
            mov_dx(port());
            if (pick(3) == 0) items.push_back(Item{{uint8_t(pick(2) ? 0xfc : 0xfd)}, -1, 0, 0});
            items.push_back(Item{{0x1e}, -1, 0, 0});  // PUSH DS
            items.push_back(Item{{0x07}, -1, 0, 0});  // POP ES
            for (unsigned n = 0, count = 1 + pick(2); n < count; ++n) {
                std::vector<uint8_t> bytes;
                if (pick(4) == 0) bytes.push_back(uint8_t(0x26 + 8 * pick(4)));
                if (rep) bytes.push_back(0xf3);
                if (pick(3) == 0) bytes.push_back(0x66);
                if (as32) bytes.push_back(0x67);
                bytes.push_back(uint8_t(0x6c + pick(4)));
                items.push_back(Item{bytes, -1, 0, 0});
            }
            fence = int(items.size());
            return;
        }
        for (unsigned n = 0, count = 1 + pick(3); n < count; ++n) {
            const bool dx = pick(2), out = pick(2), wide = pick(3) != 0, os32 = wide && pick(2);
            if (dx && pick(2)) mov_dx(port());
            const unsigned before = pick(6);
            if (before == 0) items.push_back(Item{{0xb4, byte()}, -1, 0, 0});  // MOV AH, imm8
            else if (before == 1) items.push_back(Item{{0xb0, byte()}, -1, 0, 0});  // MOV AL, imm8
            else if (before == 2) items.push_back(Item{{0x66, 0x40}, -1, 0, 0});  // INC EAX
            std::vector<uint8_t> bytes;
            if (pick(5) == 0) bytes.push_back(pick(2) ? 0xf3 : 0x2e);
            if (os32) bytes.push_back(0x66);
            bytes.push_back(uint8_t((dx ? 0xec : 0xe4) | (out ? 2 : 0) | (wide ? 1 : 0)));
            if (!dx) bytes.push_back(uint8_t(port()));
            items.push_back(Item{bytes, -1, 0, 0});
        }
    }

    // The interrupt shadow's cases, back to back, so an interrupt raised at
    // one meets the next (Irq raises one at them more often): MOV SS twice,
    // whose second does not arm the shadow again; POP SS twice; MOV SS, then
    // STI; CLI, STI, STI; STI, then POP SS. SS takes its own value back, and
    // AX and the stack are left as they were but AX's value.
    void shadow_block() {
        static const std::vector<std::vector<uint8_t>> forms = {
            {0x8c, 0xd0, 0x8e, 0xd0, 0x8e, 0xd0},  // MOV AX, SS; MOV SS, AX; MOV SS, AX
            {0x16, 0x16, 0x17, 0x17},              // PUSH SS; PUSH SS; POP SS; POP SS
            {0x8c, 0xd0, 0x8e, 0xd0, 0xfb},        // MOV AX, SS; MOV SS, AX; STI
            {0xfa, 0xfb, 0xfb},                    // CLI; STI; STI
            {0xfb, 0x16, 0x17},                    // STI; PUSH SS; POP SS
        };
        const std::vector<uint8_t> &form = forms[pick(unsigned(forms.size()))];
        for (size_t at = 0; at < form.size();) {
            const size_t length = form[at] == 0x8c || form[at] == 0x8e ? 2 : 1;
            items.push_back(Item{std::vector<uint8_t>(form.begin() + long(at), form.begin() + long(at + length)), -1, 0, 0});
            at += length;
        }
        items.push_back(Item{instruction(false), -1, 0, 0});
    }

    // ENTER imm16, level (mostly 0-4, now and then any byte); a body that
    // may push; LEAVE, all of one operand size.
    void enter_block() {
        const bool os32 = pick(3) == 0;
        const uint16_t size = pick(3) ? uint16_t(2 * pick(8)) : uint16_t(rng());
        const uint8_t level = uint8_t(pick(4) ? pick(5) : pick(256));
        std::vector<uint8_t> enter;
        if (os32) enter.push_back(0x66);
        enter.insert(enter.end(), {0xc8, uint8_t(size), uint8_t(size >> 8), level});
        items.push_back(Item{enter, -1, 0, 0});
        const unsigned length = pick(4);
        for (unsigned n = 0; n < length; ++n)
            items.push_back(Item{pick(3) == 0 ? stack_op(true, pick(3) == 0, false) : instruction(false), -1, 0, 0});
        items.push_back(Item{os32 ? std::vector<uint8_t>{0x66, 0xc9} : std::vector<uint8_t>{0xc9}, -1, 0, 0});
    }

    // The microcode routines' instructions: PUSHA and POPA, alone or around
    // a body that leaves the stack alone; PUSHF and POPF only as such a pair
    // (a POPF of random data could set TF, and single-step traps are not
    // built); or an interrupt block.
    void ucode_block() {
        const unsigned kind = pick(5);
        const bool os32 = pick(3) == 0;
        auto add = [&](std::vector<uint8_t> bytes) {
            if (os32) bytes.insert(bytes.begin(), 0x66);
            items.push_back(Item{bytes, -1, 0, 0});
        };
        auto body = [&]() {
            const unsigned length = pick(4);
            for (unsigned n = 0; n < length; ++n) items.push_back(Item{instruction(false, true), -1, 0, 0});
        };
        if (kind == 0) {
            add({uint8_t(pick(2) ? 0x60 : 0x61)});
        } else if (kind == 1) {
            add({0x60});
            body();
            add({0x61});
            fence = int(items.size());
        } else if (kind == 2) {
            add({0x9c});
            body();
            add({0x9d});
            fence = int(items.size());  // a POPF without its PUSHF could set TF
        } else {
            interrupt_block();
        }
    }

    // SS above the vector table, ES = 0, the vector's entry stored as the
    // handler's CS:IP, then INT n (vectors 20h-FFh), INT3 or INTO; a JMP
    // over the handler, whose body leaves the stack alone and IRETs. INTO
    // with OF clear goes to the JMP. A stack over the vector's entry would
    // take the 386EX's and Bochs's different orders of the entry's read and
    // the pushes (divergence interrupt-vector-before-push).
    void interrupt_block() {
        cs_moved = true;
        const unsigned form = pick(4);
        const uint8_t vector = form == 0 ? 3 : form == 1 ? 4 : uint8_t(0x20 + pick(0xe0));
        const uint16_t ss = uint16_t(0x100 + pick(0xef00));
        items.push_back(Item{{0x68, uint8_t(ss), uint8_t(ss >> 8)}, -1, 0, 0});  // PUSH imm16
        items.push_back(Item{{0x17}, -1, 0, 0});                                // POP SS
        items.push_back(Item{{0x6a, 0x00}, -1, 0, 0});  // PUSH 0
        items.push_back(Item{{0x07}, -1, 0, 0});        // POP ES
        const int at = int(items.size());
        for (unsigned part = 0; part < 2; ++part) {
            const uint16_t where = uint16_t(vector * 4 + 2 * part);
            Item mov;
            mov.bytes = {0x26, 0xc7, 0x06, uint8_t(where), uint8_t(where >> 8), 0, 0};
            mov.rel_at = 5;
            mov.rel_size = 2;
            mov.far = part ? Item::FAR_SEL : Item::FAR_OFF;
            items.push_back(mov);
        }
        if (form == 0) items.push_back(Item{{0xcc}, -1, 0, 0});
        else if (form == 1) items.push_back(Item{{0xce}, -1, 0, 0});
        else items.push_back(Item{{0xcd, vector}, -1, 0, 0});
        const int jump = int(items.size());
        items.push_back(rel_item(false, 0xe9));
        set_far_target(at, int(items.size()));
        const unsigned length = pick(4);
        for (unsigned n = 0; n < length; ++n) items.push_back(Item{instruction(false, true), -1, 0, 0});
        items.push_back(Item{{0xcf}, -1, 0, 0});
        items[size_t(jump)].target = int(items.size());
        fence = int(items.size());
    }

    // A far JMP over 0-3 instructions: JMP ptr, or JMP m16:16/32 through a
    // pointer stored just before. Code after it runs with the new CS.
    void far_jump() {
        cs_moved = true;
        const bool os32 = pick(4) == 0;
        const bool indirect = pick(3) == 0;
        const int at = indirect ? store_pointer(os32) : int(items.size());
        if (indirect) items.push_back(indirect_item(os32, 0x2e));
        else items.push_back(ptr_item(os32, 0xea));
        const unsigned skip = pick(4);
        for (unsigned n = 0; n < skip; ++n) items.push_back(Item{instruction(false), -1, 0, 0});
        set_far_target(at, int(items.size()));
    }

    // JMP or CALL ptr16:16/32, its pointer filled in at layout.
    Item ptr_item(bool os32, uint8_t op) {
        Item item;
        if (os32) item.bytes.push_back(0x66);
        item.bytes.push_back(op);
        item.rel_at = unsigned(item.bytes.size());
        item.rel_size = os32 ? 4 : 2;
        item.bytes.insert(item.bytes.end(), item.rel_size + 2, 0);
        item.far = Item::FAR_PTR;
        return item;
    }
    // JMP or CALL m16:16/32 (modrm 2Eh or 1Eh: [disp16], DS) at the pointer
    // store_pointer chose.
    Item indirect_item(bool os32, uint8_t modrm) {
        Item item;
        if (os32) item.bytes.push_back(0x66);
        item.bytes.insert(item.bytes.end(), {0xff, modrm, uint8_t(pointer), uint8_t(pointer >> 8)});
        return item;
    }
    // MOV [disp16], offset; MOV [disp16 + size], selector: the pointer an
    // indirect far transfer reads, at a random DS offset. Returns the first
    // MOV's item, whose target set_far_target sets for both.
    int store_pointer(bool os32) {
        pointer = uint16_t(rng());
        const int at = int(items.size());
        Item off;
        if (os32) off.bytes.push_back(0x66);
        off.bytes.insert(off.bytes.end(), {0xc7, 0x06, uint8_t(pointer), uint8_t(pointer >> 8)});
        off.rel_at = unsigned(off.bytes.size());
        off.rel_size = os32 ? 4 : 2;
        off.bytes.insert(off.bytes.end(), off.rel_size, 0);
        off.far = Item::FAR_OFF;
        items.push_back(off);
        const uint16_t at_sel = uint16_t(pointer + (os32 ? 4 : 2));
        Item sel;
        sel.bytes = {0xc7, 0x06, uint8_t(at_sel), uint8_t(at_sel >> 8), 0, 0};
        sel.rel_at = 4;
        sel.rel_size = 2;
        sel.far = Item::FAR_SEL;
        items.push_back(sel);
        return at;
    }
    uint16_t pointer = 0;
    // The far transfer at items[at] (or the pointer stored there and the
    // transfer after it) goes to items[target].
    void set_far_target(int at, int target) {
        for (int n = at; n < int(items.size()) && items[size_t(n)].far != Item::NEAR; ++n)
            items[size_t(n)].target = target;
    }
    // A CS:IP for a linear address: the selector 2000h below its paragraph,
    // so the offset stays near 8000h and the code after it well inside CS.
    static void far_point(uint32_t linear, uint16_t &sel, uint32_t &off) {
        const uint32_t para = linear >> 4;
        const uint32_t s = para > 0x800 ? std::min<uint32_t>(para - 0x800, 0xffff) : 0;
        sel = uint16_t(s);
        off = linear - (s << 4);
    }

    // CALL or JMP rel16/32 (rel32 with 66h).
    Item rel_item(bool os32, uint8_t op) {
        Item item;
        if (os32) item.bytes.push_back(0x66);
        item.bytes.push_back(op);
        item.rel_at = unsigned(item.bytes.size());
        item.rel_size = os32 ? 4 : 2;
        item.bytes.insert(item.bytes.end(), item.rel_size, 0);
        return item;
    }

    // A conditional or unconditional jump, or JCXZ, over 0-3 instructions.
    void forward_branch() {
        Item branch = jump_item(pick(3));
        const int at = int(items.size());
        items.push_back(branch);
        const unsigned skip = pick(4);
        for (unsigned n = 0; n < skip; ++n) items.push_back(Item{instruction(false), -1, 0, 0});
        items[size_t(at)].target = int(items.size());
    }

    // MOV (E)CX, 1-5; a body that leaves ECX alone; LOOP, LOOPE or LOOPNE back.
    void loop_block() {
        const bool as32 = pick(3) == 0;
        std::vector<uint8_t> mov;
        if (as32) mov.push_back(0x66);
        mov.push_back(0xb9);
        mov.push_back(uint8_t(1 + pick(5)));
        mov.push_back(0);
        if (as32) mov.insert(mov.end(), {0, 0});
        items.push_back(Item{mov, -1, 0, 0});
        const int body = int(items.size());
        const unsigned length = 1 + pick(5);
        for (unsigned n = 0; n < length; ++n) items.push_back(Item{instruction(true), -1, 0, 0});
        Item loop;
        if (as32) loop.bytes.push_back(0x67);
        if (pick(4) == 0) loop.bytes.push_back(0x66);
        loop.bytes.push_back(uint8_t(0xe0 + pick(3)));
        loop.rel_at = unsigned(loop.bytes.size());
        loop.rel_size = 1;
        loop.bytes.push_back(0);
        loop.target = body;
        items.push_back(loop);
    }

    // A rare conditional jump back over a few instructions; the retirement
    // limit bounds it.
    void backward_jump() {
        if (items.size() < 3) return;
        Item branch = jump_item(0);
        branch.target = int(items.size()) - 1 - int(pick(3));
        if (branch.target < fence) return;
        items.push_back(branch);
    }

    // kind 0: Jcc (rel8 or rel16/32), 1: JMP, 2: JCXZ.
    Item jump_item(unsigned kind) {
        Item item;
        const bool os32 = pick(4) == 0;
        if (os32) item.bytes.push_back(0x66);
        if (kind == 2) {
            if (pick(3) == 0) item.bytes.push_back(0x67);
            item.bytes.push_back(0xe3);
            item.rel_size = 1;
        } else if (kind == 1) {
            const bool near = pick(2);
            item.bytes.push_back(near ? 0xe9 : 0xeb);
            item.rel_size = near ? (os32 ? 4 : 2) : 1;
        } else {
            const bool near = pick(2);
            if (near) {
                item.bytes.push_back(0x0f);
                item.bytes.push_back(uint8_t(0x80 + pick(16)));
                item.rel_size = os32 ? 4 : 2;
            } else {
                item.bytes.push_back(uint8_t(0x70 + pick(16)));
                item.rel_size = 1;
            }
        }
        item.rel_at = unsigned(item.bytes.size());
        item.bytes.insert(item.bytes.end(), item.rel_size, 0);
        return item;
    }

    std::vector<uint8_t> layout() {
        std::vector<uint32_t> offset(items.size() + 1, 0);
        for (size_t n = 0; n < items.size(); ++n) offset[n + 1] = offset[n] + uint32_t(items[n].bytes.size());
        std::vector<uint8_t> out;
        for (size_t n = 0; n < items.size(); ++n) {
            Item &item = items[n];
            if (item.target >= 0 && item.far == Item::NEAR_ABS) {
                const uint32_t value = program_eip + offset[size_t(item.target)];
                for (unsigned i = 0; i < item.rel_size; ++i) item.bytes[item.rel_at + i] = uint8_t(value >> (8 * i));
            } else if (item.target >= 0 && item.far != Item::NEAR) {
                uint16_t sel;
                uint32_t off;
                far_point(program_base + offset[size_t(item.target)], sel, off);
                const uint32_t value = item.far == Item::FAR_SEL ? sel : off;
                for (unsigned i = 0; i < item.rel_size; ++i) item.bytes[item.rel_at + i] = uint8_t(value >> (8 * i));
                if (item.far == Item::FAR_PTR) {
                    item.bytes[item.rel_at + item.rel_size] = uint8_t(sel);
                    item.bytes[item.rel_at + item.rel_size + 1] = uint8_t(sel >> 8);
                }
            } else if (item.target >= 0) {
                int64_t rel = int64_t(offset[size_t(item.target)]) - int64_t(offset[n + 1]);
                if (item.rel_size == 1 && (rel < -128 || rel > 127)) rel = 0;  // falls through
                for (unsigned i = 0; i < item.rel_size; ++i) item.bytes[item.rel_at + i] = uint8_t(uint64_t(rel) >> (8 * i));
            }
            out.insert(out.end(), item.bytes.begin(), item.bytes.end());
        }
        return out;
    }
};

// Random programs' interrupts: vectors 8-15 and NMI's 2 at a handler that
// IRETs, at FFFF:FFE0, above any program's code (CS.base at most FFFF0h, EIP
// at most D100h, then the program).
constexpr uint32_t IRQ_HANDLER = 0x10ffd0;

int run_random(fes_i386::Oracle &oracle, OracleBus &bus, Rtl &rtl, const std::vector<Entry> &entries,
               Summary &summary, uint64_t seed, uint64_t programs, uint64_t retire_limit, uint64_t first) {
    std::mt19937_64 random(seed);
    Generator generator(random);
    for (uint64_t n = 0; n < programs; ++n) {
        State state;
        for (auto &r : state.gpr) r = uint32_t(random());
        // Every arithmetic flag, IF and DF at random; TF stays clear.
        state.eflags = (uint32_t(random()) & 0x0ed5) | 0x2;
        for (int k = 0; k < 6; ++k) state.seg[k] = fes_i386::real_mode_segment(uint16_t(random()));
        state.eip = 0x100 + uint32_t(random() % 0xd000);
        const uint32_t base = state.seg[fes_i386::SEG_CS].base + state.eip;
        const std::vector<uint8_t> code = generator.program(20 + unsigned(random() % 180), base, state.eip);
        if (n < first) continue;
        for (size_t i = 0; i < code.size(); ++i) bus.ram[base + i] = rtl.ram[base + i] = code[i];
        oracle.invalidate(base, uint32_t(code.size()));
        // A reset first: no NMI pending or blocked from the last program, as
        // the RTL's reset leaves it (set_state keeps them).
        oracle.reset();
        oracle.set_state(state);
        rtl.start(state);
        bus.io.reset(false);
        rtl.io.reset(false);
        bus.pic = Pic{};
        oracle.set_intr(false);
        std::vector<std::pair<uint32_t, uint8_t>> image{{IRQ_HANDLER, 0xcf}};
        for (uint32_t vector : {2u, 8u, 9u, 10u, 11u, 12u, 13u, 14u, 15u})
            for (uint32_t i = 0; i < 4; ++i) image.emplace_back(vector * 4 + i, uint8_t(0xffffffe0u >> (8 * i)));
        static_assert(0xffff0 + 0xffe0 == IRQ_HANDLER);
        for (auto [address, value] : image) {
            bus.ram[address] = rtl.ram[address] = value;
            oracle.invalidate(address, 1);
            rtl.written.insert(address);
        }
        // Each program's interrupts from its own seed, apart from the
        // programs', so FES_I386_FIRST reruns them too.
        std::mt19937_64 irq_random(seed * 0x9e3779b97f4a7c15ull + n);
        Irq irq;
        irq.random = &irq_random;
        irq.handler = IRQ_HANDLER;
        irq.entry = {0xe0, 0xff, 0xff, 0xff};  // FFFF:FFE0

        Mismatches out;
        Outcome outcome;
        try {
            outcome = lockstep(oracle, bus, rtl, entries, Fields{}, retire_limit, true, summary, out, base,
                               base + uint32_t(code.size()), &irq);
        } catch (const std::exception &error) {
            out.add(std::string("oracle-error:") + error.what(), 0, 0);
            oracle.reset();
        }
        if (out.items.empty() && !outcome.halted && !outcome.limit && !outcome.smc && outcome.exception < 0 &&
            !outcome.double_fault && !outcome.vector_moved)
            out.add("halt", 1, 0);
        summary.limits += outcome.limit;
        summary.smc += outcome.smc;
        summary.vector_moved += outcome.vector_moved;
        summary.halts += outcome.halted;
        summary.double_faults += outcome.double_fault;
        if (outcome.exception >= 0) {
            ++summary.faults;
            ++summary.fault_vectors[outcome.exception];
        }
        summary.cycles += rtl.cycles;
        rtl.cycles = 0;
        summary.load_waits += rtl.load_waits;
        rtl.load_waits = 0;
        for (size_t i = 0; i < code.size(); ++i) bus.ram[base + i] = rtl.ram[base + i] = 0;
        oracle.invalidate(base, uint32_t(code.size()));
        for (uint32_t address : rtl.written) {
            if (address < RAM_BYTES) bus.ram[address] = rtl.ram[address] = 0;
            oracle.invalidate(address, 1);
        }
        rtl.written.clear();
        ++summary.runs;
        if (out.items.empty()) {
            ++summary.passed;
        } else {
            ++summary.failed;
            std::cout << "FAIL random#" << seed << '#' << n << " cs:ip=" << hex(state.seg[fes_i386::SEG_CS].selector)
                      << ':' << hex(state.eip) << " [" << outcome.code << ']';
            for (auto &item : out.items) std::cout << ' ' << item;
            std::cout << '\n';
        }
    }
    return 0;
}

// Every encoding the runner lists as outside the build must stop the CPU
// before it retires, so no check passes over an unbuilt instruction.
int run_unbuilt(Rtl &rtl, Summary &summary) {
    State state;
    for (int k = 0; k < 6; ++k) state.seg[k] = fes_i386::real_mode_segment(0x1000);
    state.eip = 0x100;
    const uint32_t base = state.seg[fes_i386::SEG_CS].base + state.eip;
    std::string line;
    while (std::getline(std::cin, line)) {
        if (line.empty()) continue;
        std::vector<uint8_t> code;
        for (size_t i = 0; i + 1 < line.size(); i += 2) code.push_back(uint8_t(std::stoul(line.substr(i, 2), nullptr, 16)));
        code.resize(16, 0);
        code.push_back(0xf4);
        for (size_t i = 0; i < code.size(); ++i) rtl.ram[base + i] = code[i];
        rtl.start(state);
        const Rtl::Event event = rtl.next();
        ++summary.runs;
        summary.cycles += rtl.cycles;
        rtl.cycles = 0;
        if (event == Rtl::Event::Unimplemented) {
            ++summary.passed;
        } else {
            ++summary.failed;
            std::cout << "FAIL unbuilt:" << line << ' '
                      << (event != Rtl::Event::Retired ? "timeout" : rtl.top.trace_exception ? "exception" : "retired")
                      << " 1 0\n";
        }
        for (size_t i = 0; i < code.size(); ++i) rtl.ram[base + i] = 0;
    }
    return 0;
}

uint64_t env(const char *name, uint64_t fallback) {
    const char *value = std::getenv(name);
    return value ? std::strtoull(value, nullptr, 0) : fallback;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::ios::sync_with_stdio(false);
    const std::string mode = std::getenv("FES_I386_MODE") ? std::getenv("FES_I386_MODE") : "corpus";
    const uint64_t seed = env("FES_I386_SEED", 1);

    OracleBus bus;
    std::unique_ptr<fes_i386::Oracle> oracle = fes_i386::make_oracle(bus);
    VerilatedContext context;
    Rtl rtl(context);
    std::mt19937_64 latency_random(seed * 7919 + 17);
    if (env("FES_I386_LATENCY", 1)) rtl.fetch.random = rtl.data.random = rtl.port.random = rtl.inta.random = &latency_random;

    Summary summary;
    const std::vector<Entry> entries = load_entries(std::getenv("FES_I386_DIVERGENCES"));
    if (mode == "reset-store") {
        ++summary.runs;
        if (reset_store_regression(rtl)) {
            ++summary.passed;
        } else {
            ++summary.failed;
            std::cout << "FAIL reset-store reset-edge-write 0 1\n";
        }
    } else if (mode == "directed") {
        run_fault_regressions(*oracle, bus, rtl, entries, summary);
    } else if (mode == "unbuilt") {
        run_unbuilt(rtl, summary);
    } else if (mode == "random") {
        run_random(*oracle, bus, rtl, entries, summary, seed, env("FES_I386_PROGRAMS", 1000),
                   env("FES_I386_RETIRE_LIMIT", 2000), env("FES_I386_FIRST", 0));
    } else {
        run_corpus(*oracle, bus, rtl, entries, summary);
    }

    auto histogram = [](const std::map<uint64_t, uint64_t> &map) {
        std::string text;
        for (auto &[gap, count] : map)
            text += (text.empty() ? "" : ", ") + std::string("\"") + std::to_string(gap) + "\": " + std::to_string(count);
        return "{" + text + "}";
    };
    std::string vectors, divergences;
    for (auto &[vector, count] : summary.fault_vectors)
        vectors += (vectors.empty() ? "" : ", ") + std::string("\"") + std::to_string(vector) + "\": " + std::to_string(count);
    for (auto &[id, count] : summary.divergences)
        divergences += (divergences.empty() ? "" : ", ") + std::string("\"") + id + "\": " + std::to_string(count);
    // The FAIL lines go through std::cout, unsynced with stdio: flush them
    // first, so the summary line is whole after them.
    std::cout.flush();
    std::printf("{\"mode\": \"%s\", \"seed\": %llu, \"latency\": %s, \"runs\": %llu, \"passed\": %llu, \"failed\": %llu, "
                "\"retirements\": %llu, \"cycles\": %llu, \"halts\": %llu, \"interrupts\": %llu, \"faults\": %llu, \"fault_vectors\": {%s}, "
                "\"double_faults\": %llu, "
                "\"retire_limits\": %llu, \"smc_stops\": %llu, \"load_wait_cycles\": %llu, \"io_accesses\": %llu, "
                "\"external_interrupts\": %llu, \"nmis\": %llu, \"vector_moved_stops\": %llu, "
                "\"divergence_retirements\": {%s}, "
                "\"trace_hash\": \"%016llx\", "
                "\"gaps\": {\"sequential\": %s, \"prefixed\": %s, \"lea\": %s, \"selector\": %s, "
                "\"after_taken\": %s, \"after_not_taken\": %s, \"shift_consumer\": %s, \"after_shift\": %s, "
                "\"store_load\": %s, \"after_store\": %s, \"address\": %s, \"after_segment\": %s, \"after_stack\": %s, \"stack_stack\": %s, "
                "\"several\": %s, \"after_string\": %s, \"after_io\": %s}}\n",
                mode.c_str(), (unsigned long long)seed, rtl.fetch.random ? "true" : "false",
                (unsigned long long)summary.runs, (unsigned long long)summary.passed,
                (unsigned long long)summary.failed, (unsigned long long)summary.retirements,
                (unsigned long long)summary.cycles, (unsigned long long)summary.halts,
                (unsigned long long)summary.interrupts,
                (unsigned long long)summary.faults, vectors.c_str(), (unsigned long long)summary.double_faults,
                (unsigned long long)summary.limits,
                (unsigned long long)summary.smc, (unsigned long long)summary.load_waits,
                (unsigned long long)summary.io_accesses, (unsigned long long)summary.external_interrupts,
                (unsigned long long)summary.nmis, (unsigned long long)summary.vector_moved, divergences.c_str(),
                (unsigned long long)summary.trace_hash, histogram(summary.gap_sequential).c_str(),
                histogram(summary.gap_prefixed).c_str(), histogram(summary.gap_lea).c_str(),
                histogram(summary.gap_selector).c_str(), histogram(summary.gap_after_taken).c_str(),
                histogram(summary.gap_after_not_taken).c_str(), histogram(summary.gap_shift_consumer).c_str(),
                histogram(summary.gap_after_shift).c_str(), histogram(summary.gap_store_load).c_str(),
                histogram(summary.gap_after_store).c_str(), histogram(summary.gap_address).c_str(),
                histogram(summary.gap_after_segment).c_str(), histogram(summary.gap_after_stack).c_str(),
                histogram(summary.gap_stack_stack).c_str(), histogram(summary.gap_several).c_str(),
                histogram(summary.gap_after_string).c_str(), histogram(summary.gap_after_io).c_str());
    return summary.failed ? 1 : 0;
}
