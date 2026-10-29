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
};

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
};

// One oracle may exist per process (the backend keeps global CPU state).
std::unique_ptr<Oracle> make_oracle(Bus &bus);

}  // namespace fes_i386
