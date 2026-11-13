// SPDX-License-Identifier: MIT
// The oracle's AT coprocessor coupling (Oracle::set_at_coprocessor) and its
// x87 state accessor, on the oracle alone (#1768 slice 0). Each case runs a
// short real-mode program from 0000:1000 on a flat 1 MiB bus whose PIC
// raises vector 75h on a rising IRQ 13 (Oracle::x87_irq13), as an AT's
// slave PIC does in edge mode. The handlers count their calls and keep the
// IP their frame returns to:
//   75h  (IRQ 13) at 0000:2000: [0600] calls, [0608] frame IP, OUT F0h, IRET
//   2    (NMI)    at 0000:2100: [0602] calls, [060A] frame IP, OUT F0h, IRET
//   7    (#NM)    at 0000:2200: [0604] calls, [060C] frame IP, CLTS, IRET
// The prologue unmasks divide-by-zero and divides 1 by 0 (FNINIT; FLDCW
// 037Bh; FLD1; FLDZ; FDIVP ST(1), ST), so the 387 sets ES and ERROR#.
//   if1         IF set: IRQ 13 is taken after FDIVP and its frame returns to
//               the FSTP after it, which then runs with ES still set.
//   held-nmi    IF clear: the FSTP after FDIVP holds on the latch, step after
//               step, until an NMI whose handler writes F0h; the NMI's frame
//               returns to the FSTP, which then runs.
//   no-wait     with the latch set, FNCLEX and FNINIT run (they skip BUSY#),
//               FNCLEX dropping IRQ 13, and the next FLD1 holds.
//   fnstsw      FNSTSW AX holds too: it skips the error check but not BUSY#.
//   fsetpm      FSETPM, a 387's FNOP, holds too: only FNINIT and FNCLEX pass.
//   nm-first    with TS set, FLD1 raises #NM before it holds; after CLTS it
//               holds.
//   wait-mp0    WAIT with TS set and MP clear holds; with MP and TS set
//   wait-mp1    it raises #NM first.
//   f1-reset    OUT F1h resets the 387: control 037Eh, status 8081h, every
//               tag empty, IRQ 13 high, the latch unchanged.
//   accessor    x87_state() after FLD1, FLDPI, FLDZ and a denormal FLD m80
//               has the tags 00, 01, 10 and 11, FLD m80's pointers and
//               opcode, and no x87_differences from the FNSAVE image that
//               follows.
//   off         without the coupling, IRQ 13 stays low and nothing latches.
//   fresh       an oracle made after one with the coupling on starts without
//               it, as set_at_coprocessor documents.
// Output: one "PASS <case>" or "FAIL <case> <what>" line per case, then
// "SUMMARY <passed> <failed>". Exit status 1 on any failure.
// scripts/fes_i386_oracle.py builds it (tests/test_fes_i386_oracle.py).
#include "i386_oracle.h"

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <functional>
#include <initializer_list>
#include <string>
#include <vector>

namespace {

using fes_i386::State;
using fes_i386::StepResult;

class FlatBus final : public fes_i386::Bus {
public:
    fes_i386::PageMemory ram{1u << 20};
    bool pending = false;  // IRQ 13's request, until the CPU acknowledges it
    uint8_t *page(uint32_t paddr, bool) override { return paddr < ram.size() ? &ram[paddr & ~0xfffu] : nullptr; }
    void read(uint32_t, unsigned length, uint8_t *data) override { std::memset(data, 0xff, length); }
    void write(uint32_t, unsigned, const uint8_t *) override {}
    uint32_t in(uint16_t, unsigned) override { return 0xffffffffu; }
    void out(uint16_t, unsigned, uint32_t) override {}
    uint8_t interrupt_acknowledge() override {
        pending = false;
        return 0x75;
    }
    uint16_t word(uint32_t at) const { return uint16_t(ram_byte(at) | ram_byte(at + 1) << 8); }
    uint8_t ram_byte(uint32_t at) const { return const_cast<FlatBus *>(this)->ram[at]; }
    void put(uint32_t at, std::initializer_list<uint8_t> bytes) {
        for (uint8_t b : bytes) ram[at++] = b;
    }
};

constexpr uint32_t CODE = 0x1000;

// A program and the offsets it marks.
struct Code {
    std::vector<uint8_t> bytes;
    uint16_t here() const { return uint16_t(CODE + bytes.size()); }
    uint16_t op(std::initializer_list<uint8_t> b) {
        const uint16_t at = here();
        bytes.insert(bytes.end(), b);
        return at;
    }
    void prologue() {
        op({0xdb, 0xe3});              // FNINIT
        op({0xd9, 0x2e, 0x00, 0x05});  // FLDCW [0500h]: 037Bh
        op({0xd9, 0xe8});              // FLD1
        op({0xd9, 0xee});              // FLDZ
        op({0xde, 0xf9});              // FDIVP ST(1), ST: 1 / 0
    }
    void set_cr0(uint8_t bits) {
        op({0x0f, 0x20, 0xc0});  // MOV EAX, CR0
        op({0x0c, bits});        // OR AL, bits
        op({0x0f, 0x22, 0xc0});  // MOV CR0, EAX
    }
};

struct Run {
    std::vector<StepResult> steps;
    bool halted = false;
    unsigned held = 0;             // steps that ended held
    std::vector<uint32_t> held_at; // their EIPs
    std::vector<uint8_t> vectors;  // events delivered, in order
};

class Bench {
public:
    FlatBus bus;
    std::unique_ptr<fes_i386::Oracle> oracle = fes_i386::make_oracle(bus);
    unsigned passed = 0, failed = 0;
    std::string failure;

    void check(bool ok, const std::string &what) {
        if (!ok && failure.empty()) failure = what;
    }
    void done(const char *name) {
        if (failure.empty()) {
            ++passed;
            std::printf("PASS %s\n", name);
        } else {
            ++failed;
            std::printf("FAIL %s %s\n", name, failure.c_str());
        }
        failure.clear();
    }

    // Load the program and the handlers, reset, and run to HLT. hook runs
    // after every step with its index and result.
    // coupled: set_at_coprocessor's argument, or -1 to leave the coupling as
    // the oracle has it.
    Run run(const Code &code, int coupled = 1,
            const std::function<void(unsigned, const StepResult &)> &hook = {}) {
        std::memset(bus.ram.data(), 0, bus.ram.size());
        bus.pending = false;
        auto vector = [&](unsigned n, uint16_t ip) { bus.put(n * 4, {uint8_t(ip), uint8_t(ip >> 8), 0, 0}); };
        vector(0x75, 0x2000);
        vector(2, 0x2100);
        vector(6, 0x2300);
        vector(7, 0x2200);
        // PUSH BP; MOV BP, SP; PUSH AX; MOV AX, [BP+2]; MOV [frame], AX; POP AX;
        // POP BP; INC WORD [count]; then the handler's own instruction; IRET.
        auto handler = [&](uint32_t at, uint8_t count, uint8_t frame, std::initializer_list<uint8_t> own) {
            bus.put(at, {0x55, 0x8b, 0xec, 0x50, 0x8b, 0x46, 0x02, 0xa3, frame, 0x06, 0x58, 0x5d,
                         0xff, 0x06, count, 0x06});
            bus.put(at + 16, own);
            bus.put(at + 16 + uint32_t(own.size()), {0xcf});
        };
        handler(0x2000, 0x00, 0x08, {0xe6, 0xf0});  // OUT F0h, AL
        handler(0x2100, 0x02, 0x0a, {0xe6, 0xf0});
        handler(0x2200, 0x04, 0x0c, {0x0f, 0x06});  // CLTS
        bus.put(0x2300, {0xf4});                    // #UD stops here
        bus.put(0x500, {0x7b, 0x03});               // control word 037Bh: ZM unmasked
        // A denormal for FLD m80 at 0520h: significand 1, exponent 0.
        bus.put(0x520, {1, 0, 0, 0, 0, 0, 0, 0, 0, 0});
        for (size_t n = 0; n < code.bytes.size(); ++n) bus.ram[CODE + n] = code.bytes[n];
        if (coupled >= 0) oracle->set_at_coprocessor(coupled != 0);
        oracle->reset();
        oracle->invalidate(0, uint32_t(bus.ram.size()));
        State s = oracle->get_state();
        for (int n = 0; n < fes_i386::SEG_COUNT; ++n) s.seg[n] = fes_i386::real_mode_segment(0);
        s.eip = CODE;
        s.gpr[fes_i386::REG_ESP] = 0x8000;
        s.eflags = 0x2;
        oracle->set_state(s);
        oracle->set_intr(false);
        Run r;
        bool irq13 = oracle->x87_irq13();
        for (unsigned n = 0; n < 200 && !r.halted; ++n) {
            StepResult result = oracle->step();
            for (const auto &e : result.events) r.vectors.push_back(e.vector);
            if (result.x87_held) {
                ++r.held;
                r.held_at.push_back(oracle->get_state().eip);
            }
            r.halted = result.halted;
            // The PIC in edge mode: a rising IRQ 13 requests vector 75h.
            const bool now = oracle->x87_irq13();
            if (now && !irq13) bus.pending = true;
            irq13 = now;
            oracle->set_intr(bus.pending);
            r.steps.push_back(result);
            if (hook) hook(n, result);
        }
        check(r.halted, "no HLT within 200 steps");
        return r;
    }

    // An NMI once the CPU has been held for four steps.
    std::function<void(unsigned, const StepResult &)> nmi_after_holds(unsigned &holds) {
        return [this, &holds](unsigned, const StepResult &result) {
            if (result.x87_held && ++holds == 4) oracle->nmi();
        };
    }

    uint16_t ax() const { return uint16_t(oracle->get_state().gpr[fes_i386::REG_EAX]); }
    bool held_only_at(const Run &r, uint16_t at) {
        for (uint32_t eip : r.held_at)
            if (eip != at) return false;
        return true;
    }
};

void cases(Bench &b) {
    {
        Code c;
        c.op({0xfb});                                     // STI
        c.prologue();
        const uint16_t fstp = c.op({0xd9, 0x1e, 0x10, 0x05});  // FSTP DWORD [0510h]
        c.op({0xdf, 0xe0});                               // FNSTSW AX
        c.op({0xf4});
        Run r = b.run(c);
        b.check(b.bus.word(0x600) == 1, "IRQ 13 handler calls " + std::to_string(b.bus.word(0x600)));
        b.check(b.bus.word(0x608) == fstp, "IRQ 13 frame IP " + std::to_string(b.bus.word(0x608)));
        b.check(r.held == 0, "held steps " + std::to_string(r.held));
        b.check((b.ax() & 0x8084) == 0x8084, "status after FSTP " + std::to_string(b.ax()));
        b.check(b.oracle->x87_irq13() && !b.oracle->x87_busy_latched(), "IRQ 13 high, latch clear at the end");
        b.done("if1");
    }
    {
        Code c;
        c.op({0xfa});                                     // CLI
        c.prologue();
        const uint16_t fstp = c.op({0xd9, 0x1e, 0x10, 0x05});
        c.op({0xf4});
        unsigned holds = 0;
        Run r = b.run(c, true, b.nmi_after_holds(holds));
        b.check(r.held >= 4 && b.held_only_at(r, fstp), "held " + std::to_string(r.held) + " at FSTP");
        b.check(b.bus.word(0x602) == 1 && b.bus.word(0x60a) == fstp, "NMI frame IP " + std::to_string(b.bus.word(0x60a)));
        b.check(b.bus.word(0x600) == 0, "IRQ 13 taken with IF clear");
        b.check(!b.oracle->x87_busy_latched(), "latch set at the end");
        b.done("held-nmi");
    }
    {
        Code c;
        c.op({0xfa});
        c.prologue();
        c.op({0xdb, 0xe2});                     // FNCLEX
        c.op({0xdb, 0xe3});                     // FNINIT
        const uint16_t fld1 = c.op({0xd9, 0xe8});
        c.op({0xf4});
        unsigned holds = 0;
        bool dropped = false;
        Run r = b.run(c, true, [&](unsigned n, const StepResult &result) {
            b.nmi_after_holds(holds)(n, result);
            if (result.retired && result.eip_before == fld1 - 4 && !b.oracle->x87_irq13()) dropped = true;
        });
        b.check(dropped, "IRQ 13 still high after FNCLEX");
        b.check(r.held >= 4 && b.held_only_at(r, fld1), "held " + std::to_string(r.held) + " not only at FLD1");
        b.check(b.bus.word(0x60a) == fld1, "NMI frame IP");
        b.done("no-wait");
    }
    {
        Code c;
        c.op({0xfa});
        c.prologue();
        const uint16_t fnstsw = c.op({0xdf, 0xe0});
        c.op({0xf4});
        unsigned holds = 0;
        Run r = b.run(c, true, b.nmi_after_holds(holds));
        b.check(r.held >= 4 && b.held_only_at(r, fnstsw), "held " + std::to_string(r.held) + " not at FNSTSW");
        b.check((b.ax() & 0x8084) == 0x8084, "FNSTSW AX " + std::to_string(b.ax()));
        b.done("fnstsw");
    }
    {
        // FSETPM (DB E4) is an FNOP to a 387, and the 386 waits for BUSY#
        // before it as before any ESC but FNINIT and FNCLEX (80386 HRM 5.1).
        Code c;
        c.op({0xfa});
        c.prologue();
        const uint16_t fsetpm = c.op({0xdb, 0xe4});
        c.op({0xf4});
        unsigned holds = 0;
        Run r = b.run(c, true, b.nmi_after_holds(holds));
        b.check(r.held >= 4 && b.held_only_at(r, fsetpm), "held " + std::to_string(r.held) + " not at FSETPM");
        b.check(b.bus.word(0x60a) == fsetpm, "NMI frame IP");
        b.done("fsetpm");
    }
    {
        Code c;
        c.op({0xfa});
        c.prologue();
        c.set_cr0(0x08);                        // TS
        const uint16_t fld1 = c.op({0xd9, 0xe8});
        c.op({0xf4});
        unsigned holds = 0;
        Run r = b.run(c, true, b.nmi_after_holds(holds));
        b.check(b.bus.word(0x604) == 1 && b.bus.word(0x60c) == fld1, "#NM frame IP " + std::to_string(b.bus.word(0x60c)));
        b.check(!r.vectors.empty() && r.vectors[0] == 7, "#NM not first");
        b.check(r.held >= 4 && b.held_only_at(r, fld1), "held " + std::to_string(r.held) + " not at FLD1");
        b.done("nm-first");
    }
    for (int mp = 0; mp < 2; ++mp) {
        Code c;
        c.op({0xfa});
        c.prologue();
        c.set_cr0(mp ? 0x0a : 0x08);            // TS, and MP
        const uint16_t wait = c.op({0x9b});
        c.op({0xf4});
        unsigned holds = 0;
        Run r = b.run(c, true, b.nmi_after_holds(holds));
        b.check(b.bus.word(0x604) == unsigned(mp), "#NM calls " + std::to_string(b.bus.word(0x604)));
        b.check(r.held >= 4 && b.held_only_at(r, wait), "held " + std::to_string(r.held) + " not at WAIT");
        b.done(mp ? "wait-mp1" : "wait-mp0");
    }
    {
        // LOCK FWAIT is #UD before the AT BUSY# latch can hold the 386.
        Code c;
        c.op({0xfa});
        c.prologue();
        const uint16_t lock_wait = c.op({0xf0, 0x9b});
        c.op({0xf4});
        Run r = b.run(c);
        bool ud_at_lock_wait = false;
        for (const StepResult &step : r.steps) {
            if (step.eip_before == lock_wait && !step.retired && step.events.size() == 1 &&
                step.events[0].vector == 6) {
                ud_at_lock_wait = true;
            }
        }
        b.check(ud_at_lock_wait, "LOCK FWAIT did not raise #UD at its first byte");
        b.check(r.held == 0, "LOCK FWAIT held on BUSY# " + std::to_string(r.held) + " times");
        b.check(b.oracle->x87_busy_latched(), "#UD cleared the AT BUSY# latch");
        b.done("lock-fwait-ud");
    }
    {
        Code c;
        c.op({0xfa});
        c.op({0xdb, 0xe3});                     // FNINIT
        c.op({0xe6, 0xf1});                     // OUT F1h, AL
        c.op({0xf4});
        b.run(c);
        const fes_i386::X87State s = b.oracle->x87_state();
        b.check(s.control == 0x037e && s.status == 0x8081 && s.tag == 0xffff,
                "after F1h: " + std::to_string(s.control) + " " + std::to_string(s.status) + " " + std::to_string(s.tag));
        b.check(b.oracle->x87_irq13() && !b.oracle->x87_busy_latched(), "IRQ 13 low or latch set after F1h");
        b.done("f1-reset");
    }
    {
        Code c;
        c.op({0xfa});
        c.op({0xdb, 0xe3});                     // FNINIT
        c.op({0xd9, 0xe8});                     // FLD1
        c.op({0xd9, 0xeb});                     // FLDPI
        c.op({0xd9, 0xee});                     // FLDZ
        const uint16_t fld80 = c.op({0xdb, 0x2e, 0x20, 0x05});  // FLD TBYTE [0520h]: a denormal
        c.op({0xf4});
        b.run(c);
        const fes_i386::X87State s = b.oracle->x87_state();
        // Then FNSAVE [0700h] from that state: the next HLT's successor.
        State next = b.oracle->get_state();
        next.eip = 0x1800;
        b.bus.put(0x1800, {0xdd, 0x36, 0x00, 0x07, 0xf4});  // FNSAVE [0700h]; HLT
        b.oracle->invalidate(0x1800, 5);
        b.oracle->set_state(next);
        for (int n = 0; n < 4 && !b.oracle->step().halted; ++n) {}
        b.check(s.tag == 0x06ff, "tag " + std::to_string(s.tag));
        b.check(s.pointers.ip == fld80 && s.pointers.dp == 0x520 && s.opcode == 0x32e,
                "pointers " + std::to_string(s.pointers.ip) + " " + std::to_string(s.pointers.dp) + " opcode " +
                    std::to_string(s.opcode));
        // The 16-bit real-mode image (387 PRM, Figure 4-14): CS and DS are 0,
        // so its 20-bit addresses are the offsets; it stores no selectors.
        fes_i386::X87State image;
        image.control = b.bus.word(0x700);
        image.status = b.bus.word(0x702);
        image.tag = b.bus.word(0x704);
        image.pointers.ip = b.bus.word(0x706) | uint32_t(b.bus.word(0x708) & 0xf000) << 4;
        image.opcode = b.bus.word(0x708) & 0x7ff;
        image.pointers.dp = b.bus.word(0x70a) | uint32_t(b.bus.word(0x70c) & 0xf000) << 4;
        image.pointers.cs = s.pointers.cs;
        image.pointers.ds = s.pointers.ds;
        const unsigned top = (image.status >> 11) & 7;
        for (unsigned i = 0; i < 8; ++i) {
            fes_i386::X87Register &r = image.r[(top + i) & 7];
            for (unsigned k = 0; k < 8; ++k) r.significand |= uint64_t(b.bus.ram_byte(0x70e + 10 * i + k)) << (8 * k);
            r.sign_exponent = b.bus.word(0x70e + 10 * i + 8);
        }
        std::string differ;
        for (const std::string &field : fes_i386::x87_differences(s, image)) differ += " " + field;
        b.check(differ.empty(), "FNSAVE image differs:" + differ);
        b.done("accessor");
    }
    {
        Code c;
        c.op({0xfb});
        c.prologue();
        c.op({0xf4});
        Run r = b.run(c, 0);
        b.check(!b.oracle->x87_irq13() && !b.oracle->x87_busy_latched() && r.held == 0 && b.bus.word(0x600) == 0,
                "IRQ 13 or the latch without the coupling");
        b.done("off");
    }
}

}  // namespace

int main() {
    unsigned passed = 0, failed = 0;
    {
        Bench b;
        cases(b);
        // Leave this oracle with the coupling on for the fresh case.
        b.oracle->set_at_coprocessor(true);
        passed = b.passed;
        failed = b.failed;
    }
    {
        // fresh: a new oracle starts without the coupling, though the last
        // one in this process had it on (the coupling's state is global).
        Bench b;
        Code c;
        c.op({0xfb});
        c.prologue();
        c.op({0xf4});
        Run r = b.run(c, -1);
        b.check(!b.oracle->x87_irq13() && !b.oracle->x87_busy_latched() && r.held == 0 && b.bus.word(0x600) == 0,
                "the coupling is on in a new oracle");
        b.done("fresh");
        passed += b.passed;
        failed += b.failed;
    }
    std::printf("SUMMARY %u %u\n", passed, failed);
    return failed ? 1 : 0;
}
