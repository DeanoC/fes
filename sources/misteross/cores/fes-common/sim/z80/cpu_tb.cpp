// SPDX-License-Identifier: GPL-2.0-or-later
// Original instruction-level tests written from public specifications, without
// consulting CPU/emulator implementations. Sources:
// Zilog UM008011-0816: https://www.zilog.com/docs/z80/um0080.pdf
// Sean Young, The Undocumented Z80 Documented, v0.90, sections 3-6:
// https://datasheets.chipdb.org/Zilog/Z80/z80-documented-0.90.pdf
// David Banks' physical Zilog NMOS measurements (Q and repeated blocks):
// https://github.com/hoglet67/Z80Decoder/wiki/Undocumented-Flags
// Original Zilog 1987 technical manual, LD A,I and LD A,R interrupt note:
// https://www.bitsavers.org/components/zilog/z80/1987_Zilog_Z80_Technical_Manual.pdf
#ifdef DOCS_ONLY
#include "Vfes_z80_fast.h"
using Vfes_z80_engine = Vfes_z80_fast;
#else
#include "Vfes_z80_engine.h"
#endif
#include "verilated.h"

#include <array>
#include <cstdint>
#include <cstdlib>
#include <functional>
#include <iomanip>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

namespace {

#ifdef DOCS_ONLY
constexpr bool nmos = false;
#else
constexpr bool nmos = true;
#endif
constexpr uint8_t S = 0x80, Z = 0x40, Y = 0x20, H = 0x10;
constexpr uint8_t X = 0x08, P = 0x04, N = 0x02, C = 0x01;
std::string context;
uint64_t assertions = 0;

std::string hex(unsigned value, int digits = 2) {
    std::ostringstream out;
    out << "0x" << std::hex << std::setfill('0') << std::setw(digits) << value;
    return out.str();
}

void require(bool condition, const std::string& detail) {
    ++assertions;
    if (!condition) throw std::runtime_error(context + ": " + detail);
}

void expect_equal(unsigned actual, unsigned expected, const std::string& detail) {
    require(actual == expected, detail + " got " + hex(actual, 4) +
                                    ", expected " + hex(expected, 4));
}

uint8_t parity(uint8_t value) {
    unsigned ones = 0;
    for (unsigned bit = 0; bit != 8; ++bit) ones += (value >> bit) & 1;
    return (ones % 2 == 0) ? P : 0;
}

uint8_t sign_zero(uint8_t value) {
    return (value & S) | (value == 0 ? Z : 0);
}

int signed8(uint8_t value) { return value < 128 ? value : int(value) - 256; }
int signed16(uint16_t value) { return value < 32768 ? value : int(value) - 65536; }

struct Result { uint8_t a, f; };

// Arithmetic flags are derived from integer ranges and nibble arithmetic,
// independently of the RTL's carry-chain implementation.
Result alu(unsigned operation, uint8_t lhs, uint8_t rhs, uint8_t old_flags) {
    uint8_t value = 0, flags = 0;
    if (operation <= 3 || operation == 7) {
        const bool subtract = operation == 2 || operation == 3 || operation == 7;
        const int carry = (operation == 1 || operation == 3) && (old_flags & C);
        const int full = subtract ? int(lhs) - rhs - carry : int(lhs) + rhs + carry;
        const int low = subtract ? int(lhs & 15) - (rhs & 15) - carry
                                 : int(lhs & 15) + (rhs & 15) + carry;
        const int signed_full = subtract ? signed8(lhs) - signed8(rhs) - carry
                                         : signed8(lhs) + signed8(rhs) + carry;
        value = uint8_t(full);
        flags = sign_zero(value) | (low < 0 || low > 15 ? H : 0) |
                (signed_full < -128 || signed_full > 127 ? P : 0) |
                (subtract ? N : 0) | (full < 0 || full > 255 ? C : 0);
    } else {
        value = operation == 4 ? lhs & rhs : operation == 5 ? lhs ^ rhs : lhs | rhs;
        flags = sign_zero(value) | parity(value) | (operation == 4 ? H : 0);
    }
    flags |= (operation == 7 ? rhs : value) & (X | Y);
    return {operation == 7 ? lhs : value, flags};
}

struct BusEvent { uint8_t kind, data; uint16_t address; };

class Cpu {
 public:
    Vfes_z80_engine dut;
    std::array<uint8_t, 65536> memory{};
    std::array<uint8_t, 65536> ports{};
    std::vector<BusEvent> events;
    uint8_t acknowledge = 0xff;
    unsigned instructions = 0;
    uint64_t clocks = 0;

    Cpu() {
        dut.clk = 0;
        dut.enable = 1;
        dut.int_n = 1;
        dut.nmi_n = 1;
        dut.bus_ready = 1;
        dut.bus_rdata = 0;
        dut.reset = 1;
        tick(); tick(); tick();
        dut.reset = 0;
        dut.eval();
        events.clear();
        instructions = 0;
    }

    void tick(bool ready = true) {
        dut.clk = 0;
        dut.bus_ready = ready;
        dut.eval();
        const bool transfer = dut.enable && dut.bus_req && ready && !dut.reset;
        const uint8_t kind = dut.bus_kind;
        const uint16_t address = dut.bus_addr;
        const uint8_t data = dut.bus_wdata;
        dut.bus_rdata = kind == 3 ? ports[address] : kind == 5 ? acknowledge : memory[address];
        dut.eval();
        dut.clk = 1;
        dut.eval();
        if (transfer) {
            if (kind == 2) memory[address] = data;
            if (kind == 4) ports[address] = data;
            events.push_back({kind, kind == 2 || kind == 4 ? data : uint8_t(dut.bus_rdata), address});
        }
        if (dut.retired) ++instructions;
        ++clocks;
        dut.clk = 0;
        dut.eval();
    }

    void put(std::initializer_list<uint8_t> bytes, uint16_t address = 0) {
        for (uint8_t byte : bytes) memory[address++] = byte;
    }

    void put(const std::vector<uint8_t>& bytes, uint16_t address = 0) {
        for (uint8_t byte : bytes) memory[address++] = byte;
    }

    void step(unsigned count = 1) {
        const unsigned target = instructions + count;
        for (unsigned watchdog = 0; instructions < target && watchdog != 20000; ++watchdog) {
            tick();
            require(!dut.illegal, "unexpected illegal opcode at " + hex(dut.debug_pc, 4));
        }
        require(instructions == target, "instruction retirement timeout at " + hex(dut.debug_pc, 4));
    }

    void run() {
        for (unsigned watchdog = 0; !dut.halted && watchdog != 200000; ++watchdog) {
            tick();
            require(!dut.illegal, "unexpected illegal opcode at " + hex(dut.debug_pc, 4));
        }
        require(dut.halted, "HALT timeout at " + hex(dut.debug_pc, 4));
    }

    void until(const std::function<bool()>& condition) {
        for (unsigned watchdog = 0; !condition() && watchdog != 20000; ++watchdog) tick();
        require(condition(), "event timeout at " + hex(dut.debug_pc, 4));
    }

    uint8_t reg(unsigned index) const {
        switch (index) {
        case 0: return dut.debug_bc >> 8;
        case 1: return dut.debug_bc;
        case 2: return dut.debug_de >> 8;
        case 3: return dut.debug_de;
        case 4: return dut.debug_hl >> 8;
        case 5: return dut.debug_hl;
        case 6: return memory[dut.debug_hl];
        default: return dut.debug_af >> 8;
        }
    }

    void af(uint8_t expected_a, uint8_t expected_f, uint8_t mask = 0xff) const {
        if (!nmos) mask &= 0xd7; // Zilog names bits 3 and 5 unused.
        expect_equal(dut.debug_af >> 8, expected_a, "A");
        expect_equal(dut.debug_af & mask, expected_f & mask, "F mask " + hex(mask));
    }

    unsigned count_kind(unsigned kind) const {
        unsigned count = 0;
        for (const auto& event : events) count += event.kind == kind;
        return count;
    }
};

void initial_af(Cpu& cpu, std::vector<uint8_t>& program, uint8_t a, uint8_t f) {
    program.insert(program.end(), {0x31, 0x00, 0xe0, 0xf1});
    cpu.memory[0xe000] = f;
    cpu.memory[0xe001] = a;
}

void immediate_alu() {
    const std::array<uint8_t, 11> values = {0, 1, 8, 15, 16, 0x28, 0x7f, 0x80, 0x8f, 0xfe, 0xff};
    for (unsigned operation = 0; operation != 8; ++operation)
        for (uint8_t lhs : values) for (uint8_t rhs : values) for (unsigned carry = 0; carry != 2; ++carry) {
            context = "immediate ALU " + std::to_string(operation) + " " + hex(lhs) + "," + hex(rhs) +
                      " carry=" + std::to_string(carry);
            Cpu cpu;
            std::vector<uint8_t> program;
            initial_af(cpu, program, lhs, uint8_t(0xfe | carry));
            program.insert(program.end(), {uint8_t(0xc6 + 8 * operation), rhs, 0x76});
            cpu.put(program);
            cpu.run();
            const Result expected = alu(operation, lhs, rhs, uint8_t(carry));
            cpu.af(expected.a, expected.f);
        }
}

void register_and_memory_alu() {
    for (unsigned operation = 0; operation != 8; ++operation) for (unsigned source = 0; source != 8; ++source) {
        context = "register ALU " + std::to_string(operation) + "/" + std::to_string(source);
        Cpu cpu;
        std::vector<uint8_t> program;
        initial_af(cpu, program, 0x81, C);
        program.insert(program.end(), {0x01, 0x29, 0x29, 0x11, 0x29, 0x29, 0x21, 0x29, 0x29,
                                      uint8_t(0x80 + operation * 8 + source), 0x76});
        cpu.memory[0x2929] = 0x29;
        cpu.put(program); cpu.run();
        const Result expected = alu(operation, 0x81, source == 7 ? 0x81 : 0x29, C);
        cpu.af(expected.a, expected.f);
    }
    for (uint8_t prefix : {0xdd, 0xfd}) for (unsigned operation = 0; operation != 8; ++operation) {
        context = "indexed ALU " + hex(prefix) + "/" + std::to_string(operation);
        Cpu cpu;
        std::vector<uint8_t> program;
        initial_af(cpu, program, 0x7f, C);
        program.insert(program.end(), {prefix, 0x21, 0x02, 0x90, prefix,
                                      uint8_t(0x86 + 8 * operation), 0xfe, 0x76});
        cpu.memory[0x9000] = 0x81;
        cpu.put(program); cpu.run();
        const Result expected = alu(operation, 0x7f, 0x81, C);
        cpu.af(expected.a, expected.f);
    }
}

void load_matrix() {
    for (unsigned destination = 0; destination != 8; ++destination)
        for (unsigned source = 0; source != 8; ++source) {
            if (destination == 6 && source == 6) continue;
            context = "LD register matrix " + std::to_string(destination) + "/" + std::to_string(source);
            Cpu cpu;
            cpu.put({0x01, 0x12, 0x34, 0x11, 0x56, 0x78, 0x21, 0x00, 0x90,
                     0x3e, 0xab, uint8_t(0x40 + destination * 8 + source), 0x76});
            cpu.memory[0x9000] = 0xcd;
            cpu.step(4);
            const uint8_t expected = cpu.reg(source);
            cpu.step();
            // A write uses the address before an H or L destination changes HL.
            expect_equal(destination == 6 ? cpu.memory[0x9000] : cpu.reg(destination), expected, "destination");
        }
    context = "direct byte and word loads";
    Cpu cpu;
    cpu.put({0x01, 0x00, 0x90, 0x11, 0x01, 0x90, 0x3e, 0x5a, 0x02, 0x12,
             0x32, 0x02, 0x90, 0x3e, 0, 0x0a, 0x1a, 0x3a, 0x02, 0x90,
             0x21, 0xcd, 0xab, 0x22, 0x03, 0x90, 0x21, 0, 0, 0x2a, 0x03, 0x90,
             0xed, 0x43, 0x05, 0x90, 0xed, 0x53, 0x07, 0x90,
             0xed, 0x63, 0x09, 0x90, 0x31, 0xef, 0xbe, 0xed, 0x73, 0x0b, 0x90,
             0x01, 0, 0, 0x11, 0, 0, 0x21, 0, 0, 0x31, 0, 0,
             0xed, 0x4b, 0x05, 0x90, 0xed, 0x5b, 0x07, 0x90,
             0xed, 0x6b, 0x09, 0x90, 0xed, 0x7b, 0x0b, 0x90, 0x76});
    cpu.run();
    expect_equal(cpu.dut.debug_af >> 8, 0x5a, "A direct reads");
    expect_equal(cpu.dut.debug_bc, 0x9000, "BC word round trip");
    expect_equal(cpu.dut.debug_de, 0x9001, "DE word round trip");
    expect_equal(cpu.dut.debug_hl, 0xabcd, "HL word round trip");
    expect_equal(cpu.dut.debug_sp, 0xbeef, "SP word round trip");
    for (unsigned offset = 0; offset != 3; ++offset) expect_equal(cpu.memory[0x9000 + offset], 0x5a, "byte write");
}

void increment_decrement() {
    for (unsigned reg = 0; reg != 8; ++reg) for (bool decrement : {false, true})
        for (uint8_t value : {0, 1, 0x0f, 0x10, 0x7f, 0x80, 0xff}) {
            context = "INC/DEC register " + std::to_string(reg) + "/" + hex(value);
            Cpu cpu;
            std::vector<uint8_t> program;
            initial_af(cpu, program, reg == 7 ? value : 0x35, C);
            if (reg == 6) {
                program.insert(program.end(), {0x21, 0x00, 0x90}); cpu.memory[0x9000] = value;
            } else if (reg != 7) program.insert(program.end(), {uint8_t(0x06 + 8 * reg), value});
            program.insert(program.end(), {uint8_t((decrement ? 0x05 : 0x04) + 8 * reg), 0x76});
            cpu.put(program); cpu.run();
            const uint8_t result = uint8_t(value + (decrement ? -1 : 1));
            uint8_t flags = sign_zero(result) | (result & (X | Y)) | C;
            if (decrement) flags |= N | ((value & 15) == 0 ? H : 0) | (value == 0x80 ? P : 0);
            else flags |= ((value & 15) == 15 ? H : 0) | (value == 0x7f ? P : 0);
            expect_equal(cpu.reg(reg), result, "incremented value");
            cpu.af(reg == 7 ? result : 0x35, flags);
        }
    for (uint8_t prefix : {0xdd, 0xfd}) {
        context = "indexed INC/DEC " + hex(prefix);
        Cpu cpu;
        cpu.put({prefix, 0x21, 0x00, 0x90, prefix, 0x36, 0x80, 0x7f,
                 prefix, 0x34, 0x80, prefix, 0x35, 0x80, 0x76});
        cpu.run(); expect_equal(cpu.memory[0x8f80], 0x7f, "signed -128 displacement");
    }
    context = "16-bit INC/DEC preserve all flags";
    Cpu cpu;
    std::vector<uint8_t> program;
    initial_af(cpu, program, 0x5a, 0xff);
    program.insert(program.end(), {0x01, 0xff, 0xff, 0x03, 0x0b, 0x11, 0, 0, 0x1b, 0x13,
                                  0x21, 0xff, 0xff, 0x23, 0x2b, 0x31, 0, 0, 0x3b, 0x33, 0x76});
    cpu.put(program); cpu.run();
    expect_equal(cpu.dut.debug_bc, 0xffff, "BC"); expect_equal(cpu.dut.debug_de, 0, "DE");
    expect_equal(cpu.dut.debug_hl, 0xffff, "HL"); expect_equal(cpu.dut.debug_sp, 0, "SP"); cpu.af(0x5a, 0xff);
}

void arithmetic16() {
    for (unsigned operation = 0; operation != 3; ++operation)
        for (unsigned source : {0u, 1u, 2u, 3u})
            for (uint16_t lhs : {0u, 0x0fffu, 0x7fffu, 0x8000u, 0xffffu})
                for (uint16_t rhs : {0u, 1u, 0x8000u, 0xffffu}) for (unsigned carry = 0; carry != 2; ++carry) {
                    if (source == 2 && rhs != 0) continue;
                    const uint16_t operand = source == 2 ? lhs : rhs;
                    context = "16-bit ALU " + std::to_string(operation) + "/" + std::to_string(source) +
                              " " + hex(lhs, 4) + "," + hex(rhs, 4);
                    Cpu cpu;
                    std::vector<uint8_t> program;
                    initial_af(cpu, program, 0x29, uint8_t(S | Z | P | carry));
                    program.insert(program.end(), {0x21, uint8_t(lhs), uint8_t(lhs >> 8)});
                    if (source != 2) program.insert(program.end(), {
                        uint8_t(0x01 + source * 16), uint8_t(rhs), uint8_t(rhs >> 8)});
                    if (operation == 0) program.push_back(uint8_t(0x09 + source * 16));
                    else program.insert(program.end(), {0xed, uint8_t((operation == 1 ? 0x4a : 0x42) + source * 16)});
                    program.push_back(0x76); cpu.put(program); cpu.run();
                    const bool subtract = operation == 2;
                    const int ci = operation == 0 ? 0 : carry;
                    const int full = subtract ? int(lhs) - operand - ci : int(lhs) + operand + ci;
                    const int low = subtract ? int(lhs & 0xfff) - (operand & 0xfff) - ci
                                             : int(lhs & 0xfff) + (operand & 0xfff) + ci;
                    const int signed_full = subtract ? signed16(lhs) - signed16(operand) - ci
                                                     : signed16(lhs) + signed16(operand) + ci;
                    const uint16_t result = uint16_t(full);
                    uint8_t flags = uint8_t(result >> 8) & (X | Y);
                    flags |= (full < 0 || full > 65535 ? C : 0) | (low < 0 || low > 4095 ? H : 0);
                    if (operation == 0) flags |= S | Z | P;
                    else flags |= (result & 0x8000 ? S : 0) | (result == 0 ? Z : 0) |
                                  (signed_full < -32768 || signed_full > 32767 ? P : 0) | (subtract ? N : 0);
                    expect_equal(cpu.dut.debug_hl, result, "HL result"); cpu.af(0x29, flags);
                }
    context = "ADD HL,HL";
    Cpu cpu; cpu.put({0x21, 0x00, 0xc0, 0x29, 0x76}); cpu.run();
    expect_equal(cpu.dut.debug_hl, 0x8000, "self addition");
    for (uint8_t prefix : {0xdd, 0xfd}) {
        context = "indexed word arithmetic " + hex(prefix);
        Cpu index;
        index.put({prefix, 0x21, 0xff, 0x7f, 0x01, 1, 0, prefix, 0x09,
                   prefix, 0x23, prefix, 0x2b, prefix, 0x29, 0x76}); index.run();
        expect_equal(prefix == 0xdd ? index.dut.debug_ix : index.dut.debug_iy, 0, "index result");
    }
}

void exchange_and_stack() {
    context = "alternate registers, exchanges and stack";
    Cpu cpu;
    cpu.put({0x31, 0x00, 0xf0, 0x01, 0x34, 0x12, 0x11, 0x78, 0x56, 0x21, 0xbc, 0x9a,
             0xd9, 0x01, 0x11, 0x11, 0x11, 0x22, 0x22, 0x21, 0x33, 0x33, 0xd9,
             0xc5, 0xd5, 0xe5, 0xc1, 0xd1, 0xe1, 0xeb,
             0x3e, 0x29, 0xaf, 0x3e, 0x29, 0x08, 0x3e, 0x5a, 0x37, 0x08, 0x76});
    cpu.run(); expect_equal(cpu.dut.debug_bc, 0x9abc, "PUSH/POP BC");
    expect_equal(cpu.dut.debug_de, 0x1234, "EX DE,HL DE"); expect_equal(cpu.dut.debug_hl, 0x5678, "EX DE,HL HL");
    expect_equal(cpu.dut.debug_sp, 0xf000, "balanced SP"); cpu.af(0x29, Z | P);
    context = "EX (SP),HL and IX/IY word loads/stacks";
    Cpu index;
    index.put({0x31, 0x00, 0xf0, 0x21, 0x34, 0x12, 0xe3,
               0xdd, 0x21, 0x78, 0x56, 0xdd, 0xe3, 0xdd, 0xe5, 0xfd, 0xe1,
               0xfd, 0x22, 0x00, 0x90, 0xfd, 0x21, 0, 0, 0xfd, 0x2a, 0x00, 0x90,
               0xdd, 0xf9, 0x76});
    index.memory[0xf000] = 0xcd; index.memory[0xf001] = 0xab;
    index.run(); expect_equal(index.dut.debug_hl, 0xabcd, "EX (SP),HL result");
    expect_equal(index.dut.debug_ix, 0x1234, "EX (SP),IX result"); expect_equal(index.dut.debug_iy, 0x1234, "IY stack/word result");
    expect_equal(index.memory[0xf000], 0x78, "EX stack low"); expect_equal(index.memory[0xf001], 0x56, "EX stack high");
    expect_equal(index.dut.debug_sp, 0x1234, "LD SP,IX");
    context = "PUSH AF stores both bytes and POP AF restores them";
    Cpu af;
    std::vector<uint8_t> program; initial_af(af, program, 0x5a, 0xf7);
    program.insert(program.end(), {0xf5, 0xc1, 0xc5, 0xaf, 0xf1, 0x76}); af.put(program); af.run();
    expect_equal(af.dut.debug_bc, 0x5af7, "AF stack representation");
    expect_equal(af.dut.debug_sp, 0xe002, "AF stack balanced"); af.af(0x5a, 0xf7);
    context = "PUSH/POP wraps stack at zero";
    Cpu stack;
    stack.put({0x01, 0x34, 0x12, 0x31, 1, 0, 0xc5, 0xd1, 0x76}); stack.run();
    expect_equal(stack.dut.debug_de, 0x1234, "wrapped POP");
    expect_equal(stack.memory[0xffff], 0x34, "wrapped PUSH low");
    expect_equal(stack.memory[0], 0x12, "wrapped PUSH high"); expect_equal(stack.dut.debug_sp, 1, "wrapped SP");
}

void word_address_wrap() {
    for (uint8_t prefix : {0, 0xdd, 0xfd, 0xed}) {
        context = "word memory transfer wraps address " + hex(prefix);
        Cpu cpu;
        std::vector<uint8_t> program;
        if (prefix == 0xed) program.insert(program.end(), {0x01, 0x34, 0x12, 0xed, 0x43, 0xff, 0xff,
                                                         0x01, 0, 0, 0xed, 0x4b, 0xff, 0xff});
        else {
            if (prefix) program.push_back(prefix);
            program.insert(program.end(), {0x21, 0x34, 0x12});
            if (prefix) program.push_back(prefix);
            program.insert(program.end(), {0x22, 0xff, 0xff});
            if (prefix) program.push_back(prefix);
            program.insert(program.end(), {0x21, 0, 0});
            if (prefix) program.push_back(prefix);
            program.insert(program.end(), {0x2a, 0xff, 0xff});
        }
        program.push_back(0x76); cpu.put(program); cpu.run();
        expect_equal(prefix == 0xed ? cpu.dut.debug_bc : prefix == 0xdd ? cpu.dut.debug_ix :
                     prefix == 0xfd ? cpu.dut.debug_iy : cpu.dut.debug_hl, 0x1234, "wrapped word read");
        expect_equal(cpu.memory[0xffff], 0x34, "wrapped store low"); expect_equal(cpu.memory[0], 0x12, "wrapped store high");
    }
}

void index_exceptions() {
    for (uint8_t prefix : {0xdd, 0xfd}) {
        context = "indexed real H/L exception " + hex(prefix);
        Cpu cpu;
        cpu.put({0x21, 0x34, 0x12, prefix, 0x21, 0x01, 0x90, prefix, 0x74, 0xff,
                 prefix, 0x75, 1, prefix, 0x66, 2, prefix, 0x6e, 3, 0x76});
        cpu.memory[0x9003] = 0xab; cpu.memory[0x9004] = 0xcd;
        cpu.run(); expect_equal(cpu.memory[0x9000], 0x12, "stored H"); expect_equal(cpu.memory[0x9002], 0x34, "stored L");
        expect_equal(cpu.dut.debug_hl, 0xabcd, "loaded real HL");
        expect_equal(prefix == 0xdd ? cpu.dut.debug_ix : cpu.dut.debug_iy, 0x9001, "index preserved");
    }
    context = "signed indexed displacement wraps address space";
    Cpu wrap;
    wrap.put({0xdd, 0x21, 0x01, 0, 0xdd, 0x36, 0xfe, 0xab,
              0xfd, 0x21, 0xff, 0xff, 0xfd, 0x36, 2, 0xcd, 0x76});
    wrap.run(); expect_equal(wrap.memory[0xffff], 0xab, "negative wrapping displacement");
    expect_equal(wrap.memory[1], 0xcd, "positive wrapping displacement");
    if (!nmos) return;
    context = "last prefix wins, ignored prefixes and ED exceptions";
    Cpu cpu;
    cpu.put({0xdd, 0xfd, 0x21, 0x34, 0x12, 0xfd, 0xdd, 0x21, 0x78, 0x56,
             0x21, 0xbc, 0x9a, 0x11, 0xf0, 0xde, 0xdd, 0xeb,
             0x01, 1, 0, 0xdd, 0xed, 0x4a, 0xfd, 0xd9, 0xfd, 0xd9,
             0xdd, 0x3e, 0x29, 0xdd, 0x26, 0xab, 0xdd, 0x2e, 0xcd, 0x76});
    cpu.run(); expect_equal(cpu.dut.debug_iy, 0x1234, "last FD"); expect_equal(cpu.dut.debug_ix, 0xabcd, "index halves");
    expect_equal(cpu.dut.debug_hl, 0xdef1, "ED ADC uses HL"); expect_equal(cpu.dut.debug_de, 0x9abc, "ignored EX prefix");
}

Result rotate(unsigned operation, uint8_t value, uint8_t old_flags) {
    uint8_t result = 0, carry = 0;
    switch (operation) {
    case 0: carry = value >> 7; result = uint8_t((value << 1) | carry); break;
    case 1: carry = value & 1; result = uint8_t((value >> 1) | (carry << 7)); break;
    case 2: carry = value >> 7; result = uint8_t((value << 1) | (old_flags & C)); break;
    case 3: carry = value & 1; result = uint8_t((value >> 1) | ((old_flags & C) << 7)); break;
    case 4: carry = value >> 7; result = uint8_t(value << 1); break;
    case 5: carry = value & 1; result = uint8_t((value >> 1) | (value & 0x80)); break;
    case 6: carry = value >> 7; result = uint8_t((value << 1) | 1); break;
    default: carry = value & 1; result = uint8_t(value >> 1); break;
    }
    return {result, uint8_t(sign_zero(result) | (result & (X | Y)) | parity(result) | carry)};
}

void cb_families() {
    for (unsigned operation = 0; operation != 8; ++operation) {
        if (!nmos && operation == 6) continue;
        for (unsigned reg = 0; reg != 8; ++reg) for (uint8_t value : {0, 1, 0x81, 0xff}) {
            context = "CB rotate " + std::to_string(operation) + "/" + std::to_string(reg) + "/" + hex(value);
            Cpu cpu;
            std::vector<uint8_t> program;
            initial_af(cpu, program, reg == 7 ? value : 0x35, C);
            if (reg == 6) { program.insert(program.end(), {0x21, 0, 0x90}); cpu.memory[0x9000] = value; }
            else if (reg != 7) program.insert(program.end(), {uint8_t(0x06 + 8 * reg), value});
            program.insert(program.end(), {0xcb, uint8_t(operation * 8 + reg), 0x76});
            cpu.put(program); cpu.run();
            const Result expected = rotate(operation, value, C);
            expect_equal(cpu.reg(reg), expected.a, "rotated value"); cpu.af(reg == 7 ? expected.a : 0x35, expected.f);
        }
    }
    for (unsigned operation = 1; operation != 4; ++operation)
        for (unsigned bit = 0; bit != 8; ++bit) for (unsigned reg = 0; reg != 8; ++reg)
            for (uint8_t value : {0x28, 0xff}) {
                context = "CB BIT/RES/SET " + std::to_string(operation) + "/" + std::to_string(bit) + "/" + std::to_string(reg);
                Cpu cpu;
                std::vector<uint8_t> program;
                initial_af(cpu, program, reg == 7 ? value : 0x35, 0xff);
                if (reg == 6) { program.insert(program.end(), {0x21, 0, 0x90}); cpu.memory[0x9000] = value; }
                else if (reg != 7) program.insert(program.end(), {uint8_t(0x06 + 8 * reg), value});
                program.insert(program.end(), {0xcb, uint8_t(operation * 64 + bit * 8 + reg), 0x76});
                cpu.put(program); cpu.run();
                uint8_t result = value, flags = 0xff;
                if (operation == 1) {
                    flags = C | H | ((value & (1u << bit)) == 0 ? Z | P : 0) |
                            (bit == 7 && (value & S) ? S : 0) | (value & (X | Y));
                } else if (operation == 2) result &= uint8_t(~(1u << bit));
                else result |= uint8_t(1u << bit);
                expect_equal(cpu.reg(reg), result, "bit-family value");
                // (HL) XY comes from the hidden WZ latch, covered separately.
                const uint8_t mask = operation == 1 ? (!nmos ? uint8_t(Z | H | N | C) :
                                                      reg == 6 ? 0xd7 : 0xff) : 0xff;
                cpu.af(reg == 7 ? result : 0x35, flags, mask);
            }
    for (uint8_t prefix : {0xdd, 0xfd}) for (unsigned operation = 0; operation != 4; ++operation) {
        context = "indexed CB " + hex(prefix) + "/" + std::to_string(operation);
        Cpu cpu;
        std::vector<uint8_t> program;
        initial_af(cpu, program, 0x35, C);
        program.insert(program.end(), {prefix, 0x21, 1, 0xa8, prefix, 0xcb, 0xff,
                                      uint8_t(operation == 0 ? 0x06 : operation * 64 + 7 * 8 + 6), 0x76});
        cpu.memory[0xa800] = 0x81; cpu.put(program); cpu.run();
        if (operation == 0) { expect_equal(cpu.memory[0xa800], 3, "indexed RLC"); cpu.af(0x35, parity(3) | C); }
        else if (operation == 1) {
            expect_equal(cpu.memory[0xa800], 0x81, "indexed BIT preserves byte");
            cpu.af(0x35, S | Y | H | X | C, nmos ? 0xff : uint8_t(Z | H | N | C));
        }
        else { expect_equal(cpu.memory[0xa800], operation == 2 ? 1 : 0x81, "indexed RES/SET"); cpu.af(0x35, C); }
    }
    if (nmos) {
        context = "indexed CB register copy uses real H";
        Cpu cpu;
        cpu.put({0xdd, 0x21, 0, 0x90, 0x21, 0x34, 0x12, 0xdd, 0xcb, 1, 0x04,
                 0xfd, 0x21, 0, 0x91, 0xfd, 0xcb, 0xff, 0xc7, 0x76});
        cpu.memory[0x9001] = 0x81; cpu.memory[0x90ff] = 0x28; cpu.run();
        expect_equal(cpu.dut.debug_hl, 0x0334, "DDCB writes H"); expect_equal(cpu.memory[0x9001], 3, "DDCB memory");
        expect_equal(cpu.dut.debug_ix, 0x9000, "IX unchanged"); expect_equal(cpu.dut.debug_af >> 8, 0x29, "FDCB writes A");
    }
}

void accumulator_control() {
    for (unsigned operation = 0; operation != 4; ++operation) for (uint8_t value : {0, 1, 0x81, 0xff}) {
        context = "accumulator rotate " + std::to_string(operation) + "/" + hex(value);
        Cpu cpu;
        std::vector<uint8_t> program;
        initial_af(cpu, program, value, 0xff);
        program.insert(program.end(), {uint8_t(0x07 + operation * 8), 0x76});
        cpu.put(program); cpu.run(); const Result expected = rotate(operation, value, C);
        cpu.af(expected.a, uint8_t((expected.f & (X | Y | C)) | S | Z | P));
    }
    for (uint8_t value : {0, 1, 0x28, 0x80, 0xff}) {
        context = "CPL/NEG " + hex(value);
        Cpu cpu;
        std::vector<uint8_t> program;
        initial_af(cpu, program, value, S | Z | P | C);
        program.insert(program.end(), {0x2f, 0x76}); cpu.put(program); cpu.run();
        cpu.af(uint8_t(~value), uint8_t(S | Z | P | C | H | N | (uint8_t(~value) & (X | Y))));
        Cpu neg; program.clear(); initial_af(neg, program, value, 0xff);
        program.insert(program.end(), {0xed, 0x44, 0x76}); neg.put(program); neg.run();
        const Result expected = alu(2, 0, value, 0); neg.af(expected.a, expected.f);
    }
    // Validate DAA by decimal arithmetic after valid packed BCD operands,
    // rather than using a copy of the binary adjustment circuit as the oracle.
    for (unsigned lhs : {0u, 1u, 9u, 15u, 49u, 50u, 99u})
        for (unsigned rhs : {0u, 1u, 9u, 15u, 49u, 50u, 99u})
            for (bool subtract : {false, true}) for (unsigned carry = 0; carry != 2; ++carry) {
                context = "BCD DAA " + std::to_string(lhs) + "/" + std::to_string(rhs) +
                          "/" + std::to_string(subtract) + "/" + std::to_string(carry);
                Cpu cpu;
                std::vector<uint8_t> program;
                const uint8_t packed_lhs = uint8_t((lhs / 10) * 16 + lhs % 10);
                const uint8_t packed_rhs = uint8_t((rhs / 10) * 16 + rhs % 10);
                initial_af(cpu, program, packed_lhs, uint8_t(carry));
                program.insert(program.end(), {uint8_t(subtract ? 0xde : 0xce), packed_rhs, 0x27, 0x76});
                cpu.put(program); cpu.run();
                const int decimal = subtract ? int(lhs) - int(rhs) - int(carry) : int(lhs) + int(rhs) + int(carry);
                const unsigned reduced = unsigned((decimal + 100) % 100);
                const uint8_t packed = uint8_t((reduced / 10) * 16 + reduced % 10);
                cpu.af(packed, uint8_t(sign_zero(packed) | parity(packed) | (packed & (X | Y)) |
                                      (subtract ? N : 0) | (decimal < 0 || decimal > 99 ? C : 0)), 0xef);
            }
    for (uint8_t instruction : {0x37, 0x3f}) {
        context = "SCF/CCF documented flags " + hex(instruction);
        Cpu cpu;
        std::vector<uint8_t> program; initial_af(cpu, program, 0x28, 0xff);
        program.insert(program.end(), {instruction, 0x76}); cpu.put(program); cpu.run();
        cpu.af(0x28, uint8_t(S | Z | P | X | Y | (instruction == 0x37 ? C : H)));
    }
    if (nmos) {
        context = "SCF Q history after POP AF";
        Cpu cpu;
        std::vector<uint8_t> program; initial_af(cpu, program, 0, X | Y);
        program.insert(program.end(), {0x37, 0x76}); cpu.put(program); cpu.run(); cpu.af(0, X | Y | C);
        context = "SCF Q history after flag update";
        Cpu modified;
        program.clear(); initial_af(modified, program, 0, 0xff);
        program.insert(program.end(), {0x06, 0x27, 0x04, 0x37, 0x76}); modified.put(program); modified.run();
        modified.af(0, C);
        context = "SCF Q history lost by index prefix";
        Cpu prefixed;
        program.clear(); initial_af(prefixed, program, 0, 0xff);
        program.insert(program.end(), {0x06, 0x27, 0x04, 0xdd, 0x37, 0x76}); prefixed.put(program); prefixed.run();
        prefixed.af(0, X | Y | C);
    }
}

void nibble_rotates() {
    for (uint8_t instruction : {0x67, 0x6f}) {
        context = "RRD/RLD " + hex(instruction);
        Cpu cpu;
        std::vector<uint8_t> program; initial_af(cpu, program, 0xa3, C);
        program.insert(program.end(), {0x21, 0, 0x90, 0xed, instruction, 0x76});
        cpu.memory[0x9000] = 0x5c; cpu.put(program); cpu.run();
        const uint8_t result = instruction == 0x67 ? 0xac : 0xa5;
        expect_equal(cpu.memory[0x9000], instruction == 0x67 ? 0x35 : 0xc3, "nibble memory");
        cpu.af(result, uint8_t(sign_zero(result) | (result & (X | Y)) | parity(result) | C));
    }
}

void branches() {
    for (unsigned condition = 0; condition != 8; ++condition) for (bool taken : {false, true}) {
        const std::array<uint8_t, 8> true_flags = {0, Z, 0, C, 0, P, 0, S};
        const std::array<uint8_t, 8> false_flags = {Z, 0, C, 0, P, 0, S, 0};
        context = "JP/CALL/RET condition " + std::to_string(condition) + "/" + std::to_string(taken);
        Cpu jp;
        std::vector<uint8_t> program; initial_af(jp, program, 0x29, taken ? true_flags[condition] : false_flags[condition]);
        program.insert(program.end(), {uint8_t(0xc2 + condition * 8), 0x00, 0x01, 0x3e, 0x11, 0x76});
        jp.put(program); jp.put({0x3e, 0x22, 0x76}, 0x100); jp.run();
        expect_equal(jp.dut.debug_af >> 8, taken ? 0x22 : 0x11, "conditional JP");
        Cpu call; program.clear(); initial_af(call, program, 0x29, taken ? true_flags[condition] : false_flags[condition]);
        program.insert(program.end(), {uint8_t(0xc4 + condition * 8), 0, 1, 0x76});
        call.put(program); call.put({0x06, 0x5a, 0xc9}, 0x100); call.run();
        if (taken) expect_equal(call.dut.debug_bc >> 8, 0x5a, "conditional CALL");
        expect_equal(call.dut.debug_sp, 0xe002, "CALL balances stack");
        Cpu ret; program.clear(); initial_af(ret, program, 0x29, taken ? true_flags[condition] : false_flags[condition]);
        program.insert(program.end(), {uint8_t(0xc0 + condition * 8), 0x3e, 0x11, 0x76});
        ret.memory[0xe002] = 0; ret.memory[0xe003] = 1;
        ret.put(program); ret.put({0x3e, 0x22, 0x76}, 0x100); ret.run();
        expect_equal(ret.dut.debug_af >> 8, taken ? 0x22 : 0x11, "conditional RET");
        expect_equal(ret.dut.debug_sp, taken ? 0xe004 : 0xe002, "RET SP");
    }
    for (unsigned condition = 0; condition != 4; ++condition) for (bool taken : {false, true}) {
        context = "JR condition " + std::to_string(condition) + "/" + std::to_string(taken);
        const uint8_t flag = condition < 2 ? Z : C;
        const bool set = taken == bool(condition & 1);
        Cpu cpu;
        std::vector<uint8_t> program; initial_af(cpu, program, 0x29, set ? flag : 0);
        program.insert(program.end(), {uint8_t(0x20 + condition * 8), 3, 0x3e, 0x11, 0x76, 0x3e, 0x22, 0x76});
        cpu.put(program); cpu.run(); expect_equal(cpu.dut.debug_af >> 8, taken ? 0x22 : 0x11, "conditional JR");
    }
    context = "DJNZ backwards loop and unconditional JR";
    Cpu loop; loop.put({0x06, 3, 0x3e, 0, 0x3c, 0x10, 0xfd, 0x18, 2, 0x3e, 0xff, 0x76}); loop.run();
    expect_equal(loop.dut.debug_af >> 8, 3, "loop A"); expect_equal(loop.dut.debug_bc >> 8, 0, "loop B");
    for (unsigned vector = 0; vector != 8; ++vector) {
        context = "RST " + std::to_string(vector);
        Cpu cpu; cpu.put({0xc3, 0, 1});
        cpu.put({0x31, 0, 0xf0, uint8_t(0xc7 + vector * 8), 0x76}, 0x100);
        cpu.step();
        // Install the vector after leaving reset, including vector zero.
        cpu.put({0x06, 0x5a, 0xc9}, uint16_t(vector * 8));
        cpu.run(); expect_equal(cpu.dut.debug_bc >> 8, 0x5a, "RST handler");
        expect_equal(cpu.dut.debug_sp, 0xf000, "RST SP");
    }
    for (uint8_t prefix : {0, 0xdd, 0xfd}) {
        context = "indirect JP/LD SP " + hex(prefix);
        Cpu cpu;
        std::vector<uint8_t> program;
        if (prefix) program.push_back(prefix);
        program.insert(program.end(), {0x21, 0, 1});
        if (prefix) program.push_back(prefix);
        program.push_back(0xf9);
        if (prefix) program.push_back(prefix);
        program.push_back(0xe9); cpu.put(program); cpu.put({0x76}, 0x100); cpu.run();
        expect_equal(cpu.dut.debug_sp, 0x100, "LD SP"); expect_equal(cpu.dut.debug_pc, 0x101, "JP target");
    }
    context = "relative branch wrap at ffff";
    Cpu wrap; wrap.put({0xc3, 0xfe, 0xff}); wrap.put({0x18, 0xfe}, 0xfffe);
    wrap.step(2); expect_equal(wrap.dut.debug_pc, 0xfffe, "JR -2 wraps its displacement fetch");
}

void memory_blocks() {
    for (uint8_t instruction : {0xa0, 0xa8, 0xb0, 0xb8}) {
        context = "LD block " + hex(instruction);
        const bool reverse = instruction & 8, repeat = instruction & 16;
        Cpu cpu;
        std::vector<uint8_t> program; initial_af(cpu, program, 0x11, S | Z | C);
        program.insert(program.end(), {0x01, 3, 0, 0x21, uint8_t(reverse ? 2 : 0), 0x90,
                                      0x11, uint8_t(reverse ? 2 : 0), 0x91, 0xed, instruction, 0x76});
        cpu.memory[0x9000] = 0x21; cpu.memory[0x9001] = 0x38; cpu.memory[0x9002] = 0x47;
        cpu.put(program); cpu.run();
        const unsigned moved = repeat ? 3 : 1;
        expect_equal(cpu.dut.debug_bc, 3 - moved, "block BC");
        expect_equal(cpu.dut.debug_hl, uint16_t((reverse ? 0x9002 : 0x9000) + (reverse ? -int(moved) : int(moved))), "block HL");
        expect_equal(cpu.dut.debug_de, uint16_t((reverse ? 0x9102 : 0x9100) + (reverse ? -int(moved) : int(moved))), "block DE");
        for (unsigned offset = 0; offset != moved; ++offset) {
            const unsigned index = reverse ? 2 - offset : offset;
            expect_equal(cpu.memory[0x9100 + index], cpu.memory[0x9000 + index], "copied byte");
        }
        const uint8_t last = cpu.memory[repeat ? (reverse ? 0x9000 : 0x9002) : (reverse ? 0x9002 : 0x9000)];
        const uint8_t sum = uint8_t(last + 0x11);
        cpu.af(0x11, uint8_t(S | Z | C | (repeat ? 0 : P) | (sum & X) | ((sum & 2) ? Y : 0)));
    }
    for (uint8_t instruction : {0xa1, 0xa9, 0xb1, 0xb9}) {
        context = "CP block " + hex(instruction);
        const bool reverse = instruction & 8, repeat = instruction & 16;
        Cpu cpu;
        std::vector<uint8_t> program; initial_af(cpu, program, 0x20, C);
        program.insert(program.end(), {0x01, 3, 0, 0x21, uint8_t(reverse ? 2 : 0), 0x90, 0xed, instruction, 0x76});
        cpu.memory[0x9000] = reverse ? 0x32 : 0x11; cpu.memory[0x9001] = 0x20;
        cpu.memory[0x9002] = reverse ? 0x11 : 0x32;
        cpu.put(program); cpu.run();
        const unsigned compared = repeat ? 2 : 1;
        expect_equal(cpu.dut.debug_bc, 3 - compared, "search BC");
        expect_equal(cpu.dut.debug_hl, reverse ? 0x9002 - compared : 0x9000 + compared, "search HL");
        const uint8_t result = repeat ? 0 : 0x0f;
        const uint8_t adjusted = repeat ? 0 : 0x0e;
        cpu.af(0x20, uint8_t(sign_zero(result) | (repeat ? 0 : H) | P | N | C |
                            (adjusted & X) | ((adjusted & 2) ? Y : 0)));
    }
    context = "CPIR exhausts counter on no match";
    Cpu missing; missing.put({0x3e, 0x20, 0x01, 2, 0, 0x21, 0, 0x90, 0xed, 0xb1, 0x76});
    missing.memory[0x9000] = 0x11; missing.memory[0x9001] = 0x12; missing.run();
    expect_equal(missing.dut.debug_bc, 0, "no-match BC"); expect_equal(missing.dut.debug_hl, 0x9002, "no-match HL");
    require((missing.dut.debug_af & (Z | P)) == 0, "no-match flags");
}

void io_instructions() {
    context = "immediate IN/OUT use A in upper address, preserve flags";
    Cpu cpu;
    std::vector<uint8_t> program; initial_af(cpu, program, 0x29, 0xff);
    program.insert(program.end(), {0xd3, 0x34, 0xdb, 0x35, 0x76});
    cpu.ports[0x2935] = 0x80; cpu.put(program); cpu.run();
    expect_equal(cpu.ports[0x2934], 0x29, "OUT (n),A"); cpu.af(0x80, 0xff);
    for (unsigned reg : {0u, 1u, 2u, 3u, 4u, 5u, 7u}) {
        context = "IN/OUT (C) register " + std::to_string(reg);
        Cpu in;
        program.clear(); initial_af(in, program, 0x35, C);
        program.insert(program.end(), {0x01, 0x34, 0x12, 0xed, uint8_t(0x40 + reg * 8), 0x76});
        in.ports[0x1234] = 0x80; in.put(program); in.run();
        expect_equal(in.reg(reg), 0x80, "IN value"); in.af(reg == 7 ? 0x80 : 0x35, S | C);
        Cpu out;
        out.put({0x01, 0x34, 0x12, 0x11, 0x78, 0x56, 0x21, 0xbc, 0x9a, 0x3e, 0xde,
                 0xed, uint8_t(0x41 + reg * 8), 0x76});
        const std::array<uint8_t, 8> values = {0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0, 0xde};
        out.run(); expect_equal(out.ports[0x1234], values[reg], "OUT register value");
    }
    for (uint8_t instruction : {0xa2, 0xaa, 0xb2, 0xba, 0xa3, 0xab, 0xb3, 0xbb}) {
        context = "I/O block " + hex(instruction);
        const bool output = instruction & 1, reverse = instruction & 8, repeat = instruction & 16;
        const int delta = reverse ? -1 : 1;
        Cpu block;
        block.put({0x01, 0x40, 3, 0x21, 0x02, 0x90, 0xed, instruction, 0x76});
        for (unsigned b = 1; b != 4; ++b) block.ports[b * 256 + 0x40] = uint8_t(0x80 + b);
        for (unsigned offset = 0; offset != 5; ++offset) block.memory[0x9000 + offset] = uint8_t(0x80 + offset);
        block.run(); const unsigned moved = repeat ? 3 : 1;
        expect_equal(block.dut.debug_bc, (3 - moved) * 256 + 0x40, "I/O block B decrements");
        expect_equal(block.dut.debug_hl, uint16_t(0x9002 + int(moved) * delta), "I/O block HL");
        expect_equal(block.count_kind(output ? 4 : 3), moved, "I/O transaction count");
        const uint8_t b = uint8_t(3 - moved);
        uint8_t last_data = 0;
        unsigned index = 0;
        for (const auto& event : block.events) if (event.kind == (output ? 4 : 3)) {
            // OUTx decrements B before the transfer; INx uses its old value.
            expect_equal(event.address, (output ? 2 - index : 3 - index) * 256 + 0x40, "I/O block port address");
            const uint16_t address = uint16_t(0x9002 + int(index) * delta);
            last_data = output ? uint8_t(0x80 + (address - 0x9000)) : uint8_t(0x83 - index);
            expect_equal(output ? event.data : block.memory[address], last_data, "I/O block data");
            ++index;
        }
        if (nmos) {
            const unsigned sum = last_data + uint8_t(output ? block.dut.debug_hl : 0x40 + delta);
            uint8_t flags = sign_zero(b) | (b & (X | Y)) | (last_data & 0x80 ? N : 0) |
                            (sum > 255 ? H | C : 0) | parity(uint8_t((sum & 7) ^ b));
            expect_equal(block.dut.debug_af & 0xff, flags, "NMOS I/O block flags");
        } else {
            expect_equal(block.dut.debug_af & (Z | N), uint8_t((b == 0 ? Z : 0) | N), "documented I/O block flags");
        }
    }
    if (!nmos) for (uint8_t instruction : {0xa2, 0xaa, 0xb2, 0xba, 0xa3, 0xab, 0xb3, 0xbb}) {
        context = "documented I/O block preserves set C " + hex(instruction);
        Cpu block;
        std::vector<uint8_t> code; initial_af(block, code, 0x5a, C);
        code.insert(code.end(), {0x01, 0x40, 2, 0x21, 2, 0x90, 0xed, instruction, 0x76});
        block.memory[0x9000] = 0x80; block.memory[0x9001] = 0x80; block.memory[0x9002] = 0x80;
        block.ports[0x0240] = 0x80; block.ports[0x0140] = 0x80;
        block.put(code); block.run();
        expect_equal(block.dut.debug_af & C, C, "I/O block leaves C set");
    }
}

void refresh_and_special() {
    context = "I/R transfers, parity is IFF2 and R wraps low seven bits";
    Cpu cpu;
    std::vector<uint8_t> program; initial_af(cpu, program, 0xff, C);
    program.insert(program.end(), {0xed, 0x4f, 0x00, 0xed, 0x5f, 0x76}); cpu.put(program);
    cpu.step(3); expect_equal(cpu.dut.debug_ir & 255, 0xff, "LD R,A overrides its own fetch increments");
    cpu.step(); expect_equal(cpu.dut.debug_ir & 255, 0x80, "R low-seven-bit wrap");
    cpu.step(); cpu.af(0x82, S | C); expect_equal(cpu.dut.debug_ir & 255, 0x82, "LD A,R sees both M1 increments");
    Cpu ir;
    ir.put({0x3e, 0x28, 0xed, 0x47, 0xfb, 0, 0xed, 0x57, 0xf3, 0xed, 0x57, 0x76});
    ir.step(5); ir.af(0x28, X | Y | P); expect_equal(ir.dut.debug_ir >> 8, 0x28, "I register");
    ir.step(2); ir.af(0x28, X | Y); expect_equal(ir.dut.debug_iff >> 1, 0, "DI clears both IFFs");
    context = "DDCB refresh count";
    Cpu indexed;
    indexed.put({0xdd, 0x21, 0, 0x90, 0xdd, 0xcb, 0, 0x46, 0x76});
    indexed.step(); expect_equal(indexed.dut.retire_pc, 0, "indexed LD retirement starts at first prefix");
    indexed.step(); expect_equal(indexed.dut.retire_pc, 4, "DDCB retirement starts at first prefix");
    expect_equal(indexed.dut.debug_ir & 255, 4, "DDCB has two M1 cycles");
    expect_equal(indexed.count_kind(0), 4, "DDCB final operation and displacement are memory reads");
    context = "HALT keeps refreshing without advancing PC";
    Cpu halt; halt.put({0x76}); halt.run();
    const unsigned pc = halt.dut.debug_pc, r = halt.dut.debug_ir & 0x7f;
    for (unsigned clock = 0; clock != 100; ++clock) halt.tick();
    expect_equal(halt.dut.debug_pc, pc, "HALT PC"); require((halt.dut.debug_ir & 0x7f) != r, "HALT refresh increments R");
}

void interrupts() {
    for (unsigned mode = 0; mode != 3; ++mode) {
        context = "EI one-instruction delay, HALT wake, interrupt mode " + std::to_string(mode);
        Cpu cpu;
        cpu.put({0x31, 0, 0xf0, 0x3e, 0x90, 0xed, 0x47,
                 0xed, uint8_t(mode == 0 ? 0x46 : mode == 1 ? 0x56 : 0x5e),
                 0xfb, 0x06, 0x5a, 0x76, 0x76});
        cpu.put({0x3e, 0xa5, 0xed, 0x4d}, 0x38);
        cpu.put({0x3e, 0xa5, 0xed, 0x4d}, 0x200);
        cpu.memory[0x9010] = 0; cpu.memory[0x9011] = 2;
        cpu.acknowledge = mode == 2 ? 0x10 : 0xff;
        cpu.step(4);
        cpu.dut.int_n = 0;
        cpu.step();
        expect_equal(cpu.count_kind(5), 0, "EI does not accept pending INT immediately");
        cpu.step(); expect_equal(cpu.dut.debug_bc >> 8, 0x5a, "instruction after EI executes");
        cpu.until([&] { return cpu.count_kind(5) != 0; }); cpu.dut.int_n = 1;
        cpu.run(); expect_equal(cpu.dut.debug_af >> 8, 0xa5, "interrupt service routine executes");
        expect_equal(cpu.dut.debug_sp, 0xf000, "RETI balances stack"); expect_equal(cpu.dut.debug_iff >> 1, 0, "IRQ clears IFFs");
        expect_equal(cpu.memory[0xeffe], 12, "IRQ pushes PC after delayed instruction");
        expect_equal(cpu.memory[0xefff], 0, "IRQ PC high");
        expect_equal(cpu.dut.debug_ir & 0x7f, (cpu.count_kind(0) + cpu.count_kind(5)) & 0x7f,
                     "IRQ acknowledge increments refresh register");
    }
    context = "HALT accepted IRQ returns past HALT";
    Cpu halt;
    halt.put({0x31, 0, 0xf0, 0xed, 0x56, 0xfb, 0, 0x76, 0x3e, 0x5a, 0x76});
    halt.put({0xed, 0x4d}, 0x38); halt.run(); halt.dut.int_n = 0;
    halt.until([&] { return halt.count_kind(5) != 0; }); halt.dut.int_n = 1;
    halt.until([&] { return halt.dut.debug_af >> 8 == 0x5a; }); halt.run();
    expect_equal(halt.memory[0xeffe], 8, "HALT return address");
    context = "IM0 executes supplied NOP rather than forcing RST";
    Cpu im0; im0.put({0x31, 0, 0xf0, 0xfb, 0, 0x3e, 0x5a, 0x76});
    im0.acknowledge = 0; im0.step(2); im0.dut.int_n = 0; im0.step();
    im0.until([&] { return im0.count_kind(5) != 0; }); im0.dut.int_n = 1; im0.run();
    expect_equal(im0.dut.debug_sp, 0xf000, "IM0 NOP does not push stack"); expect_equal(im0.dut.debug_af >> 8, 0x5a, "IM0 resumes stream");
    context = "IM0 injected CALL obtains operand bytes from interrupted instruction stream";
    Cpu im0_call;
    im0_call.put({0x31, 0, 0xf0, 0xfb, 0, 0, 2, 0x3e, 0x5a, 0x76});
    im0_call.put({0x06, 0xa5, 0xc9}, 0x200);
    im0_call.acknowledge = 0xcd; im0_call.step(); im0_call.dut.int_n = 0;
    im0_call.step(2); im0_call.until([&] { return im0_call.count_kind(5) != 0; });
    im0_call.dut.int_n = 1; im0_call.run();
    expect_equal(im0_call.dut.debug_af >> 8, 0x5a, "injected CALL returns after its two operands");
    expect_equal(im0_call.dut.debug_bc >> 8, 0xa5, "injected CALL target");
    expect_equal(im0_call.dut.debug_sp, 0xf000, "injected CALL stack balances");
    expect_equal(im0_call.memory[0xeffe], 7, "injected CALL return address");
    context = "IM0 injected immediate arithmetic decodes acknowledged opcode";
    Cpu im0_alu;
    std::vector<uint8_t> program; initial_af(im0_alu, program, 0x7f, C);
    program.insert(program.end(), {0xfb, 0, 1, 0x76}); im0_alu.put(program);
    im0_alu.acknowledge = 0xc6; im0_alu.step(2); im0_alu.dut.int_n = 0; im0_alu.step(2);
    im0_alu.until([&] { return im0_alu.count_kind(5) != 0; }); im0_alu.dut.int_n = 1; im0_alu.run();
    const Result added = alu(0, 0x7f, 1, C); im0_alu.af(added.a, added.f);
    expect_equal(im0_alu.dut.debug_sp, 0xe002, "injected ADD does not push stack");
    for (uint8_t operation : {0x04, 0x80}) {
        context = "IM0 injected register arithmetic " + hex(operation);
        Cpu im0_reg;
        program.clear(); initial_af(im0_reg, program, operation == 0x04 ? 0x35 : 0x7f, C);
        program.insert(program.end(), {0x06, uint8_t(operation == 0x04 ? 0x7f : 1), 0xfb, 0, 0x76});
        im0_reg.put(program); im0_reg.acknowledge = operation;
        im0_reg.step(3); im0_reg.dut.int_n = 0; im0_reg.step(2);
        im0_reg.until([&] { return im0_reg.count_kind(5) != 0; }); im0_reg.dut.int_n = 1; im0_reg.run();
        if (operation == 0x04) {
            expect_equal(im0_reg.dut.debug_bc >> 8, 0x80, "injected INC B updates selected register");
            im0_reg.af(0x35, S | H | P | C);
        } else im0_reg.af(added.a, added.f);
        expect_equal(im0_reg.dut.debug_sp, 0xe002, "injected register arithmetic does not push stack");
    }
    context = "NMI edge latching and RETN restore IFF1";
    Cpu nmi;
    nmi.put({0x31, 0, 0xf0, 0xfb, 0, 0x76, 0x3e, 0x5a, 0x76});
    nmi.put({0x06, 0xa5, 0xed, 0x45}, 0x66); nmi.run(); nmi.dut.nmi_n = 0;
    nmi.until([&] { return nmi.count_kind(6) != 0; });
    nmi.until([&] { return nmi.dut.debug_pc == 0x66; }); expect_equal(nmi.dut.debug_iff >> 1, 2, "NMI preserves IFF2 and resets IFF1");
    nmi.until([&] { return nmi.dut.debug_af >> 8 == 0x5a; }); nmi.run();
    expect_equal(nmi.dut.debug_bc >> 8, 0xa5, "NMI handler"); expect_equal(nmi.dut.debug_sp, 0xf000, "RETN stack");
    expect_equal(nmi.dut.debug_iff >> 1, 3, "RETN restored IFF1"); expect_equal(nmi.count_kind(6), 1, "held low NMI is edge-triggered");
    for (unsigned clock = 0; clock != 50; ++clock) nmi.tick();
    expect_equal(nmi.count_kind(6), 1, "no repeated NMI edge");
    context = "nested NMI preserves IFF2 even though IFF1 is already clear";
    Cpu nested;
    nested.put({0x31, 0, 0xf0, 0xfb, 0, 0x76, 0x3e, 0x5a, 0x76});
    nested.put({0, 0, 0xed, 0x45}, 0x66); nested.run(); nested.dut.nmi_n = 0;
    nested.until([&] { return nested.count_kind(6) == 1 && nested.dut.debug_pc == 0x66; });
    expect_equal(nested.dut.debug_iff >> 1, 2, "outer NMI IFFs");
    nested.dut.nmi_n = 1; nested.tick(false);
    nested.dut.nmi_n = 0; nested.tick(false);
    nested.until([&] { return nested.count_kind(6) == 2 && nested.dut.debug_pc == 0x66; });
    expect_equal(nested.dut.debug_iff >> 1, 2, "inner NMI does not overwrite IFF2");
    nested.dut.nmi_n = 1;
    nested.until([&] { return nested.dut.debug_af >> 8 == 0x5a; }); nested.run();
    expect_equal(nested.dut.debug_sp, 0xf000, "nested NMI stacks balance");
    expect_equal(nested.dut.debug_iff >> 1, 3, "nested RETN restores enabled state");
    expect_equal(nested.dut.debug_ir & 0x7f, (nested.count_kind(0) + nested.count_kind(6)) & 0x7f,
                 "both NMI acknowledgements increment refresh register");
    if (!nmos) for (uint8_t operation : {0x57, 0x5f}) {
        // UM0080 pp94-95 explicitly documents the interrupt parity note.
        context = "documented LD A,I/R accepted IRQ clears parity " + hex(operation);
        Cpu interrupted;
        program.clear(); initial_af(interrupted, program, 0x28, C);
        program.insert(program.end(), {0xed, 0x47, 0xed, 0x56, 0xfb, 0, 0xed, operation, 0x76});
        interrupted.put(program); interrupted.put({0x76}, 0x38);
        interrupted.step(6); interrupted.dut.int_n = 0; interrupted.step();
        expect_equal(interrupted.dut.debug_af & P, 0, "documented accepted-IRQ parity clearing");
        expect_equal(interrupted.dut.debug_iff >> 1, 0, "pending IRQ accepted");

        context = "fast LD A,I/R withdrawn INT uses live completion level " + hex(operation);
        Cpu withdrawn;
        withdrawn.put(program); withdrawn.put({0x76}, 0x38);
        withdrawn.memory[0xe000] = C; withdrawn.memory[0xe001] = 0x28;
        withdrawn.step(6); withdrawn.dut.int_n = 0; withdrawn.tick(); // ED prefix only.
        withdrawn.dut.int_n = 1; withdrawn.step();
        expect_equal(withdrawn.dut.debug_af & P, P, "withdrawn INT preserves IFF2 parity");
        expect_equal(withdrawn.dut.debug_iff >> 1, 3, "withdrawn INT preserves enabled IFFs");
        expect_equal(withdrawn.count_kind(5), 0, "withdrawn INT never acknowledges");

        context = "fast LD A,I/R INT after retirement cannot change parity " + hex(operation);
        Cpu late;
        late.put(program); late.put({0x76}, 0x38);
        late.memory[0xe000] = C; late.memory[0xe001] = 0x28;
        late.step(7); late.dut.int_n = 0; late.tick(false);
        expect_equal(late.dut.debug_af & P, P, "stalled successor preserves completed IFF2 transfer");
        expect_equal(late.dut.debug_iff >> 1, 3, "late INT waits for successor completion");
        late.step();
        expect_equal(late.dut.debug_af & P, P, "IRQ after successor does not alter LD A,I/R parity");
        expect_equal(late.dut.debug_iff >> 1, 0, "late INT accepted at successor boundary");
    }
    if (nmos) {
        context = "NMOS IM2 accepts odd vector and wraps vector word";
        Cpu odd;
        odd.put({0x31, 0, 0xf0, 0x3e, 0xff, 0xed, 0x47, 0xed, 0x5e, 0xfb, 0, 0x76});
        odd.memory[0xffff] = 0; odd.memory[0] = 2; // Restore entry before execution, then vector after setup.
        odd.put({0x31, 0, 0xf0}); odd.step(4); odd.memory[0] = 2;
        odd.put({0x3e, 0xa5, 0x76}, 0x200); odd.acknowledge = 0xff; odd.dut.int_n = 0;
        odd.run(); expect_equal(odd.dut.debug_af >> 8, 0xa5, "IM2 odd vector wraps high byte");
    }
}

void stalls() {
    context = "bus ready and enable hold state and transactions";
    Cpu cpu; cpu.put({0x21, 0, 0x90, 0x36, 0x5a, 0x76}); cpu.step();
    cpu.until([&] { return cpu.dut.bus_req && cpu.dut.bus_kind == 2; });
    const unsigned pc = cpu.dut.debug_pc, address = cpu.dut.bus_addr;
    const unsigned prior_writes = cpu.count_kind(2);
    for (unsigned clock = 0; clock != 8; ++clock) {
        cpu.tick(false); expect_equal(cpu.dut.debug_pc, pc, "stalled PC"); expect_equal(cpu.dut.bus_addr, address, "stalled address");
        require(cpu.dut.bus_req && cpu.dut.bus_kind == 2 && cpu.dut.bus_wdata == 0x5a, "stalled request stable");
    }
    expect_equal(cpu.count_kind(2), prior_writes, "no write before acceptance");
    cpu.dut.enable = 0;
    for (unsigned clock = 0; clock != 8; ++clock) cpu.tick();
    expect_equal(cpu.count_kind(2), prior_writes, "no write while disabled"); expect_equal(cpu.dut.debug_pc, pc, "disabled PC");
    cpu.dut.enable = 1; cpu.run(); expect_equal(cpu.memory[0x9000], 0x5a, "accepted write");
    expect_equal(cpu.count_kind(2), prior_writes + 1, "exactly one write");
}

void undocumented_policy() {
    if (nmos) {
        context = "NMOS ED aliases and undefined ED no-op";
        Cpu cpu; cpu.put({0x3e, 1, 0xed, 0x4c, 0xed, 0x00, 0x76}); cpu.run();
        const Result expected = alu(2, 0, 1, 0); cpu.af(expected.a, expected.f);
        context = "NMOS IN discard and OUT zero";
        Cpu io; io.put({0x01, 0x34, 0x12, 0x3e, 0x5a, 0xed, 0x70, 0xed, 0x71, 0x76});
        io.ports[0x1234] = 0x28; io.run(); expect_equal(io.ports[0x1234], 0, "OUT (C),0");
        io.af(0x5a, X | Y | parity(0x28), 0xfe);
    } else {
        for (const std::vector<uint8_t>& opcode : {
                std::vector<uint8_t>{0xcb, 0x30}, {0xed, 0x4c}, {0xed, 0x00},
                {0xed, 0x70}, {0xed, 0x71}, {0xdd, 0x26, 0x5a}, {0xdd, 0xcb, 0, 0x00},
                {0xdd, 0x00}, {0xdd, 0xfd, 0x21, 0, 0}, {0xfd, 0xcb, 0, 0x40}}) {
            context = "docs-only rejects undocumented " + hex(opcode.front());
            Cpu cpu; cpu.put(opcode);
            cpu.until([&] { return bool(cpu.dut.illegal); });
            require(cpu.dut.illegal, "illegal signal");
            require(cpu.dut.halted && !cpu.dut.bus_req, "fault stops issuing bus requests");
            expect_equal(cpu.instructions, 0, "offending instruction does not retire");
            for (unsigned clock = 0; clock != 8; ++clock) cpu.tick();
            require(cpu.dut.illegal && cpu.dut.halted && !cpu.dut.bus_req, "fault remains until reset");
            cpu.dut.reset = 1; cpu.tick(); cpu.dut.reset = 0; cpu.put({0x76}); cpu.run();
            require(!cpu.dut.illegal, "reset recovers fault");
        }
    }
}

void nmos_flag_latches() {
    if (!nmos) return;
    context = "BIT (HL) takes XY from WZ rather than memory or HL";
    Cpu bit;
    std::vector<uint8_t> program; initial_af(bit, program, 0, C);
    program.insert(program.end(), {0x21, 0, 0x90, 0xc3, 0, 0x28});
    bit.put(program); bit.put({0xcb, 0x46, 0x76}, 0x2800); bit.run();
    bit.af(0, Z | Y | H | X | P | C);
    for (uint8_t operation : {0xb0, 0xb1}) {
        context = "repeated memory block XY is instruction PC " + hex(operation);
        Cpu block;
        program.clear(); initial_af(block, program, operation == 0xb0 ? 0 : 0x20, C);
        program.insert(program.end(), {0x01, 2, 0, 0x21, 0, 0x90, 0x11, 0, 0x91, 0xc3, 0, 0x28});
        block.put(program); block.put({0xed, operation, 0x76}, 0x2800);
        block.memory[0x9000] = operation == 0xb0 ? 0 : 0x11;
        block.memory[0x9001] = operation == 0xb0 ? 0 : 0x20;
        block.step(7);
        expect_equal(block.dut.debug_pc, 0x2800, "block repeats at ED prefix");
        expect_equal(block.dut.debug_bc, 1, "one block iteration retires");
        block.af(operation == 0xb0 ? 0 : 0x20, uint8_t(X | Y | P | C | (operation == 0xb1 ? H | N : 0)));
        block.run(); block.af(operation == 0xb0 ? 0 : 0x20, operation == 0xb0 ? C : Z | N | C);
    }
    context = "repeated INIR changes XY and parity before interrupt boundary";
    Cpu input;
    input.put({0x01, 0x40, 3, 0x21, 0, 0x90, 0xc3, 0, 0x28});
    input.put({0xed, 0xb2, 0x76}, 0x2800); input.ports[0x0340] = 0x83;
    input.step(4); expect_equal(input.dut.debug_bc, 0x0240, "one INIR iteration");
    expect_equal(input.dut.debug_af & 0xff, X | Y | N, "INIR repeat parity latch");
    for (uint8_t operation : {0x57, 0x5f}) {
        context = "NMOS LD A,I/R parity clears when maskable interrupt accepted " + hex(operation);
        Cpu interrupted;
        program.clear(); initial_af(interrupted, program, 0x28, C);
        program.insert(program.end(), {0xed, 0x47, 0xed, 0x56, 0xfb, 0, 0xed, operation, 0x76});
        interrupted.put(program); interrupted.put({0x76}, 0x38);
        interrupted.step(6); interrupted.dut.int_n = 0;
        interrupted.step();
        expect_equal(interrupted.dut.debug_af & P, 0, "interrupt-boundary LD A,I/R parity");
        expect_equal(interrupted.dut.debug_iff >> 1, 0, "maskable interrupt accepted");
    }
    context = "NMOS NMI during LD A,I preserves IFF2 parity";
    Cpu nonmaskable;
    program.clear(); initial_af(nonmaskable, program, 0x28, C);
    program.insert(program.end(), {0xed, 0x47, 0xfb, 0, 0xed, 0x57, 0x76});
    nonmaskable.put(program); nonmaskable.put({0x76}, 0x66);
    nonmaskable.step(5); nonmaskable.dut.nmi_n = 0;
    // Latch the edge while the opcode fetch is stalled, then complete it.
    nonmaskable.tick(false); nonmaskable.step();
    expect_equal(nonmaskable.dut.debug_af & P, P, "NMI does not clear LD A,I parity");
    expect_equal(nonmaskable.dut.debug_iff >> 1, 2, "NMI preserves IFF2");
}

} // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    std::vector<std::pair<std::string, std::function<void()>>> tests = {
        {"load", load_matrix}, {"alu", immediate_alu}, {"alu-operands", register_and_memory_alu},
        {"inc-dec", increment_decrement}, {"alu16", arithmetic16}, {"exchange-stack", exchange_and_stack},
        {"word-wrap", word_address_wrap},
        {"index", index_exceptions}, {"cb", cb_families}, {"control", accumulator_control},
        {"nibbles", nibble_rotates}, {"branches", branches}, {"blocks", memory_blocks},
        {"io", io_instructions}, {"refresh", refresh_and_special}, {"interrupts", interrupts},
        {"stalls", stalls}, {"undocumented", undocumented_policy},
    };
    if (nmos) tests.emplace_back("nmos-latches", nmos_flag_latches);
    const std::string requested = argc > 1 ? argv[1] : "";
    unsigned completed = 0;
    try {
        for (const auto& test : tests) if (requested.empty() || requested == test.first) {
            context = test.first;
            test.second();
            std::cout << "PASS " << test.first << '\n';
            ++completed;
        }
        require(completed != 0, "unknown test group " + requested);
    } catch (const std::exception& error) {
        std::cerr << "FAIL " << error.what() << '\n';
        return EXIT_FAILURE;
    }
    std::cout << (nmos ? "NMOS" : "documented") << " Z80: " << completed << " groups, "
              << assertions << " assertions\n";
    return EXIT_SUCCESS;
}
