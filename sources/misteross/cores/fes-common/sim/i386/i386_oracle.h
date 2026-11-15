// SPDX-License-Identifier: MIT
// Backend-neutral interface to the external i386 lockstep oracle. The oracle is
// test-only: it never enters a package or an RTL source list. The chosen
// backend and its pinned source are recorded in README.md and
// scripts/fes_i386_oracle.py.
#pragma once
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <functional>
#include <memory>
#include <new>
#include <string>
#include <vector>

namespace fes_i386 {

enum SegmentIndex { SEG_ES = 0, SEG_CS, SEG_SS, SEG_DS, SEG_FS, SEG_GS, SEG_COUNT };
enum RegisterIndex { REG_EAX = 0, REG_ECX, REG_EDX, REG_EBX, REG_ESP, REG_EBP, REG_ESI, REG_EDI, REG_COUNT };

// A selector with its hidden descriptor cache. `ar` holds descriptor bits 8..23
// with the limit nibble cleared: bits 0-3 type, 4 S, 5-6 DPL, 7 P, 12 AVL,
// 14 D/B, 15 G. `limit` is the byte-granular effective limit.
struct Segment {
    uint16_t selector = 0;
    uint32_t base = 0;
    uint32_t limit = 0xffff;
    uint16_t ar = 0x93;
    bool valid = true;
};

struct Table {
    uint32_t base = 0;
    uint16_t limit = 0xffff;
};

struct State {
    uint32_t gpr[REG_COUNT] = {};
    uint32_t eflags = 0x2;
    uint32_t eip = 0;
    Segment seg[SEG_COUNT];
    Segment ldtr, tr;
    Table gdtr, idtr;
    uint32_t cr0 = 0, cr2 = 0, cr3 = 0;
    uint32_t dr[4] = {};
    uint32_t dr6 = 0xffff0ff0, dr7 = 0x400;
    // The CPL in protected mode. It is CS's RPL only once CS has been loaded
    // there: protected-mode entry from real mode keeps CPL 0 whatever CS's
    // selector. In real mode it is 0.
    uint8_t cpl = 0;
};

// The cache a real-mode selector has after reset: limit and attributes are
// those of reset, which a real-mode load keeps.
inline Segment real_mode_segment(uint16_t selector) {
    return Segment{selector, uint32_t(selector) << 4, 0xffff, 0x93, true};
}

// Memory and I/O are served by the harness. `page` may return a 4 KiB-aligned
// host pointer to the page holding `paddr`; the oracle then reads and writes
// that page directly, and instruction fetch requires it. Returning nullptr
// routes data accesses through `read` and `write` (MMIO). Physical addresses
// reach the bus after A20 masking; `read` and `write` never cross a 4 KiB page.
// Zeroed, 4 KiB-aligned storage for Bus::page.
class PageMemory {
public:
    explicit PageMemory(size_t bytes)
        : bytes_(static_cast<uint8_t *>(std::aligned_alloc(4096, bytes))), size_(bytes) {
        if (!bytes_ || bytes % 4096) throw std::bad_alloc();
        std::memset(bytes_, 0, bytes);
    }
    ~PageMemory() { std::free(bytes_); }
    PageMemory(const PageMemory &) = delete;
    PageMemory &operator=(const PageMemory &) = delete;
    uint8_t &operator[](size_t index) { return bytes_[index]; }
    uint8_t *data() { return bytes_; }
    size_t size() const { return size_; }

private:
    uint8_t *bytes_;
    size_t size_;
};

class Bus {
public:
    virtual ~Bus() = default;
    virtual uint8_t *page(uint32_t paddr, bool write) = 0;
    virtual void read(uint32_t paddr, unsigned length, uint8_t *data) = 0;
    virtual void write(uint32_t paddr, unsigned length, const uint8_t *data) = 0;
    virtual uint32_t in(uint16_t port, unsigned length) = 0;
    virtual void out(uint16_t port, unsigned length, uint32_t value) = 0;
    // INTA cycle: the vector the machine's PICs return.
    virtual uint8_t interrupt_acknowledge() = 0;
};

enum class EventKind : uint8_t { Exception, ExternalInterrupt, Nmi, SoftwareInterrupt };

struct Event {
    EventKind kind;
    uint8_t vector;
    uint32_t error_code;
    uint32_t writes_before = 0;  // the step's writes before its delivery began (StepResult writes)
};

struct Access {
    uint32_t linear;
    uint32_t physical;
    uint8_t length;
};

// One step either takes the event pending at the current instruction boundary
// (a debug trap, NMI or INTR) and stops at the handler's first instruction, or
// executes one instruction. A faulting instruction delivers its exception within
// the step and does not retire. A REP string instruction retires as one step
// unless an event is pending between iterations.
struct StepResult {
    bool retired = false;      // an instruction completed
    bool halted = false;       // the CPU is in HLT after this step
    bool shutdown = false;     // triple fault or shutdown
    uint32_t eip_before = 0;   // EIP at the start of the step
    std::vector<Event> events; // in delivery order
    std::vector<Access> writes;
    std::vector<Access> reads;  // data reads, a read-modify-write's included, in the order made
    // With the AT coprocessor coupling (Oracle::set_at_coprocessor): the CPU
    // is held at an ESC or WAIT that waits for BUSY#, which the board's latch
    // keeps active. Nothing retired; CS:EIP is that instruction's, which runs
    // once OUT F0h clears the latch. An NMI or INTR (IF set) is taken while
    // held, and its frame returns to the held instruction.
    bool x87_held = false;
};

// The x87 pointers a 386 keeps for its coprocessor, which FNSTENV and FNSAVE
// store: the last non-control ESC instruction's CS selector and EIP, and its
// memory operand's segment selector and offset.
struct X87Pointers {
    uint32_t ip = 0;
    uint32_t dp = 0;
    uint16_t cs = 0;
    uint16_t ds = 0;
};

// A 387 register: the 64-bit significand and the sign and 15-bit exponent.
struct X87Register {
    uint64_t significand = 0;
    uint16_t sign_exponent = 0;
    bool operator==(const X87Register &o) const {
        return significand == o.significand && sign_exponent == o.sign_exponent;
    }
};

// A 387's state as FNSAVE stores it: the control word, the status word with
// TOP in bits 13-11, the tag word with two bits per physical register (00
// valid, 01 zero, 10 special, 11 empty) as FNSTENV computes it from the
// registers, the last instruction's 11-bit opcode, the x87 pointers, and the
// eight physical registers: ST(i) is r[(TOP + i) & 7].
struct X87State {
    uint16_t control = 0;
    uint16_t status = 0;
    uint16_t tag = 0;
    uint16_t opcode = 0;
    X87Pointers pointers;
    X87Register r[8];
};

// The fields in which two X87States differ: control, status, tag, opcode,
// fip, fcs, fdp, fds and r0-r7 (physical registers).
inline std::vector<std::string> x87_differences(const X87State &a, const X87State &b) {
    std::vector<std::string> out;
    if (a.control != b.control) out.push_back("control");
    if (a.status != b.status) out.push_back("status");
    if (a.tag != b.tag) out.push_back("tag");
    if (a.opcode != b.opcode) out.push_back("opcode");
    if (a.pointers.ip != b.pointers.ip) out.push_back("fip");
    if (a.pointers.cs != b.pointers.cs) out.push_back("fcs");
    if (a.pointers.dp != b.pointers.dp) out.push_back("fdp");
    if (a.pointers.ds != b.pointers.ds) out.push_back("fds");
    for (int n = 0; n < 8; ++n)
        if (!(a.r[n] == b.r[n])) out.push_back("r" + std::to_string(n));
    return out;
}

// The oracle's decode of one instruction, without executing it.
struct Decoded {
    bool complete = false;   // the instruction ends within min(available, 15) bytes
    bool undefined = false;  // the oracle raises #UD for this encoding on this CPU
    uint8_t length = 0;      // bytes, prefixes included, when complete
};

class Oracle {
public:
    virtual ~Oracle() = default;
    virtual std::string name() const = 0;
    // Decode the instruction at bytes[0..available) with default operand and
    // address size code32 (CS.D). Touches no CPU state.
    virtual Decoded decode(const uint8_t *bytes, unsigned available, bool code32) const = 0;
    // Hardware reset: CS:EIP F000:FFF0 with CS base FFFF0000.
    virtual void reset() = 0;
    virtual State get_state() const = 0;
    virtual void set_state(const State &state) = 0;
    virtual StepResult step() = 0;
    // INTR is a level sampled at instruction boundaries; NMI is an edge.
    virtual void set_intr(bool level) = 0;
    virtual void nmi() = 0;
    // Called after each iteration of a REP string instruction a step runs,
    // with the iterations the step has run. It may raise INTR or NMI, which
    // the instruction takes after this iteration unless it is the last.
    // An empty hook clears it.
    virtual void set_iteration_hook(std::function<void(unsigned iterations)> hook) = 0;
    virtual void set_a20(bool enabled) = 0;
    // The harness changed memory behind the oracle's back (test setup, DMA).
    virtual void invalidate(uint32_t paddr, uint32_t length) = 0;
    // The x87 pointers, read and set.
    virtual X87Pointers x87_pointers() const = 0;
    virtual void set_x87_pointers(const X87Pointers &pointers) = 0;
    // Make the oracle's coprocessor read as an empty socket's: the control
    // and tag words, the opcode and the registers all ones, and the status
    // word all ones when status is set, else clear, so that no exception is
    // pending. A step after this stores what a 386 with no 387 stores.
    virtual void empty_socket(bool status) = 0;
    // The coprocessor's whole state (X87State).
    virtual X87State x87_state() const = 0;
    // A 387 fitted as on an AT board (the i386dx+387 identity), on or off;
    // off by default and at construction, when the oracle behaves as before
    // this existed. On:
    //   - the CPU's ERROR# input is tied inactive, so no ESC or WAIT raises
    //     exception 16 or waits for IGNNE#; the 387's ERROR# (its status
    //     word's ES) is IRQ 13's level (x87_irq13), for the bench to drive
    //     into the slave PIC;
    //   - ERROR# going active latches BUSY# active. While latched, every ESC
    //     except FNINIT and FNCLEX, and WAIT, holds before it runs
    //     (StepResult::x87_held) unless it raises #NM first;
    //   - a byte written to port F0h clears the latch, and one to F1h resets
    //     the 387 (control word 037Eh, status word 8081h with B mirroring ES
    //     and ERROR# active, every register empty) without setting it. Both
    //     still reach the bus.
    // reset() clears the latch. The sources are in the oracle README.
    virtual void set_at_coprocessor(bool fitted) = 0;
    // IRQ 13's level from the 387's ERROR#: false unless set_at_coprocessor.
    virtual bool x87_irq13() const = 0;
    // Whether the AT board's BUSY# latch is set.
    virtual bool x87_busy_latched() const = 0;
};

// One oracle may exist per process (the backend keeps global CPU state).
std::unique_ptr<Oracle> make_oracle(Bus &bus);

}  // namespace fes_i386
