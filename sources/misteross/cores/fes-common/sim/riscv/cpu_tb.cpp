// SPDX-License-Identifier: MIT
// fes_rv32_cpu host simulation: directed programs with specification-derived
// expectations, interrupt and trap behaviour, bus wait states and faults, and
// random instruction streams checked in lockstep against rv32_model.h.
#include "Vfes_rv32_cpu.h"
#include "Vfes_rv32_cpu___024root.h"
#include "verilated.h"
#include "rv32_asm.h"
#include "rv32_model.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <algorithm>
#include <iostream>
#include <map>
#include <string>
#include <vector>

using namespace rv;

namespace {

std::vector<std::string> trace;   // recent instruction completions
[[noreturn]] void fail(const std::string &message) {
    std::cerr << "fes_rv32_cpu: " << message << '\n';
    for (const std::string &line : trace) std::cerr << "  " << line << '\n';
    std::exit(EXIT_FAILURE);
}
void require(bool ok, const std::string &message) { if (!ok) fail(message); }
std::string hex(uint32_t v) { char b[16]; std::snprintf(b, sizeof b, "0x%08x", v); return b; }

struct Rng {
    uint64_t s;
    explicit Rng(uint64_t seed) : s(seed * 0x9e3779b97f4a7c15ull + 1) {}
    uint32_t next() { s ^= s << 13; s ^= s >> 7; s ^= s << 17; return uint32_t(s >> 11); }
    uint32_t below(uint32_t n) { return next() % n; }
};

// Small assembler with labels for the directed programs.
struct Asm {
    std::vector<uint32_t> code;
    struct Fix { size_t index; int label; bool jal; };
    std::vector<Fix> fixes;
    std::vector<int32_t> labels;
    uint32_t base;
    explicit Asm(uint32_t base_addr = 0) : base(base_addr) {}
    uint32_t here() const { return base + 4 * uint32_t(code.size()); }
    int label() { labels.push_back(-1); return int(labels.size()) - 1; }
    void place(int l) { labels[l] = int32_t(here()); }
    void emit(uint32_t w) { code.push_back(w); }
    void branch(uint32_t f3, uint32_t a, uint32_t b, int l) { fixes.push_back({code.size(), l, false}); emit(B(0, b, a, f3)); }
    void jal(uint32_t rd, int l) { fixes.push_back({code.size(), l, true}); emit(J(0, rd)); }
    void li(uint32_t rd, uint32_t value) {
        uint32_t hi = (value + 0x800) >> 12;
        int32_t lo = int32_t(value) - int32_t(hi << 12);
        if (hi == 0) emit(ADDI(rd, 0, lo));
        else { emit(LUI(rd, hi & 0xfffff)); if (lo) emit(ADDI(rd, rd, lo)); }
    }
    std::vector<uint32_t> finish() {
        for (const Fix &f : fixes) {
            require(labels[f.label] >= 0, "unplaced label");
            int32_t off = labels[f.label] - int32_t(base + 4 * f.index);
            uint32_t w = code[f.index];
            code[f.index] = f.jal ? J(off, (w >> 7) & 31) : B(off, (w >> 20) & 31, (w >> 15) & 31, (w >> 12) & 7);
        }
        return code;
    }
};

struct Harness {
    Vfes_rv32_cpu dut;
    TestSystem rtl_sys, model_sys;
    Model model{model_sys};
    Rng rng;
    int max_wait;
    bool pending = false;
    int wait_left = 0;
    uint32_t held_addr = 0, held_wdata = 0;
    uint8_t held_strb = 0;
    bool held_instr = false;
    uint64_t cycles = 0;
    uint64_t steps = 0, traps = 0, interrupts = 0, unpredictable = 0;
    bool verbose = false;

    Harness(uint64_t seed, int waits) : rng(seed), max_wait(waits) {}

    void load(const std::vector<uint32_t> &words, uint32_t address) {
        for (size_t i = 0; i < words.size(); ++i) {
            std::memcpy(&rtl_sys.ram[address + 4 * i], &words[i], 4);
            std::memcpy(&model_sys.ram[address + 4 * i], &words[i], 4);
        }
    }
    void fill_random(uint32_t address, uint32_t bytes) {
        for (uint32_t i = 0; i < bytes; ++i) {
            uint8_t v = uint8_t(rng.next());
            rtl_sys.ram[address + i] = v;
            model_sys.ram[address + i] = v;
        }
    }

    void reset() {
        dut.clk = 0; dut.reset = 1; dut.bus_ready = 0; dut.bus_error = 0; dut.bus_rdata = 0;
        dut.irq_external = dut.irq_timer = dut.irq_software = 0; dut.mtime = 0;
        dut.eval();
        for (int i = 0; i < 3; ++i) { dut.clk = 1; dut.eval(); dut.clk = 0; dut.eval(); }
        dut.reset = 0; dut.eval();
        model.reset(0);
        pending = false;
    }

    // One clock cycle; returns after the rising edge.
    void cycle() {
        dut.clk = 0;
        dut.irq_external = rtl_sys.irq_lines & 1;
        dut.irq_timer = (rtl_sys.irq_lines >> 1) & 1;
        dut.irq_software = (rtl_sys.irq_lines >> 2) & 1;
        dut.mtime = cycles;
        dut.bus_ready = 0; dut.bus_error = 0;
        dut.eval();
        bool store_now = false;
        uint32_t store_addr = 0, store_data = 0;
        uint8_t store_strb = 0;
        if (dut.bus_valid) {
            if (!pending) {
                pending = true;
                held_addr = dut.bus_addr; held_wdata = dut.bus_wdata; held_strb = dut.bus_wstrb;
                held_instr = dut.bus_instr;
                wait_left = max_wait ? (rng.below(2) ? 0 : int(rng.below(uint32_t(max_wait) + 1))) : 0;
                require(!held_instr || held_strb == 0, "instruction fetch must not write");
            } else {
                require(dut.bus_addr == held_addr && dut.bus_wdata == held_wdata &&
                        dut.bus_wstrb == held_strb && dut.bus_instr == held_instr,
                        "bus request changed while waiting for ready");
            }
            if (wait_left == 0) {
                dut.bus_ready = 1;
                uint32_t data = 0;
                bool ok;
                if (held_strb) {
                    ok = rtl_sys.write(held_addr, held_wdata, held_strb);
                    store_now = ok; store_addr = held_addr; store_data = held_wdata; store_strb = held_strb;
                } else {
                    ok = rtl_sys.read(held_addr, data);
                    if (held_instr && !TestSystem::in_ram(held_addr)) ok = false;
                }
                dut.bus_error = !ok;
                dut.bus_rdata = data;
                pending = false;
            } else {
                --wait_left;
            }
        } else {
            require(!pending, "request dropped before ready");
        }
        dut.eval();
        observe(store_now, store_addr, store_data, store_strb);
        dut.clk = 1;
        dut.eval();
        ++cycles;
    }

    void observe(bool store_now, uint32_t store_addr, uint32_t store_data, uint8_t store_strb) {
        if (dut.trap && !dut.step) {
            ++interrupts;
            require(model.interrupt_enabled_pending(),
                    "interrupt taken while the model has none enabled and pending at " + hex(dut.step_pc));
            require(dut.trap_cause == model.interrupt_cause(),
                    "interrupt cause " + hex(dut.trap_cause) + " expected " + hex(model.interrupt_cause()));
            require(dut.debug_pc == model.pc, "interrupt mepc differs from model pc");
            model.take_interrupt(dut.trap_cause);
            require(!dut.wb_valid && !store_now, "interrupt entry has no side effects");
            return;
        }
        if (!dut.step) {
            require(!dut.wb_valid, "register write outside an instruction completion");
            require(!store_now, "store completion without an instruction completion");
            return;
        }
        ++steps;
        StepResult e = model.step();
        const std::string at = " at pc " + hex(dut.step_pc);
        require(dut.step_pc == e.pc, "pc " + hex(dut.step_pc) + " model " + hex(e.pc));
        require(bool(dut.trap) == e.trap, std::string(e.trap ? "missing" : "unexpected") + " trap" + at);
        if (e.trap) {
            ++traps;
            require(dut.trap_cause == e.cause, "cause " + hex(dut.trap_cause) + " model " + hex(e.cause) + at);
            require(bool(dut.retired) == false, "trapping instruction retired" + at);
        } else {
            require(bool(dut.retired), "retired not asserted" + at);
        }
        require(bool(dut.wb_valid) == e.wb, std::string(e.wb ? "missing" : "unexpected") + " register write" + at);
        if (e.wb) {
            require(dut.wb_rd == e.rd, "rd differs" + at);
            if (e.wb_unpredictable) { ++unpredictable; model.adopt(e.rd, dut.wb_data); }
            else require(dut.wb_data == e.wb_data, "x" + std::to_string(e.rd) + " = " + hex(dut.wb_data) +
                                                   " model " + hex(e.wb_data) + at);
        }
        require(store_now == e.store, std::string(e.store ? "missing" : "unexpected") + " store" + at);
        if (e.store)
            require(store_addr == e.store_addr && store_data == e.store_data && store_strb == e.store_strb,
                    "store " + hex(store_addr) + "/" + hex(store_data) + " model " + hex(e.store_addr) + "/" + hex(e.store_data) + at);
        std::string line = hex(e.pc) + (e.trap ? " trap " + hex(e.cause) + " tval " + hex(e.tval) : "") +
                           (e.wb ? " x" + std::to_string(e.rd) + "=" + hex(dut.wb_data) : "") +
                           (e.store ? " [" + hex(e.store_addr) + "]=" + hex(e.store_data) : "");
        if (verbose) std::cout << line << '\n';
        trace.push_back(line);
        if (trace.size() > 48) trace.erase(trace.begin());
    }

    void run(uint64_t max_cycles) {
        while (!rtl_sys.done) {
            require(cycles < max_cycles, "program did not finish within " + std::to_string(max_cycles) + " cycles");
            cycle();
        }
        for (int i = 0; i < 8; ++i) cycle();
        require(model_sys.done, "model did not reach the end marker");
        for (int i = 1; i < 32; ++i)
            require(dut.rootp->fes_rv32_cpu__DOT__regs[i] == model.x[i],
                    "final x" + std::to_string(i) + " " + hex(dut.rootp->fes_rv32_cpu__DOT__regs[i]) + " model " + hex(model.x[i]));
        require(rtl_sys.ram == model_sys.ram, "final memory differs from the model");
    }

    uint32_t word(uint32_t address) const { uint32_t v; std::memcpy(&v, &rtl_sys.ram[address], 4); return v; }
};

// ---------------------------------------------------------------------------
// Directed program. Results are stored at RESULTS + 4*i and compared against
// values taken from the specification, independently of the model.
constexpr uint32_t RESULTS = 0x3000;
constexpr uint32_t SCRATCH = 0x0040;      // trap handler save area (x0-relative)
constexpr uint32_t LOG = 0x0100;
constexpr uint32_t HANDLER = 0x0200;
constexpr uint32_t VECTORS = 0x0400;      // vectored mtvec base (64-byte aligned)
constexpr uint32_t PROGRAM = 0x0800;
constexpr uint32_t DATA = 0x2000;

struct Expectation { uint32_t index, value; int line; };
std::vector<Expectation> expected;
uint32_t result_index = 0;

// Store register `reg` as the next result and record the expected value.
void result_at(Asm &a, uint32_t reg, uint32_t value, int line) {
    a.emit(SW(reg, 20, int32_t(4 * result_index)));
    expected.push_back({result_index, value, line});
    ++result_index;
}
#define result(a, reg, value) result_at(a, reg, value, __LINE__)

// Trap handler. Exceptions: records cause/mepc/mtval, advances mepc past the
// instruction (cause 1 restores the address saved at SCRATCH+8 instead).
// Interrupts: records cause, clears exactly the line it took. x21..x23 are
// handler temporaries; the main program does not use them.
std::vector<uint32_t> handler_program() {
    Asm a(HANDLER);
    a.emit(CSRRS(21, MCAUSE, 0));
    a.emit(CSRRS(22, MEPC, 0));
    a.emit(CSRRS(23, MTVAL, 0));
    a.emit(SW(21, 0, SCRATCH + 0));          // last cause
    a.emit(SW(22, 0, SCRATCH + 4));          // last mepc
    a.emit(SW(23, 0, SCRATCH + 12));         // last mtval
    a.emit(CSRRS(23, MSTATUS, 0));
    a.emit(SW(23, 0, SCRATCH + 16));         // mstatus inside the handler
    a.emit(LW(23, 0, SCRATCH + 20));         // handler entry counter
    a.emit(ADDI(23, 23, 1));
    a.emit(SW(23, 0, SCRATCH + 20));
    int irq = a.label(), fetch_fault = a.label(), done = a.label();
    a.branch(4, 21, 0, irq);                 // blt x21, x0 -> interrupt
    a.emit(ADDI(23, 0, 1));
    a.branch(0, 21, 23, fetch_fault);        // beq cause, 1
    a.emit(ADDI(22, 22, 4));
    a.emit(CSRRW(0, MEPC, 22));
    a.jal(0, done);
    a.place(fetch_fault);
    a.emit(LW(22, 0, SCRATCH + 8));
    a.emit(CSRRW(0, MEPC, 22));
    a.jal(0, done);
    a.place(irq);
    // cause 11 -> line 1, cause 7 -> line 2, cause 3 -> line 4
    a.emit(ADDI(23, 0, 1));
    a.emit(ANDI(22, 21, 0xf));
    int is_ext = a.label(), is_timer = a.label(), clear = a.label();
    a.emit(ADDI(21, 0, 11));
    a.branch(0, 22, 21, is_ext);
    a.emit(ADDI(21, 0, 7));
    a.branch(0, 22, 21, is_timer);
    a.emit(ADDI(23, 0, 4));
    a.jal(0, clear);
    a.place(is_timer);
    a.emit(ADDI(23, 0, 2));
    a.jal(0, clear);
    a.place(is_ext);
    a.emit(ADDI(23, 0, 1));
    a.place(clear);
    a.li(22, TestSystem::MMIO_IRQ_CLEAR);
    a.emit(SW(23, 22, 0));
    a.emit(LW(23, 0, SCRATCH + 24));         // interrupt order log pointer
    a.emit(CSRRS(21, MCAUSE, 0));
    a.emit(SW(21, 23, 0));
    a.emit(ADDI(23, 23, 4));
    a.emit(SW(23, 0, SCRATCH + 24));
    a.place(done);
    a.emit(MRET());
    return a.finish();
}

// Vector table for mtvec mode 1: slot k sets x24 = k and joins the handler.
std::vector<uint32_t> vector_table() {
    Asm a(VECTORS);
    std::vector<int> stubs;
    for (uint32_t slot = 0; slot < 16; ++slot) { stubs.push_back(a.label()); a.jal(0, stubs.back()); }
    for (uint32_t slot = 0; slot < 16; ++slot) {
        a.place(stubs[slot]);
        a.emit(ADDI(24, 0, int32_t(slot)));
        a.emit(JAL(0, int32_t(HANDLER) - int32_t(a.here())));
    }
    return a.finish();
}

std::vector<uint32_t> directed_program() {
    expected.clear();
    result_index = 0;
    Asm a(PROGRAM);
    a.li(20, RESULTS);
    a.li(19, DATA);
    a.li(18, TestSystem::MMIO_BASE);
    a.li(1, HANDLER);
    a.emit(CSRRW(0, MTVEC, 1));
    a.li(1, LOG);
    a.emit(SW(1, 0, SCRATCH + 24));          // interrupt log

    // Immediates and arithmetic (unprivileged spec 2.4).
    a.li(1, 0x12345678);
    result(a, 1, 0x12345678);
    a.emit(ADDI(2, 1, -0x123));
    result(a, 2, 0x12345678u - 0x123u);
    a.emit(LUI(3, 0xfffff));
    result(a, 3, 0xfffff000u);
    a.emit(SLTI(4, 3, 0));                   // -4096 < 0
    result(a, 4, 1);
    a.emit(SLTIU(4, 3, 0));
    result(a, 4, 0);
    a.emit(SLTIU(4, 0, 1));                  // seqz x0
    result(a, 4, 1);
    a.emit(XORI(4, 1, -1));
    result(a, 4, ~0x12345678u);
    a.emit(ORI(4, 1, 0x7ff));
    result(a, 4, 0x12345678u | 0x7ff);
    a.emit(ANDI(4, 1, -16));
    result(a, 4, 0x12345670u);
    a.emit(SLLI(4, 1, 4));
    result(a, 4, 0x23456780u);
    a.emit(SRLI(4, 3, 4));
    result(a, 4, 0x0fffff00u);
    a.emit(SRAI(4, 3, 4));
    result(a, 4, 0xffffff00u);
    a.emit(SRLI(4, 3, 31));
    result(a, 4, 1);
    a.emit(SRAI(4, 3, 31));
    result(a, 4, 0xffffffffu);
    a.li(5, 0x80000000u);
    a.li(6, 1);
    a.emit(ADD(4, 5, 5));                    // overflow wraps
    result(a, 4, 0);
    a.emit(SUB(4, 0, 6));
    result(a, 4, 0xffffffffu);
    a.emit(SLT(4, 5, 6));                    // INT_MIN < 1
    result(a, 4, 1);
    a.emit(SLTU(4, 5, 6));
    result(a, 4, 0);
    a.emit(SLT(4, 6, 5));
    result(a, 4, 0);
    a.li(7, 35);                             // shift amounts use the low five bits
    a.emit(SLL(4, 6, 7));
    result(a, 4, 8);
    a.emit(SRL(4, 5, 7));
    result(a, 4, 0x10000000u);
    a.emit(SRA(4, 5, 7));
    result(a, 4, 0xf0000000u);
    a.emit(XOR(4, 1, 3));
    result(a, 4, 0x12345678u ^ 0xfffff000u);
    a.emit(OR(4, 1, 3));
    result(a, 4, 0x12345678u | 0xfffff000u);
    a.emit(AND(4, 1, 3));
    result(a, 4, 0x12345000u);
    a.emit(ADDI(0, 0, 123));                 // writes to x0 are discarded
    a.emit(ADD(4, 0, 0));
    result(a, 4, 0);
    a.emit(AUIPC(4, 0));
    uint32_t auipc_pc = a.here() - 4;
    result(a, 4, auipc_pc);
    uint32_t auipc2_pc = a.here();
    a.emit(AUIPC(4, 0x10));
    result(a, 4, auipc2_pc + 0x10000);

    // Loads and stores (2.6): little-endian byte lanes, sign extension.
    a.li(1, 0x8899aabb);
    a.emit(SW(1, 19, 0));
    a.emit(LB(4, 19, 0)); result(a, 4, 0xffffffbbu);
    a.emit(LB(4, 19, 1)); result(a, 4, 0xffffffaau);
    a.emit(LB(4, 19, 2)); result(a, 4, 0xffffff99u);
    a.emit(LB(4, 19, 3)); result(a, 4, 0xffffff88u);
    a.emit(LBU(4, 19, 1)); result(a, 4, 0xaa);
    a.emit(LH(4, 19, 0)); result(a, 4, 0xffffaabbu);
    a.emit(LH(4, 19, 2)); result(a, 4, 0xffff8899u);
    a.emit(LHU(4, 19, 2)); result(a, 4, 0x8899);
    a.emit(LW(4, 19, 0)); result(a, 4, 0x8899aabbu);
    a.li(1, 0x11);
    a.emit(SB(1, 19, 6));                    // byte lane 2 of word 1
    a.li(1, 0x2233);
    a.emit(SH(1, 19, 8));
    a.emit(SH(1, 19, 14));
    a.emit(LW(4, 19, 4)); result(a, 4, 0x00110000u);
    a.emit(LW(4, 19, 8)); result(a, 4, 0x00002233u);
    a.emit(LW(4, 19, 12)); result(a, 4, 0x22330000u);
    a.emit(ADDI(1, 19, 100));
    a.emit(SW(1, 1, -100));                  // negative offset
    a.emit(LW(4, 19, 0)); result(a, 4, DATA + 100);

    // Branches (2.5) and jumps.
    a.li(1, 5); a.li(2, 0xfffffffbu); a.li(4, 0);  // 5 and -5
    {
        int l = a.label();
        a.branch(0, 1, 2, l); a.emit(ADDI(4, 4, 1)); a.place(l);   // beq not taken
        l = a.label();
        a.branch(1, 1, 2, l); a.emit(ADDI(4, 4, 2)); a.place(l);   // bne taken
        l = a.label();
        a.branch(4, 2, 1, l); a.emit(ADDI(4, 4, 4)); a.place(l);   // blt -5 < 5 taken
        l = a.label();
        a.branch(6, 2, 1, l); a.emit(ADDI(4, 4, 8)); a.place(l);   // bltu not taken
        l = a.label();
        a.branch(5, 1, 2, l); a.emit(ADDI(4, 4, 16)); a.place(l);  // bge 5 >= -5 taken
        l = a.label();
        a.branch(7, 1, 2, l); a.emit(ADDI(4, 4, 32)); a.place(l);  // bgeu not taken
        l = a.label();
        a.branch(5, 1, 1, l); a.emit(ADDI(4, 4, 64)); a.place(l);  // bge equal taken
    }
    result(a, 4, 1 + 8 + 32);
    {
        int back = a.label(), out = a.label();
        a.li(1, 3); a.li(4, 0);
        a.place(back);
        a.emit(ADDI(4, 4, 10));
        a.emit(ADDI(1, 1, -1));
        a.branch(1, 1, 0, back);                                   // backward loop
        result(a, 4, 30);
        a.jal(5, out);
        a.emit(ADDI(4, 0, 99));
        a.place(out);
        result(a, 5, a.here() - 4);                                // link = jal pc + 4
    }
    {
        int target = a.label();
        a.emit(AUIPC(1, 0));
        uint32_t auipc_at = a.here() - 4;
        a.emit(JALR(6, 1, 13));                                    // bit 0 of the target is cleared
        a.emit(ADDI(4, 0, 98));
        a.place(target);
        require(a.here() == auipc_at + 12, "jalr target layout");
        result(a, 6, auipc_at + 8);
    }

    // CSRs (Zicsr): read-modify-write forms and identity registers.
    a.li(1, 0x5a5a1234);
    a.emit(CSRRW(4, MSCRATCH, 1)); result(a, 4, 0);
    a.emit(CSRRSI(4, MSCRATCH, 0x3)); result(a, 4, 0x5a5a1234u);
    a.emit(CSRRCI(4, MSCRATCH, 0x10)); result(a, 4, 0x5a5a1237u);
    a.emit(CSRRS(4, MSCRATCH, 0)); result(a, 4, 0x5a5a1227u);
    a.emit(CSRRS(4, MISA, 0)); result(a, 4, 0x40000100u);
    a.emit(CSRRS(4, MHARTID, 0)); result(a, 4, 0);
    a.emit(CSRRS(4, MVENDORID, 0)); result(a, 4, 0);
    a.emit(CSRRS(4, MSTATUS, 0)); result(a, 4, 3u << 11);        // MPP=11, MIE=MPIE=0
    a.emit(CSRRWI(0, MTVEC, 1));                                 // mode bits are WARL: 0 or 1
    a.emit(CSRRS(4, MTVEC, 0)); result(a, 4, 1);
    a.emit(CSRRWI(0, MTVEC, 3));
    a.emit(CSRRS(4, MTVEC, 0)); result(a, 4, 1);
    a.li(1, HANDLER);
    a.emit(CSRRW(0, MTVEC, 1));
    a.emit(CSRRSI(0, MEPC, 3));                                  // mepc[1:0] read as zero
    a.emit(CSRRS(4, MEPC, 0)); result(a, 4, 0);
    a.emit(CSRRSI(0, MIP, 0x8));                                 // mip write is ignored, not illegal
    a.emit(CSRRS(4, MIP, 0)); result(a, 4, 0);
    a.emit(CSRRS(4, MIE, 0)); result(a, 4, 0);

    // Counters (Zicntr): instret counts retired instructions exactly.
    a.emit(CSRRS(1, MINSTRET, 0));
    a.emit(NOP()); a.emit(NOP()); a.emit(NOP());
    a.emit(CSRRS(2, INSTRET, 0));
    a.emit(SUB(4, 2, 1)); result(a, 4, 4);
    a.emit(CSRRS(1, MCYCLE, 0));
    a.emit(CSRRS(2, CYCLE, 0));
    a.emit(SLTU(4, 1, 2)); result(a, 4, 1);                      // cycle advances
    a.emit(CSRRS(1, TIME, 0));
    a.emit(CSRRS(2, TIME, 0));
    a.emit(SLTU(4, 1, 2)); result(a, 4, 1);                      // mtime input advances
    a.emit(CSRRW(0, MINSTRET, 0));                               // writable; the writer is not counted
    a.emit(CSRRS(4, MINSTRET, 0)); result(a, 4, 0);
    a.emit(CSRRS(4, MINSTRETH, 0)); result(a, 4, 0);
    a.emit(FENCE()); a.emit(FENCE_I()); a.emit(WFI());           // no-operations

    // Exceptions (privileged spec 3.1.15/3.1.16): cause, mepc and mtval.
    a.emit(SW(0, 0, SCRATCH + 20));
    a.emit(CSRRSI(0, MSTATUS, 0x8));                             // MIE=1 so the handler sees MPIE
    uint32_t ecall_pc = a.here();
    a.emit(ECALL());
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 11);
    a.emit(LW(4, 0, SCRATCH + 4)); result(a, 4, ecall_pc);
    a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, 0);
    a.emit(LW(4, 0, SCRATCH + 16)); result(a, 4, (3u << 11) | 0x80);   // MIE=0, MPIE=1 in handler
    a.emit(CSRRS(4, MSTATUS, 0)); result(a, 4, (3u << 11) | 0x88);     // mret: MIE=MPIE=1
    a.emit(CSRRCI(0, MSTATUS, 0x8));
    uint32_t ebreak_pc = a.here();
    a.emit(EBREAK());
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 3);
    a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, ebreak_pc);
    a.emit(0xffffffffu);                                         // illegal instruction
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 2);
    a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, 0xffffffffu);
    a.emit(R(1, 2, 1, 0, 4, 0x33));                              // MUL: M is not implemented
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 2);
    a.emit(CSRRS(4, 0x7c0, 0));                                  // unknown CSR
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 2);
    a.emit(CSRRWI(0, MISA, 1));                                  // misa write: legal, ignored
    a.emit(CSRRS(4, MISA, 0)); result(a, 4, 0x40000100u);
    a.emit(CSRRWI(0, MHARTID, 0));                               // write to a read-only CSR
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 2);
    a.emit(CSRRS(0, MHARTID, 0));                                // read of a read-only CSR is fine
    a.emit(SW(0, 0, SCRATCH + 0));
    a.emit(LW(4, 19, 2));                                        // misaligned load
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 4);
    a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, DATA + 2);
    a.emit(LH(4, 19, 1));
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 4);
    a.emit(SW(4, 19, 3));                                        // misaligned store
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 6);
    a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, DATA + 3);
    a.emit(LW(4, 19, 4)); result(a, 4, 0x00110000u);             // the misaligned store did not write
    a.li(1, 0x80000000u);
    a.emit(LW(4, 1, 0));                                         // load access fault
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 5);
    a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, 0x80000000u);
    a.emit(SW(4, 1, 8));                                         // store access fault
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 7);
    a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, 0x80000008u);
    {
        uint32_t auipc_at = a.here();
        a.emit(AUIPC(1, 0));
        a.emit(JALR(0, 1, 10));                                  // target bit 1 set: misaligned
        a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 0);
        a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, auipc_at + 10);
        uint32_t beq_at = a.here();
        a.emit(B(2, 0, 0, 0));                                   // beq x0,x0,+2 -> misaligned branch
        a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 0);
        a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, beq_at + 2);
    }
    {
        // Fetch fault: jump outside RAM; the handler returns to the saved address.
        int resume = a.label();
        a.emit(AUIPC(1, 0));
        a.emit(ADDI(1, 1, 20));
        a.emit(SW(1, 0, SCRATCH + 8));
        a.li(2, 0x40000000u);
        a.emit(JALR(0, 2, 0));
        a.place(resume);
        a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 1);
        a.emit(LW(4, 0, SCRATCH + 4)); result(a, 4, 0x40000000u);
        a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, 0x40000000u);
    }
    a.emit(LW(4, 0, SCRATCH + 20)); result(a, 4, 14);            // handler entries so far

    // Interrupts: direct mode. A line raised by a store is recognised at the
    // fetch after the next instruction (one synchroniser cycle), so mepc is
    // the address two instructions past the store.
    a.emit(CSRRSI(0, MIE, 0x8));                                 // MSIE
    a.li(1, 0x880);
    a.emit(CSRRS(0, MIE, 1));                                    // MTIE, MEIE
    a.emit(CSRRS(4, MIE, 0)); result(a, 4, 0x888);
    a.li(1, 1);
    a.emit(SW(1, 18, 4));                                        // raise external while MIE=0
    a.emit(NOP()); a.emit(NOP());
    a.emit(LW(4, 0, SCRATCH + 20)); result(a, 4, 14);            // not taken yet
    a.emit(CSRRS(4, MIP, 0)); result(a, 4, 0x800);
    uint32_t enable_pc = a.here();
    a.emit(CSRRSI(0, MSTATUS, 0x8));                             // MIE=1: taken before the next instruction
    a.emit(NOP());
    a.emit(LW(4, 0, SCRATCH + 20)); result(a, 4, 15);
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 0x8000000bu);
    a.emit(LW(4, 0, SCRATCH + 4)); result(a, 4, enable_pc + 4);
    a.emit(LW(4, 0, SCRATCH + 12)); result(a, 4, 0);
    a.emit(LW(4, 0, SCRATCH + 16)); result(a, 4, (3u << 11) | 0x80);
    a.emit(CSRRS(4, MSTATUS, 0)); result(a, 4, (3u << 11) | 0x88);
    a.li(1, 7);
    uint32_t raise_pc = a.here();
    a.emit(SW(1, 18, 4));                                        // all three at once: priority order
    a.emit(NOP()); a.emit(NOP()); a.emit(NOP()); a.emit(NOP()); a.emit(NOP()); a.emit(NOP());
    a.emit(NOP()); a.emit(NOP()); a.emit(NOP()); a.emit(NOP()); a.emit(NOP()); a.emit(NOP());
    a.emit(LW(4, 0, SCRATCH + 20)); result(a, 4, 18);
    a.emit(LW(4, 0, LOG + 0)); result(a, 4, 0x8000000bu);   // the first interrupt above
    a.emit(LW(4, 0, LOG + 4)); result(a, 4, 0x8000000bu);   // external
    a.emit(LW(4, 0, LOG + 8)); result(a, 4, 0x80000003u);   // software
    a.emit(LW(4, 0, LOG + 12)); result(a, 4, 0x80000007u);   // timer
    a.emit(LW(4, 0, SCRATCH + 4)); result(a, 4, raise_pc + 8);   // last mepc: resumed after the store
    // Vectored mode: external interrupts enter slot 11, exceptions slot 0.
    a.li(1, VECTORS | 1);
    a.emit(CSRRW(0, MTVEC, 1));
    a.emit(ADDI(24, 0, -1));
    a.li(1, 1);
    a.emit(SW(1, 18, 4));
    a.emit(NOP()); a.emit(NOP()); a.emit(NOP()); a.emit(NOP());
    result(a, 24, 11);
    a.emit(ADDI(24, 0, -1));
    a.emit(ECALL());
    result(a, 24, 0);
    a.emit(LW(4, 0, SCRATCH + 20)); result(a, 4, 20);
    a.li(1, HANDLER);
    a.emit(CSRRW(0, MTVEC, 1));
    a.emit(CSRRCI(0, MSTATUS, 0x8));
    a.li(1, 2);
    a.emit(SW(1, 18, 4));                                        // timer pending but masked by MIE
    a.emit(NOP()); a.emit(NOP()); a.emit(NOP());
    a.emit(LW(4, 0, SCRATCH + 20)); result(a, 4, 20);
    a.emit(CSRRS(0, MIE, 0));                                    // rs1=x0 form never writes
    a.li(1, 0x80);
    a.emit(CSRRC(0, MIE, 1));                                    // clear MTIE: still masked after MIE=1
    a.emit(CSRRSI(0, MSTATUS, 0x8));
    a.emit(NOP()); a.emit(NOP()); a.emit(NOP());
    a.emit(LW(4, 0, SCRATCH + 20)); result(a, 4, 20);
    a.emit(CSRRS(0, MIE, 1));                                    // MTIE back: taken now
    a.emit(NOP()); a.emit(NOP()); a.emit(NOP());
    a.emit(LW(4, 0, SCRATCH + 20)); result(a, 4, 21);
    a.emit(LW(4, 0, SCRATCH + 0)); result(a, 4, 0x80000007u);
    a.emit(SW(0, 18, 0));                                        // done
    a.emit(JAL(0, 0));
    return a.finish();
}

void run_directed(int waits) {
    Harness h(1, waits);
    h.load({JAL(0, int32_t(PROGRAM))}, 0);
    h.load(handler_program(), HANDLER);
    h.load(vector_table(), VECTORS);
    std::vector<uint32_t> program = directed_program();
    require(PROGRAM + 4 * program.size() < DATA, "directed program overlaps data");
    h.load(program, PROGRAM);
    h.reset();
    h.run(400000);
    for (const Expectation &e : expected) {
        uint32_t got = h.word(RESULTS + 4 * e.index);
        require(got == e.value, "directed result " + std::to_string(e.index) + " (cpu_tb.cpp:" +
                std::to_string(e.line) + ") = " + hex(got) + " expected " + hex(e.value));
    }
    std::cout << "directed (max wait " << waits << "): " << h.steps << " instructions, " << h.traps
              << " exceptions, " << h.interrupts << " interrupts, " << h.cycles << " cycles\n";
}

// ---------------------------------------------------------------------------
// Random instruction streams. Reserved: x27 MMIO base, x28/x26 handler
// temporaries, x29 loop counter, x30 jump temporary, x31 data base.
constexpr uint32_t FUZZ_HANDLER = 0x100, FUZZ_PROGRAM = 0x200, FUZZ_DATA = 0x8000, FUZZ_DATA_BYTES = 0x1000;

std::vector<uint32_t> fuzz_handler() {
    Asm a(FUZZ_HANDLER);
    a.emit(CSRRW(28, MSCRATCH, 28));
    a.emit(CSRRS(28, MCAUSE, 0));
    int irq = a.label(), done = a.label();
    a.branch(4, 28, 0, irq);
    a.emit(CSRRS(28, MTVAL, 0));
    a.emit(SW(28, 0, 0x10));                 // last mtval, compared through the model
    a.emit(CSRRS(28, MEPC, 0));
    a.emit(ADDI(28, 28, 4));
    a.emit(CSRRW(0, MEPC, 28));
    a.jal(0, done);
    a.place(irq);
    a.emit(SW(26, 0, 0x14));
    a.emit(LUI(26, 0xf0000));
    a.emit(ADDI(28, 0, 7));
    a.emit(SW(28, 26, 8));                   // clear every line
    a.emit(LW(26, 0, 0x14));
    a.place(done);
    a.emit(CSRRW(28, MSCRATCH, 28));
    a.emit(MRET());
    return a.finish();
}

bool reserved(uint32_t r) { return r >= 26 && r <= 31; }

std::vector<uint32_t> fuzz_program(Rng &rng, uint32_t items, uint32_t loops) {
    struct Item { std::vector<uint32_t> words; int skip = -1; bool jal = false; bool jalr = false; };
    std::vector<Item> body;
    auto rd = [&]() { uint32_t r; do { r = rng.below(32); } while (reserved(r)); return r; };
    auto rs = [&]() { return rng.below(32); };
    for (uint32_t i = 0; i < items; ++i) {
        Item it;
        uint32_t k = rng.below(100);
        if (k < 30) {                                   // OP-IMM
            uint32_t f3 = rng.below(8);
            if (f3 == 1) it.words.push_back(SLLI(rd(), rs(), rng.below(32)));
            else if (f3 == 5) it.words.push_back(rng.below(2) ? SRLI(rd(), rs(), rng.below(32)) : SRAI(rd(), rs(), rng.below(32)));
            else it.words.push_back(I(int32_t(rng.next()) >> 20, rs(), f3, rd(), 0x13));
        } else if (k < 50) {                            // OP
            uint32_t f3 = rng.below(8);
            uint32_t f7 = (f3 == 0 || f3 == 5) && rng.below(2) ? 0x20 : 0;
            it.words.push_back(R(f7, rs(), rs(), f3, rd(), 0x33));
        } else if (k < 56) {
            it.words.push_back(rng.below(2) ? LUI(rd(), rng.next() & 0xfffff) : AUIPC(rd(), rng.next() & 0xfffff));
        } else if (k < 70) {                            // loads and stores to the data window
            uint32_t f3 = rng.below(5); if (f3 == 3) f3 = 0;
            uint32_t width = f3 == 0 || f3 == 4 ? 1 : f3 == 1 ? 2 : 4;
            int32_t off = int32_t(rng.below(FUZZ_DATA_BYTES - 4));
            if (rng.below(10)) off &= ~int32_t(width - 1);
            if (rng.below(2)) it.words.push_back(I(off, 31, f3, rd(), 0x03));
            else it.words.push_back(S(off, rs(), 31, f3 & 3 ? f3 : (width == 1 ? 0 : 2), 0x23));
        } else if (k < 78) {                            // forward branch
            static const uint32_t conditions[] = {0, 1, 4, 5, 6, 7};
            it.words.push_back(B(0, rs(), rs(), conditions[rng.below(6)]));
            it.skip = int(rng.below(8));
        } else if (k < 82) {                            // forward jal
            it.words.push_back(J(0, rng.below(3) ? 0 : rd()));
            it.skip = int(rng.below(8)); it.jal = true;
        } else if (k < 85) {                            // jalr through x30, sometimes misaligned
            it.words.push_back(AUIPC(30, 0));
            it.words.push_back(JALR(rng.below(2) ? 0 : rd(), 30, 0));
            it.skip = int(rng.below(6)); it.jalr = true;
        } else if (k < 91) {                            // CSRs
            static const uint32_t csrs[] = {MSCRATCH, MTVAL, MCAUSE, MEPC, MIE, MSTATUS, MIP, MCYCLE, MINSTRET,
                                            CYCLE, TIME, INSTRET, MCYCLEH, MISA, MHARTID, 0x7c0, 0x800};
            uint32_t csr = rng.below(3) == 0 ? MSTATUS : csrs[rng.below(sizeof csrs / sizeof csrs[0])];
            uint32_t f3 = 1 + rng.below(3) + (rng.below(2) ? 4 : 0);
            uint32_t src = f3 & 4 ? rng.below(32) : rs();
            if (csr == MSTATUS || csr == MIE) {
                // keep MIE/MPIE/MEIE/MTIE/MSIE patterns; uimm forms reach bits 3 and 7 only through x registers
                f3 = 1 + rng.below(3) + 4; src = rng.below(2) ? 0x8 : 0x0;
                if (csr == MIE && rng.below(2)) { f3 -= 4; src = rs(); }
            }
            it.words.push_back(I(int32_t(csr), src, f3, rd(), 0x73));
        } else if (k < 95) {
            static const uint32_t misc[] = {ECALL(), EBREAK(), 0xffffffffu, 0x00000000u, FENCE(), FENCE_I(), WFI(),
                                            0x00200073u, 0x10200073u, 0x0000400fu, 0x00004073u};
            it.words.push_back(misc[rng.below(sizeof misc / sizeof misc[0])]);
        } else {                                        // raise interrupt lines from a register
            it.words.push_back(SW(rs(), 27, 4));
        }
        body.push_back(it);
    }
    Asm a(FUZZ_PROGRAM);
    a.li(31, FUZZ_DATA);
    a.li(27, TestSystem::MMIO_BASE);
    a.li(29, loops);
    a.li(30, FUZZ_HANDLER);
    a.emit(CSRRW(0, MTVEC, 30));
    a.li(1, 0x888);
    a.emit(CSRRW(0, MIE, 1));
    int start = a.label(), end = a.label();
    a.place(start);
    std::vector<uint32_t> starts(body.size() + 1);
    uint32_t addr = a.here();
    for (size_t i = 0; i < body.size(); ++i) { starts[i] = addr; addr += 4 * uint32_t(body[i].words.size()); }
    starts[body.size()] = addr;
    for (size_t i = 0; i < body.size(); ++i) {
        Item &it = body[i];
        if (it.skip >= 0) {
            size_t target_index = std::min(body.size(), i + 1 + size_t(it.skip));
            int32_t target = int32_t(starts[target_index]);
            if (it.jalr) {
                int32_t off = target - int32_t(starts[i]);
                if (rng.below(8) == 0) off += 2;                     // misaligned target
                it.words[1] = JALR((it.words[1] >> 7) & 31, 30, off);
            } else if (it.jal) {
                it.words[0] = J(target - int32_t(starts[i]), (it.words[0] >> 7) & 31);
            } else {
                uint32_t w = it.words[0];
                it.words[0] = B(target - int32_t(starts[i]), (w >> 20) & 31, (w >> 15) & 31, (w >> 12) & 7);
            }
        }
        for (uint32_t w : it.words) a.emit(w);
    }
    a.place(end);
    a.emit(ADDI(29, 29, -1));
    a.branch(1, 29, 0, start);
    a.emit(SW(0, 27, 0));
    a.emit(JAL(0, 0));
    return a.finish();
}

void run_fuzz(uint64_t seed, uint32_t items, uint32_t loops, int waits) {
    Harness h(seed, waits);
    h.load({JAL(0, int32_t(FUZZ_PROGRAM))}, 0);
    h.load(fuzz_handler(), FUZZ_HANDLER);
    std::vector<uint32_t> program = fuzz_program(h.rng, items, loops);
    require(FUZZ_PROGRAM + 4 * program.size() < FUZZ_DATA, "fuzz program overlaps data");
    h.load(program, FUZZ_PROGRAM);
    h.fill_random(FUZZ_DATA, FUZZ_DATA_BYTES);
    h.reset();
    h.run(uint64_t(items) * loops * 40 + 100000);
    std::cout << "fuzz seed " << seed << " (max wait " << waits << "): " << h.steps << " instructions, "
              << h.traps << " exceptions, " << h.interrupts << " interrupts, " << h.unpredictable
              << " adopted counter reads, " << h.cycles << " cycles\n";
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    uint32_t seeds = 6, items = 400, loops = 40;
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg.rfind("--seeds=", 0) == 0) seeds = uint32_t(std::stoul(arg.substr(8)));
        else if (arg.rfind("--items=", 0) == 0) items = uint32_t(std::stoul(arg.substr(8)));
        else if (arg.rfind("--loops=", 0) == 0) loops = uint32_t(std::stoul(arg.substr(8)));
    }
    run_directed(0);
    run_directed(3);
    for (uint32_t seed = 1; seed <= seeds; ++seed)
        run_fuzz(seed, items, loops, seed % 3 == 0 ? 0 : 3);
    std::cout << "fes_rv32_cpu checks passed (host simulation only).\n";
    return 0;
}
