// SPDX-License-Identifier: MIT
// Named-state programs for Route C: current-main consumer semantics.
// Compares public debug_* ports, not anonymous Verilator register matching.
#include "Vfes_z80_engine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>

namespace {

#ifdef Z80_ROUTE_C_NMOS
constexpr bool nmos = true;
#else
constexpr bool nmos = false;
#endif

std::string context;
uint64_t assertions = 0;

std::string hex(unsigned value, int digits = 2) {
    std::ostringstream out;
    out << "0x" << std::hex << value;
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

class Cpu {
 public:
    Vfes_z80_engine dut;
    uint8_t memory[65536]{};
    unsigned instructions = 0;

    Cpu() {
        dut.clk = 0;
        dut.enable = 1;
        dut.int_n = 1;
        dut.nmi_n = 1;
        dut.interrupt_blocked = 0;
        dut.bus_resume = 0;
        dut.bus_ready = 1;
        dut.bus_rdata = 0;
        dut.reset = 1;
        tick();
        tick();
        tick();
        dut.reset = 0;
        dut.eval();
        instructions = 0;
    }

    void tick() {
        dut.clk = 0;
        dut.bus_ready = 1;
        dut.eval();
        const bool transfer = dut.enable && dut.bus_req && !dut.reset;
        const uint8_t kind = dut.bus_kind;
        const uint16_t address = dut.bus_addr;
        const uint8_t data = dut.bus_wdata;
        dut.bus_rdata = memory[address];
        dut.eval();
        dut.clk = 1;
        dut.eval();
        if (transfer && kind == 2) memory[address] = data;
        if (dut.retired) ++instructions;
        dut.clk = 0;
        dut.eval();
    }

    void put(std::initializer_list<uint8_t> bytes, uint16_t address = 0) {
        for (uint8_t byte : bytes) memory[address++] = byte;
    }

    void run() {
        for (unsigned watchdog = 0; !dut.halted && watchdog != 200000; ++watchdog) {
            tick();
            require(!dut.illegal, "unexpected illegal opcode at " + hex(dut.debug_pc, 4));
        }
        require(dut.halted, "HALT timeout at " + hex(dut.debug_pc, 4));
    }
};

void add_a_h() {
    context = "ADD A,H uses architectural H";
    Cpu cpu;
    cpu.put({0x3e, 0x10, 0x21, 0x50, 0x40, 0x84, 0x76});
    cpu.run();
    expect_equal(cpu.dut.debug_af >> 8, 0x50, "A after ADD A,H");
    expect_equal(cpu.dut.debug_hl, 0x4050, "HL named state");
}

void add_a_ixh() {
    if (!nmos) return;
    context = "NMOS DD ADD A,H uses IXH, not H";
    Cpu cpu;
    cpu.put({0x3e, 0x10, 0x21, 0x50, 0x40, 0xdd, 0x21, 0x30, 0x20, 0xdd, 0x84, 0x76});
    cpu.run();
    expect_equal(cpu.dut.debug_af >> 8, 0x30, "A after ADD A,IXH");
    expect_equal(cpu.dut.debug_ix, 0x2030, "IX named state");
    expect_equal(cpu.dut.debug_hl, 0x4050, "HL preserved through IXH add");
}

void ldir_pv_flag() {
    context = "LDIR PV follows remaining BC (main next_bc!=0)";
    Cpu once;
    once.put({0x01, 0x01, 0x00, 0x21, 0x00, 0x90, 0x11, 0x00, 0x91, 0xed, 0xb0, 0x76});
    once.memory[0x9000] = 0x21;
    once.run();
    expect_equal(once.dut.debug_bc, 0, "LDIR BC=1 exhausts");
    expect_equal(once.memory[0x9100], 0x21, "copied byte");
    require((once.dut.debug_af & 0x04) == 0, "PV clear when next BC is 0");

    Cpu more;
    more.put({0x01, 0x02, 0x00, 0x21, 0x00, 0x90, 0x11, 0x00, 0x91, 0xed, 0xb0, 0x76});
    more.memory[0x9000] = 0x21;
    more.memory[0x9001] = 0x38;
    more.run();
    expect_equal(more.dut.debug_bc, 0, "LDIR BC=2 exhausts");
    require((more.dut.debug_af & 0x04) == 0, "PV clear after final LDIR iteration");
}

void inc_h_and_jp_hl() {
    context = "INC H then JP (HL)";
    Cpu cpu;
    cpu.put({0x21, 0xff, 0x10, 0x24, 0xe9});
    cpu.memory[0x11ff] = 0x76;
    cpu.run();
    expect_equal(cpu.dut.debug_hl, 0x11ff, "INC H named state");
    expect_equal(cpu.dut.retire_pc, 0x11ff, "JP (HL) retires at HL");
}

void cpir_match() {
    context = "CPIR stops on match with remaining BC";
    Cpu cpu;
    cpu.put({0x3e, 0x20, 0x01, 0x03, 0x00, 0x21, 0x00, 0x90, 0xed, 0xb1, 0x76});
    cpu.memory[0x9000] = 0x11;
    cpu.memory[0x9001] = 0x20;
    cpu.memory[0x9002] = 0x32;
    cpu.run();
    expect_equal(cpu.dut.debug_bc, 0x0001, "CPIR remaining BC");
    expect_equal(cpu.dut.debug_hl, 0x9002, "CPIR HL after match");
    require((cpu.dut.debug_af & 0x40) != 0, "Z set on match");
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        add_a_h();
        add_a_ixh();
        ldir_pv_flag();
        inc_h_and_jp_hl();
        cpir_match();
    } catch (const std::exception& error) {
        std::cerr << "FAIL " << error.what() << '\n';
        return EXIT_FAILURE;
    }
    std::cout << (nmos ? "NMOS" : "fast") << " route-c named-state programs: "
              << assertions << " assertions\n";
    return EXIT_SUCCESS;
}
