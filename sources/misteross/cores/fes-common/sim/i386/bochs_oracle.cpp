// SPDX-License-Identifier: MIT
// Thin adapter from fes_i386::Oracle to the Bochs CPU (LGPL-2.1) configured at
// CPU level 3. scripts/fes_i386_oracle.py fetches Bochs at a pinned revision
// into the ignored build directory and links this file with Bochs' CPU
// libraries. Nothing here or in Bochs enters a package or an RTL source list.
//
// Only the CPU is used. This file supplies the platform objects the CPU calls
// (memory, I/O, PIC acknowledge, A20, logging and the few parameters it reads)
// and the instrumentation hooks declared in bochs/instrument.h.
#include "i386_oracle.h"

#define NEED_CPU_REG_SHORTCUTS 1
#include "bochs.h"
#include "cpu/cpu.h"
#include "cpu/decoder/fetchdecode.h"
#include "cpu/icache.h"
#include "iodev/iodev.h"
#include "memory/memory-bochs.h"
#include "pc_system.h"

#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <functional>
#include <stdexcept>
#include <string>
#include <utility>

#if BX_CPU_LEVEL != 3 || !BX_SUPPORT_FPU || BX_SUPPORT_SMP || BX_SUPPORT_X86_64 || !BX_X86_DEBUGGER
#error "the FES i386 oracle requires Bochs configured by scripts/fes_i386_oracle.py"
#endif

// Bochs' decoder (cpu/decoder/fetchdecode32.cc). Opcodes the configured CPU
// level lacks keep their decoded length but execute BxError (#UD).
extern int fetchDecode32(const Bit8u *iptr, bool is_32, bxInstruction_c *i, unsigned remainingInPage);
extern bxIAOpcodeTable BxOpcodesTable[];

namespace {

fes_i386::Bus *bus = nullptr;
bool log_enabled = false;

struct StepContext {
    fes_i386::StepResult *result = nullptr;
    bool boundary_event = false;  // an NMI or INTR was taken at this boundary
    bool delivery_pending = false;  // exception()/HW event awaiting interrupt()
    bool nmi_requested = false;
    bool rep_string = false;  // the instruction being executed is a REP string op
    unsigned iterations = 0;  // REP string iterations the step has run
};
StepContext context;
std::function<void(unsigned)> iteration_hook;

// The AT coupling of a fitted 387 (Oracle::set_at_coprocessor). The 386's
// ERROR# input is tied inactive; the 387's ERROR# drives IRQ 13 and, going
// active, clocks a flip-flop that holds BUSY# active until a write to port
// F0h (80387 Programmer's Reference Manual 231917-001, Appendix F; IBM PC AT
// Technical Reference 6183355, "NMI and Coprocessor Controls").
struct AtCoprocessor {
    bool fitted = false;
    bool busy = false;   // the board's BUSY# latch
    bool error = false;  // the 387's ERROR# after the last step or port write
};
AtCoprocessor at387;

// The 387 drives ERROR# while its status word's ES bit is set.
bool x87_error() { return bx_cpu.the_i387.swd & FPU_SW_Summary; }

// Bochs's IGNNE# input stands for the AT's wiring: with the 387 fitted, a
// waiting ESC whose pending error Bochs would report runs on, as on a board
// whose CPU ERROR# is tied inactive, except while the latch holds BUSY#.
// Bochs's own wait for IGNNE# (FPU_check_pending_exceptions) is then never
// reached, since a waiting ESC holds before it runs (fes_bx_before_execution).
void update_ignne() { bx_pc_system.IGNNE = at387.fitted && !at387.busy; }

// ERROR# going active latches BUSY#.
void sample_error() {
    const bool now = x87_error();
    if (at387.fitted && now && !at387.error) at387.busy = true;
    at387.error = now;
    update_ignne();
}

// Whether the instruction about to run waits for BUSY#: every ESC but FNINIT
// and FNCLEX, and WAIT (80386 Hardware Reference Manual, 5.1: "When the 80386
// encounters any coprocessor instruction except FNINIT and FNCLEX, the BUSY#
// input must be inactive"), unless #NM or #UD comes first: EM or TS for an
// ESC, MP and TS for WAIT, LOCK for an ESC.
bool waits_for_busy(const bxInstruction_c *i) {
    const unsigned op = i->getIaOpcode();
    const Bit32u cr0 = bx_cpu.cr0.get32();
    if (op == BX_IA_FWAIT) return (cr0 & 0xa) != 0xa;
    if (!(BxOpcodesTable[op].opflags & BX_PREPARE_FPU) || op == BX_IA_FNINIT || op == BX_IA_FNCLEX) return false;
    return !(cr0 & 0xc) && !i->getLock();
}

bool is_prefix(uint8_t b) {
    switch (b) {
    case 0x26: case 0x2e: case 0x36: case 0x3e: case 0x64: case 0x65:
    case 0x66: case 0x67: case 0xf0: case 0xf2: case 0xf3:
        return true;
    default:
        return false;
    }
}

void stop_at_next_boundary() {
    bx_cpu.async_event |= 1;
    bx_pc_system.kill_bochs_request = 1;
}

void vlog(const char *level, const char *fmt, va_list ap) {
    if (!log_enabled) return;
    std::fprintf(stderr, "[bochs %s] ", level);
    std::vfprintf(stderr, fmt, ap);
    std::fputc('\n', stderr);
}

std::string format(const char *fmt, va_list ap) {
    char buffer[512];
    std::vsnprintf(buffer, sizeof buffer, fmt, ap);
    return buffer;
}

class FalseParameters : public bx_simulator_interface_c {
public:
    // The CPU reads reset_on_triple_fault and, outside level 3, CPUID
    // settings. Triple faults shut down, as on a 386.
    bx_param_bool_c *get_param_bool(const char *, bx_param_c *) override {
        static bx_param_bool_c value(nullptr, "fes_false", "", "", 0);
        return &value;
    }
};
FalseParameters parameters;

class Pic : public bx_pic_stub_c {
public:
    Bit8u IAC() override { return bus->interrupt_acknowledge(); }
};
Pic pic;

}  // namespace

// ---- Platform objects the Bochs CPU links against -------------------------

BX_CPU_C bx_cpu;
BX_MEM_C bx_mem;
bx_pc_system_c bx_pc_system;
bx_devices_c bx_devices;
bx_debug_t bx_dbg;
static logfunctions oracle_log;
logfunctions *genlog = &oracle_log;
logfunctions *pluginlog = &oracle_log;
logfunctions *siminterface_log = &oracle_log;
bx_simulator_interface_c *SIM = &parameters;
// Bochs' parameter tree (gui/paramtree.cc) backs the CPU's register_state()
// and the parameter above; it needs these roots but no GUI.
bx_list_c *root_param = nullptr;
class bx_gui_c *bx_gui = nullptr;

logfunctions::logfunctions() : name(nullptr), prefix(nullptr), logio(nullptr) {}
logfunctions::logfunctions(iofunctions *) : logfunctions() {}
logfunctions::~logfunctions() {}
void logfunctions::put(const char *) {}
void logfunctions::put(const char *, const char *) {}
void logfunctions::ldebug(const char *fmt, ...) { va_list ap; va_start(ap, fmt); vlog("debug", fmt, ap); va_end(ap); }
void logfunctions::info(const char *fmt, ...) { va_list ap; va_start(ap, fmt); vlog("info", fmt, ap); va_end(ap); }
void logfunctions::lwarn(const char *fmt, ...) { va_list ap; va_start(ap, fmt); vlog("warn", fmt, ap); va_end(ap); }
void logfunctions::error(const char *fmt, ...) { va_list ap; va_start(ap, fmt); vlog("error", fmt, ap); va_end(ap); }
void logfunctions::panic(const char *fmt, ...) {
    va_list ap; va_start(ap, fmt); std::string message = format(fmt, ap); va_end(ap);
    throw std::runtime_error("Bochs panic: " + message);
}
void logfunctions::fatal1(const char *fmt, ...) {
    va_list ap; va_start(ap, fmt); std::string message = format(fmt, ap); va_end(ap);
    throw std::runtime_error("Bochs fatal: " + message);
}

BX_MEMORY_STUB_C::BX_MEMORY_STUB_C() : len(0), allocated(0), block_size(0), actual_vector(nullptr),
    vector(nullptr), blocks(nullptr), rom(nullptr), bogus(nullptr), used_blocks(0) {}
BX_MEMORY_STUB_C::~BX_MEMORY_STUB_C() {}
Bit64u BX_MEMORY_STUB_C::get_memory_len() { return BX_CONST64(1) << 32; }
BX_MEM_C::BX_MEM_C() {}
BX_MEM_C::~BX_MEM_C() {}

Bit8u *BX_MEM_C::getHostMemAddr(BX_CPU_C *, bx_phy_address addr, unsigned rw) {
    // Bochs ORs page offsets into this pointer, so it must be page aligned.
    Bit8u *page = bus->page(Bit32u(A20ADDR(addr)), rw == BX_WRITE || rw == BX_RW);
    if (reinterpret_cast<uintptr_t>(page) & 0xfff) throw std::logic_error("Bus::page returned an unaligned page");
    return page;
}

void BX_MEM_C::readPhysicalPage(BX_CPU_C *, bx_phy_address addr, unsigned len, void *data) {
    Bit32u paddr = Bit32u(A20ADDR(addr));
    if (Bit8u *page = bus->page(paddr, false)) std::memcpy(data, page + (paddr & 0xfff), len);
    else bus->read(paddr, len, static_cast<uint8_t *>(data));
}

void BX_MEM_C::writePhysicalPage(BX_CPU_C *, bx_phy_address addr, unsigned len, void *data) {
    Bit32u paddr = Bit32u(A20ADDR(addr));
    pageWriteStampTable.decWriteStamp(paddr, len);
    if (Bit8u *page = bus->page(paddr, true)) std::memcpy(page + (paddr & 0xfff), data, len);
    else bus->write(paddr, len, static_cast<const uint8_t *>(data));
}

bool BX_MEM_C::dbg_fetch_mem(BX_CPU_C *cpu, bx_phy_address addr, unsigned len, Bit8u *buf) {
    readPhysicalPage(cpu, addr, len, buf);
    return true;
}

bx_pc_system_c::bx_pc_system_c() {
    currCountdown = currCountdownPeriod = 0x40000000;
    ticksTotal = 0;
    HRQ = false;
    enable_a20 = true;
    a20_mask = ~bx_phy_address(0);
    IGNNE = false;
    kill_bochs_request = 0;
}
void bx_pc_system_c::countdownEvent() {
    ticksTotal += currCountdownPeriod;
    currCountdown = currCountdownPeriod;
}
int bx_pc_system_c::Reset(unsigned type) {
    bx_cpu.reset(type);
    return 0;
}

bx_devices_c::bx_devices_c() {
    init_stubs();
    pluginPicDevice = &pic;
}
void bx_devices_c::init_stubs() {
    // Bochs' own no-op device stubs; the CPU reaches the x87 error (FERR) one.
    pluginCmosDevice = &stubCmos;
    pluginDmaDevice = &stubDma;
    pluginHardDrive = &stubHardDrive;
    pluginPicDevice = &stubPic;
    pluginPitDevice = &stubPit;
    pluginSpeaker = &stubSpeaker;
    pluginVgaDevice = &stubVga;
#if BX_SUPPORT_IODEBUG
    pluginIODebug = &stubIODebug;
#endif
#if BX_SUPPORT_GAMEPORT
    pluginGameport = &stubGameport;
#endif
    pluginExtFpuIRQ = &stubExtFpuIRQ;
}
bx_devices_c::~bx_devices_c() {}
Bit32u bx_devices_c::inp(Bit16u port, unsigned len) { return bus->in(port, len); }
void bx_devices_c::outp(Bit16u port, Bit32u value, unsigned len) {
    // The AT's coprocessor controls, by byte: F0h clears the BUSY# latch,
    // F1h resets the 387. The machine sees the write as any other.
    for (unsigned n = 0; at387.fitted && n < len; ++n) {
        const unsigned at = port + n;
        if (at == 0xf0) at387.busy = false;
        if (at == 0xf1) {
            // A 387's RESET: FNINIT's state but for the invalid-operation
            // mask clear and IE and ES set, so ERROR# is active (387 PRM 4.8.1
            // and Appendix C.1). The 387 is not busy, so the latch stays as
            // it is. The x87 pointers are the CPU's and stay.
            i387_t &x87 = bx_cpu.the_i387;
            const i387_t kept = x87;
            x87.init();
            x87.cwd = 0x037e;
            x87.swd = 0x0081;
            x87.fip = kept.fip;
            x87.fdp = kept.fdp;
            x87.fcs = kept.fcs;
            x87.fds = kept.fds;
            x87.foo = kept.foo;
            at387.error = true;
        }
    }
    update_ignne();
    bus->out(port, len, value);
}

// ---- Instrumentation -------------------------------------------------------

void fes_bx_before_execution(bxInstruction_c *i) {
    if (!context.result) return;
    if (context.boundary_event) {
        // The step took an NMI or INTR; stop at the handler's first instruction.
        stop_at_next_boundary();
        longjmp(bx_cpu.jmp_buf_env, 1);
    }
    context.result->eip_before = Bit32u(bx_cpu.prev_rip);
    if (at387.fitted && at387.busy && waits_for_busy(i)) {
        // Held on BUSY# before the instruction starts: RIP is still its
        // first byte's, and the step ends with the CPU waiting.
        bx_cpu.activity_state = BX_CPU_C::BX_ACTIVITY_WAIT_FOR_X87;
        context.result->x87_held = true;
        stop_at_next_boundary();
        longjmp(bx_cpu.jmp_buf_env, 1);
    }
    // Read before execution: a store to this page can invalidate *i.
    context.rep_string = i->lockRepUsedValue() >= 2 && std::strncmp(i->getIaOpcodeNameShort(), "REP_", 4) == 0;
}

void fes_bx_after_execution(bxInstruction_c *) {
    if (!context.result) return;
    // Bochs pauses a REP string instruction between iterations whenever it
    // stores near code it has decoded, and for a debug trap (single step, a
    // data breakpoint). The instruction is still running, so the step
    // continues it, and a trap is taken within the step after that
    // iteration, as an interrupt raised there is.
    bool paused = context.rep_string && Bit32u(bx_cpu.prev_rip) == context.result->eip_before;
    if (paused) return;
    context.result->retired = true;
    stop_at_next_boundary();
}

void fes_bx_repeat_iteration(bxInstruction_c *) {
    // Bochs then steps the count and, unless that was the last iteration,
    // stops between iterations for a pending event the hook raised.
    if (!context.result) return;
    ++context.iterations;
    if (iteration_hook) iteration_hook(context.iterations);
}

void fes_bx_exception(unsigned vector, unsigned error_code) {
    if (!context.result) return;
    context.result->events.push_back({fes_i386::EventKind::Exception, uint8_t(vector), error_code,
                                      uint32_t(context.result->writes.size())});
    context.delivery_pending = true;
    stop_at_next_boundary();
}

void fes_bx_hwinterrupt(unsigned vector) {
    if (!context.result) return;
    bool nmi = context.nmi_requested && vector == 2 && !bx_cpu.is_pending(BX_EVENT_NMI);
    if (nmi) context.nmi_requested = false;
    context.result->events.push_back({nmi ? fes_i386::EventKind::Nmi : fes_i386::EventKind::ExternalInterrupt,
                                      uint8_t(vector), 0, uint32_t(context.result->writes.size())});
    context.delivery_pending = true;
    context.boundary_event = true;
}

void fes_bx_interrupt(unsigned vector) {
    if (!context.result) return;
    if (context.delivery_pending) {
        context.delivery_pending = false;
        return;
    }
    context.result->events.push_back({fes_i386::EventKind::SoftwareInterrupt, uint8_t(vector), 0,
                                      uint32_t(context.result->writes.size())});
}

void fes_bx_lin_access(bx_address lin, bx_address phy, unsigned len, unsigned rw) {
    if (!context.result) return;
    if (rw == BX_WRITE || rw == BX_RW) context.result->writes.push_back({Bit32u(lin), Bit32u(phy), uint8_t(len)});
    if (rw == BX_READ || rw == BX_RW) context.result->reads.push_back({Bit32u(lin), Bit32u(phy), uint8_t(len)});
}

// ---- Oracle -----------------------------------------------------------------

namespace fes_i386 {

namespace {

Segment read_segment(const bx_segment_reg_t &segment) {
    Segment value;
    value.selector = segment.selector.value;
    value.base = Bit32u(segment.cache.u.segment.base);
    value.limit = segment.cache.u.segment.limit_scaled;
    value.ar = uint16_t((BX_CPU_C::get_descriptor_h(&segment.cache) >> 8) & 0xf0ff);
    value.valid = segment.cache.valid != 0;
    return value;
}

void write_segment(bx_segment_reg_t &segment, const Segment &value) {
    bx_cpu.set_segment_ar_data(&segment, value.valid, value.selector, value.base, value.limit, value.ar);
}

class BochsOracle final : public Oracle {
public:
    explicit BochsOracle(Bus &target) {
        if (bus) throw std::logic_error("only one Bochs oracle may exist per process");
        bus = &target;
        log_enabled = std::getenv("FES_I386_ORACLE_LOG") != nullptr;
        // The AT coupling is off for every new oracle (at387 is global).
        at387 = AtCoprocessor{};
        bx_cpu.initialize();
        reset();
    }
    ~BochsOracle() override {
        at387 = AtCoprocessor{};
        bx_pc_system.IGNNE = false;
        bus = nullptr;
    }

    std::string name() const override { return "bochs-level3"; }

    Decoded decode(const uint8_t *bytes, unsigned available, bool code32) const override {
        Decoded result;
        // Below CPU level 6 Bochs keeps the 0F 38 and 0F 3A three-byte escapes
        // in its descriptor table with no opcode table, and decoding them
        // dereferences null. The i386 raises #UD for both; their length is
        // not architectural, so report the two opcode bytes.
        unsigned limit = available < 15 ? available : 15, at = 0;
        while (at < limit && is_prefix(bytes[at])) ++at;
        if (at + 1 < limit && bytes[at] == 0x0f && (bytes[at + 1] == 0x38 || bytes[at + 1] == 0x3a))
            return Decoded{true, true, uint8_t(at + 2)};
        bxInstruction_c i;
        if (!available || fetchDecode32(bytes, code32, &i, available) < 0) return result;
        result.complete = true;
        result.length = uint8_t(i.ilen());
        unsigned opcode = i.getIaOpcode();
        // The handler choice mirrors BX_CPU_C::assignHandler.
        BxExecutePtr_tR handler = i.modC0() ? BxOpcodesTable[opcode].execute2 : BxOpcodesTable[opcode].execute1;
        result.undefined = opcode == BX_IA_ERROR || handler == &BX_CPU_C::BxError;
        return result;
    }

    void reset() override {
        bx_cpu.reset(BX_RESET_HARDWARE);
        context = StepContext{};
        at387.busy = false;
        at387.error = x87_error();
        update_ignne();
    }

    State get_state() const override {
        State state;
        for (int n = 0; n < REG_COUNT; ++n) state.gpr[n] = BX_READ_32BIT_REG(n);
        state.eflags = bx_cpu.read_eflags();
        state.eip = Bit32u(bx_cpu.prev_rip);
        for (int n = 0; n < SEG_COUNT; ++n) state.seg[n] = read_segment(bx_cpu.sregs[n]);
        state.ldtr = read_segment(bx_cpu.ldtr);
        state.tr = read_segment(bx_cpu.tr);
        state.gdtr = {Bit32u(bx_cpu.gdtr.base), bx_cpu.gdtr.limit};
        state.idtr = {Bit32u(bx_cpu.idtr.base), bx_cpu.idtr.limit};
        state.cr0 = bx_cpu.cr0.get32();
        state.cpl = uint8_t(bx_cpu.sregs[BX_SEG_REG_CS].selector.rpl);
        state.cr2 = Bit32u(bx_cpu.cr2);
        state.cr3 = Bit32u(bx_cpu.cr3);
        for (int n = 0; n < 4; ++n) state.dr[n] = Bit32u(bx_cpu.dr[n]);
        state.dr6 = bx_cpu.dr6.get32();
        state.dr7 = bx_cpu.dr7.get32();
        return state;
    }

    // Mirrors Bochs' own RSM state restore, without SMM bookkeeping.
    void set_state(const State &state) override {
        bx_cpu.cr0.set32(state.cr0);
        bx_cpu.cr2 = state.cr2;
        bx_cpu.cr3 = state.cr3;
        bx_cpu.setEFlags(state.eflags);
        for (int n = 0; n < REG_COUNT; ++n) BX_WRITE_32BIT_REGZ(n, state.gpr[n]);
        RIP = bx_cpu.prev_rip = state.eip;
        for (int n = 0; n < 4; ++n) bx_cpu.dr[n] = state.dr[n];
        bx_cpu.dr6.set32(state.dr6);
        bx_cpu.dr7.set32(state.dr7);
        bx_cpu.gdtr.base = state.gdtr.base;
        bx_cpu.gdtr.limit = state.gdtr.limit;
        bx_cpu.idtr.base = state.idtr.base;
        bx_cpu.idtr.limit = state.idtr.limit;
        for (int n = 0; n < SEG_COUNT; ++n) write_segment(bx_cpu.sregs[n], state.seg[n]);
        // In real mode a selector's RPL is 0, as Bochs's own real-mode loads
        // leave it. Bochs keeps CPL as CS's RPL, which protected-mode entry
        // leaves at 0 whatever CS's selector: state.cpl.
        if (!(state.cr0 & 1))
            for (int n = 0; n < SEG_COUNT; ++n) bx_cpu.sregs[n].selector.rpl = 0;
        else
            bx_cpu.sregs[BX_SEG_REG_CS].selector.rpl = state.cpl & 3;
        write_segment(bx_cpu.ldtr, state.ldtr);
        write_segment(bx_cpu.tr, state.tr);
        bx_cpu.activity_state = BX_CPU_C::BX_ACTIVITY_STATE_ACTIVE;
        bx_cpu.inhibit_mask = 0;
        // A pending debug trap stays: a lockstep rule that hands the oracle
        // the RTL's state between instructions must not drop the single step
        // or data breakpoint the last instruction armed (reset() clears it).
        bx_cpu.last_exception_type = 0;
        bx_cpu.speculative_rsp = false;
        bx_cpu.handleCpuContextChange();
    }

    StepResult step() override {
        StepResult result;
        result.eip_before = Bit32u(bx_cpu.prev_rip);
        if (bx_cpu.activity_state != BX_CPU_C::BX_ACTIVITY_STATE_ACTIVE && !wake_pending()) {
            finish(result);
            return result;
        }
        context.result = &result;
        context.boundary_event = false;
        context.delivery_pending = false;
        context.iterations = 0;
        bx_pc_system.kill_bochs_request = 0;
        bx_cpu.async_event |= 1;  // evaluate boundary events before fetching
        try {
            bx_cpu.cpu_loop();
        } catch (...) {
            context.result = nullptr;
            throw;
        }
        context.result = nullptr;
        finish(result);
        sample_error();
        return result;
    }

    void set_intr(bool level) override {
        if (level) bx_cpu.raise_INTR();
        else bx_cpu.clear_INTR();
    }

    void nmi() override {
        context.nmi_requested = true;
        bx_cpu.deliver_NMI();
    }

    void set_iteration_hook(std::function<void(unsigned)> hook) override { iteration_hook = std::move(hook); }

    void set_a20(bool enabled) override {
        bx_pc_system.enable_a20 = enabled;
        bx_pc_system.a20_mask = enabled ? ~bx_phy_address(0) : ~bx_phy_address(1u << 20);
        bx_cpu.TLB_flush();
        flushICaches();
    }

    void invalidate(uint32_t paddr, uint32_t length) override {
        if (!length) return;
        for (uint64_t page = paddr & ~0xfffu; page < uint64_t(paddr) + length; page += 0x1000)
            pageWriteStampTable.decWriteStamp(bx_phy_address(page));
    }

    X87Pointers x87_pointers() const override {
        const i387_t &x87 = bx_cpu.the_i387;
        return {Bit32u(x87.fip), Bit32u(x87.fdp), x87.fcs, x87.fds};
    }

    void set_x87_pointers(const X87Pointers &pointers) override {
        i387_t &x87 = bx_cpu.the_i387;
        x87.fip = pointers.ip;
        x87.fdp = pointers.dp;
        x87.fcs = pointers.cs;
        x87.fds = pointers.ds;
    }

    X87State x87_state() const override {
        const i387_t &x87 = bx_cpu.the_i387;
        X87State state;
        state.control = x87.cwd;
        state.status = x87.get_status_word();
        state.opcode = x87.foo;
        state.pointers = x87_pointers();
        for (unsigned n = 0; n < 8; ++n) {
            const floatx80 &reg = x87.st_space[n];
            state.r[n] = {reg.signif, reg.signExp};
            // FNSTENV's tag: empty stays empty, any other from the value.
            const unsigned tag = (x87.twd >> (2 * n)) & 3;
            state.tag |= uint16_t((tag == FPU_Tag_Empty ? tag : unsigned(FPU_tagof(reg)) & 3) << (2 * n));
        }
        return state;
    }

    void set_at_coprocessor(bool fitted) override {
        at387 = AtCoprocessor{fitted, false, x87_error()};
        update_ignne();
    }

    bool x87_irq13() const override { return at387.fitted && x87_error(); }

    bool x87_busy_latched() const override { return at387.busy; }

    // Bochs models a 387. Its words as the empty socket's data bus reads
    // them, with TOP 7 so the status word's image is all ones too.
    void empty_socket(bool status) override {
        i387_t &x87 = bx_cpu.the_i387;
        x87.cwd = 0xffff;
        x87.twd = 0xffff;
        x87.foo = 0x7ff;
        x87.swd = status ? 0xffff : 0;
        x87.tos = status ? 7 : 0;
        for (floatx80 &reg : x87.st_space) {
            reg.signif = ~Bit64u(0);
            reg.signExp = 0xffff;
        }
    }

private:
    static bool wake_pending() {
        return (bx_cpu.is_pending(BX_EVENT_PENDING_INTR) && bx_cpu.get_IF())
            || bx_cpu.is_unmasked_event_pending(BX_EVENT_NMI)
            || (bx_cpu.activity_state == BX_CPU_C::BX_ACTIVITY_WAIT_FOR_X87 && bx_pc_system.IGNNE);
    }

    static void finish(StepResult &result) {
        result.halted = bx_cpu.activity_state == BX_CPU_C::BX_ACTIVITY_STATE_HLT;
        result.shutdown = bx_cpu.activity_state == BX_CPU_C::BX_ACTIVITY_STATE_SHUTDOWN;
        result.x87_held = bx_cpu.activity_state == BX_CPU_C::BX_ACTIVITY_WAIT_FOR_X87;
    }
};

}  // namespace

std::unique_ptr<Oracle> make_oracle(Bus &target) {
    return std::make_unique<BochsOracle>(target);
}

}  // namespace fes_i386
