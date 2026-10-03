// SPDX-License-Identifier: MIT
// Original state/transaction checker. Reads fixture values only, never an
// external CPU implementation. See scripts/test_fes_z80_vectors.py for provenance.
// Verilator --public-flat-rw exposes original engine state for test setup; this
// adds no debug-loading interface to synthesizable RTL.
#include "Vfes_z80_engine.h"
#include "Vfes_z80_engine___024root.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <map>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {
// Order is shared with the Python data-only fixture adapter.
constexpr std::array<const char *, 25> fields = {
    "pc", "sp", "a", "b", "c", "d", "e", "f", "h", "l", "i", "r",
    "ei", "wz", "ix", "iy", "af_", "bc_", "de_", "hl_", "im", "p",
    "q", "iff1", "iff2"
};
using Registers = std::array<unsigned, fields.size()>;
struct Byte { unsigned address, value; };
struct Port { unsigned address, value, write; };
struct Fixture {
    std::string label;
    Registers initial{}, final{};
    std::vector<Byte> initial_ram, final_ram;
    std::vector<Port> ports;
};

unsigned read_number(std::istream &input) {
    unsigned value;
    if (!(input >> value)) throw std::runtime_error("truncated fixture stream");
    return value;
}

bool read_fixture(std::istream &input, Fixture &fixture) {
    if (!(input >> fixture.label)) return false;
    for (unsigned &value : fixture.initial) value = read_number(input);
    for (unsigned &value : fixture.final) value = read_number(input);
    for (auto *ram : {&fixture.initial_ram, &fixture.final_ram}) {
        ram->clear();
        const unsigned count = read_number(input);
        if (count > 65536) throw std::runtime_error("invalid RAM entry count");
        for (unsigned index = 0; index != count; ++index)
            ram->push_back({read_number(input), read_number(input)});
    }
    fixture.ports.clear();
    const unsigned count = read_number(input);
    if (count > 65536) throw std::runtime_error("invalid port entry count");
    for (unsigned index = 0; index != count; ++index)
        fixture.ports.push_back({read_number(input), read_number(input), read_number(input)});
    return true;
}

class Checker {
    Vfes_z80_engine dut;
    std::array<uint8_t, 65536> memory{};
    std::array<bool, 65536> initialized{}, final_addresses{};
    std::vector<std::string> errors;
    unsigned next_port = 0;

    void mismatch(const std::string &what, unsigned actual, unsigned expected) {
        std::ostringstream message;
        message << what << " got 0x" << std::hex << actual << " expected 0x" << expected;
        errors.push_back(message.str());
    }

    void seed(const Registers &r) {
        auto *root = dut.rootp;
#define LOAD(member, index) root->fes_z80_engine__DOT__##member = r[index]
        LOAD(pc, 0); LOAD(sp, 1); LOAD(a_reg, 2); LOAD(b_reg, 3);
        LOAD(c_reg, 4); LOAD(d_reg, 5); LOAD(e_reg, 6); LOAD(f_reg, 7);
        LOAD(h_reg, 8); LOAD(l_reg, 9); LOAD(i_reg, 10); LOAD(r_reg, 11);
        LOAD(ei_delay, 12); LOAD(wz, 13); LOAD(ix, 14); LOAD(iy, 15);
        LOAD(af_alt, 16); LOAD(bc_alt, 17); LOAD(de_alt, 18); LOAD(hl_alt, 19);
        LOAD(im, 20); LOAD(ld_air, 21); LOAD(iff1, 23); LOAD(iff2, 24);
#undef LOAD
        // The corpus encodes Q as the saved F byte; our engine stores whether
        // the preceding instruction changed F. When F=0 these representations
        // have the same observable SCF/CCF effect, despite different booleans.
        root->fes_z80_engine__DOT__q = r[22] != 0;
        dut.eval();
    }

    Registers snapshot() const {
        const auto *root = dut.rootp;
#define GET(member) unsigned(root->fes_z80_engine__DOT__##member)
        return {GET(pc), GET(sp), GET(a_reg), GET(b_reg), GET(c_reg), GET(d_reg),
                GET(e_reg), GET(f_reg), GET(h_reg), GET(l_reg), GET(i_reg), GET(r_reg),
                GET(ei_delay), GET(wz), GET(ix), GET(iy), GET(af_alt), GET(bc_alt),
                GET(de_alt), GET(hl_alt), GET(im), GET(ld_air),
                GET(q) ? GET(f_reg) : 0U, GET(iff1), GET(iff2)};
#undef GET
    }

    void edge() {
        dut.clk = 1; dut.eval();
        dut.clk = 0; dut.eval();
    }

public:
    const std::vector<std::string> &check(const Fixture &fixture) {
        errors.clear(); next_port = 0;
        memory.fill(0); initialized.fill(false); final_addresses.fill(false);
        for (const auto &byte : fixture.initial_ram) {
            memory[byte.address] = byte.value;
            initialized[byte.address] = true;
        }
        for (const auto &byte : fixture.final_ram) final_addresses[byte.address] = true;
        dut.clk = 0; dut.reset = 1; dut.enable = 1;
        dut.int_n = 1; dut.nmi_n = 1; dut.bus_ready = 1; dut.bus_rdata = 0;
        dut.eval(); edge();
        dut.reset = 0; dut.eval(); seed(fixture.initial);
        bool complete = false;
        for (unsigned cycle = 0; cycle != 64; ++cycle) {
            dut.clk = 0; dut.eval();
            if (!dut.bus_req) {
                errors.push_back("engine stopped issuing requests before retirement");
                break;
            }
            const unsigned kind = dut.bus_kind, address = dut.bus_addr, data = dut.bus_wdata;
            unsigned input = 0;
            if (kind == 0 || kind == 1) {
                if (!initialized[address]) {
                    errors.push_back("read of RAM absent from initial fixture at " + std::to_string(address));
                    break;
                }
                input = memory[address];
            } else if (kind == 2) {
                if (!final_addresses[address])
                    errors.push_back("write outside final fixture RAM at " + std::to_string(address));
                memory[address] = data; initialized[address] = true;
            } else if (kind == 3 || kind == 4) {
                if (next_port == fixture.ports.size()) {
                    errors.push_back("unexpected I/O transaction");
                    break;
                }
                const auto &port = fixture.ports[next_port++];
                if (address != port.address) mismatch("I/O address", address, port.address);
                if ((kind == 4) != bool(port.write))
                    errors.push_back("I/O direction differs from fixture");
                if (kind == 4 && data != port.value) mismatch("I/O write byte", data, port.value);
                input = port.value;
            } else if (kind != 7) {
                errors.push_back("unexpected interrupt acknowledge without an asserted interrupt");
                break;
            }
            dut.bus_rdata = input; dut.eval(); edge();
            if (dut.illegal) {
                errors.push_back("NMOS instruction was rejected");
                break;
            }
            if (dut.retired) { complete = true; break; }
        }
        if (!complete) errors.push_back("instruction did not retire within 64 transactions");
        if (complete) {
            const auto actual = snapshot();
            for (unsigned index = 0; index != fields.size(); ++index) {
                if (actual[index] != fixture.final[index])
                    mismatch(fields[index], actual[index], fixture.final[index]);
            }
            for (const auto &byte : fixture.final_ram)
                if (memory[byte.address] != byte.value)
                    mismatch("RAM[" + std::to_string(byte.address) + "]", memory[byte.address], byte.value);
            if (next_port != fixture.ports.size())
                mismatch("I/O transaction count", next_port, fixture.ports.size());
        }
        return errors;
    }
};
} // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 3) {
        std::cerr << "usage: Vfes_z80_engine fixture-stream max-detailed-failures\n";
        return 2;
    }
    try {
        std::ifstream input(argv[1]);
        if (!input) throw std::runtime_error("cannot open fixture stream");
        const unsigned detail_limit = std::stoul(argv[2]);
        Checker checker;
        Fixture fixture;
        unsigned total = 0, failed = 0;
        std::map<std::string, unsigned> failures_by_opcode;
        while (read_fixture(input, fixture)) {
            ++total;
            const auto &errors = checker.check(fixture);
            if (errors.empty()) continue;
            ++failed;
            ++failures_by_opcode[fixture.label.substr(0, fixture.label.find('/'))];
            if (failed <= detail_limit) {
                std::cerr << fixture.label << ": FAIL\n";
                for (const auto &error : errors) std::cerr << "  " << error << '\n';
            }
        }
        if (total == 0) throw std::runtime_error("empty fixture stream");
        std::cout << "NMOS external state vectors: " << total << " checked, "
                  << total - failed << " passed, " << failed << " failed\n";
        for (const auto &[opcode, count] : failures_by_opcode)
            std::cout << "  " << opcode << ": " << count << " failed\n";
        std::cout << "Compared all supplied architectural state, WZ, encoded Q, P, EI, IFF/IM, RAM and I/O.\n"
                  << "Excluded exact bus waveform; no physical hardware acceptance.\n";
        return failed == 0 ? 0 : 1;
    } catch (const std::exception &error) {
        std::cerr << error.what() << '\n';
        return 2;
    }
}
