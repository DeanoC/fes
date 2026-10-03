// SPDX-License-Identifier: MIT
// Original state/transaction checker. Reads fixture values only, never an
// external CPU implementation. See scripts/test_fes_z80_vectors.py for provenance.
// Verilator --public-flat-rw exposes original engine state for test setup; this
// adds no debug-loading interface to synthesizable RTL.
#ifdef PIN_QUALIFICATION
#include "Vfes_z80_nmos.h"
#include "Vfes_z80_nmos___024root.h"
using Model = Vfes_z80_nmos;
#define CPU_MEMBER(member) fes_z80_nmos__DOT__cpu__DOT__##member
#else
#include "Vfes_z80_engine.h"
#include "Vfes_z80_engine___024root.h"
using Model = Vfes_z80_engine;
#define CPU_MEMBER(member) fes_z80_engine__DOT__##member
#endif
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
struct Transaction { unsigned kind, address, value; };
struct Fixture {
    std::string label;
    Registers initial{}, final{};
    std::vector<Byte> initial_ram, final_ram;
    std::vector<Port> ports;
#ifdef PIN_QUALIFICATION
    unsigned t_states = 0;
    std::vector<Transaction> transactions;
    std::vector<unsigned> refreshes;
#endif
};

unsigned read_number(std::istream &input, unsigned maximum = ~0U) {
    unsigned value;
    if (!(input >> value)) throw std::runtime_error("truncated fixture stream");
    if (value > maximum) throw std::runtime_error("fixture integer outside expected range");
    return value;
}

bool read_fixture(std::istream &input, Fixture &fixture) {
    if (!(input >> fixture.label)) return false;
    for (auto *registers : {&fixture.initial, &fixture.final})
        for (unsigned index = 0; index != registers->size(); ++index) {
            const unsigned maximum = index <= 1 || (index >= 13 && index <= 19) ? 65535 :
                index == 12 || index == 21 || index >= 23 ? 1 : index == 20 ? 2 : 255;
            (*registers)[index] = read_number(input, maximum);
        }
    for (auto *ram : {&fixture.initial_ram, &fixture.final_ram}) {
        ram->clear();
        const unsigned count = read_number(input);
        if (count > 65536) throw std::runtime_error("invalid RAM entry count");
        for (unsigned index = 0; index != count; ++index)
            ram->push_back({read_number(input, 65535), read_number(input, 255)});
    }
    fixture.ports.clear();
    const unsigned count = read_number(input);
    if (count > 65536) throw std::runtime_error("invalid port entry count");
    for (unsigned index = 0; index != count; ++index)
        fixture.ports.push_back({read_number(input, 65535), read_number(input, 255), read_number(input, 1)});
#ifdef PIN_QUALIFICATION
    fixture.t_states = read_number(input, 1024);
    if (fixture.t_states == 0) throw std::runtime_error("zero fixture duration");
    fixture.transactions.clear();
    const unsigned transaction_count = read_number(input);
    if (transaction_count > 1024) throw std::runtime_error("invalid transaction count");
    for (unsigned index = 0; index != transaction_count; ++index)
        fixture.transactions.push_back({read_number(input, 3), read_number(input, 65535), read_number(input, 255)});
    fixture.refreshes.clear();
    const unsigned refresh_count = read_number(input);
    if (refresh_count > 256) throw std::runtime_error("invalid refresh count");
    for (unsigned index = 0; index != refresh_count; ++index)
        fixture.refreshes.push_back(read_number(input, 65535));
#endif
    return true;
}

class Checker {
    Model dut;
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
#define LOAD(member, index) root->CPU_MEMBER(member) = r[index]
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
        root->CPU_MEMBER(q) = r[22] != 0;
        dut.eval();
    }

    Registers snapshot() const {
        const auto *root = dut.rootp;
#define GET(member) unsigned(root->CPU_MEMBER(member))
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

#ifdef PIN_QUALIFICATION
    bool old_rd = true, old_wr = true, old_m1 = true, old_refresh = true;
    unsigned active_input = 0, next_transaction = 0, m1_count = 0;
    std::vector<unsigned> refreshes;

    void settle(const Fixture &fixture) {
        dut.eval();
        if (!dut.iorq_n && !dut.rd_n) {
            dut.din = !old_rd ? active_input :
                next_port < fixture.ports.size() ? fixture.ports[next_port].value : 0;
        } else dut.din = memory[dut.a];
        dut.eval();
    }

    void observe(const Fixture &fixture) {
        if (!dut.rd_n && !dut.wr_n) errors.push_back("RD and WR asserted together");
        if (!dut.mreq_n && !dut.iorq_n) errors.push_back("memory and I/O selected together");
        if (!dut.rfsh_n && (!dut.rd_n || !dut.wr_n || !dut.iorq_n))
            errors.push_back("refresh overlaps a read/write or I/O strobe");
        if (!dut.busak_n) errors.push_back("unexpected DMA grant without BUSRQ");
        if (!dut.m1_n && old_m1) ++m1_count;
        if (!dut.rfsh_n && old_refresh) refreshes.push_back(dut.a);
        if ((!dut.rd_n && old_rd) || (!dut.wr_n && old_wr)) {
            const bool write = !dut.wr_n, io = !dut.iorq_n;
            const unsigned kind = (io ? 2U : 0U) + unsigned(write);
            const unsigned address = dut.a, value = write ? dut.dout : dut.din;
            if (dut.mreq_n && dut.iorq_n)
                errors.push_back("read/write strobe without memory or I/O selection");
            if (next_transaction >= fixture.transactions.size())
                errors.push_back("unexpected strobed bus transaction");
            else {
                const auto &expected = fixture.transactions[next_transaction];
                const std::string label = "transaction[" + std::to_string(next_transaction) + "]";
                if (kind != expected.kind) mismatch(label + " kind", kind, expected.kind);
                if (address != expected.address) mismatch(label + " address", address, expected.address);
                if (value != expected.value) mismatch(label + " data", value, expected.value);
            }
            ++next_transaction;
            if (io) {
                if (next_port == fixture.ports.size()) errors.push_back("unexpected I/O transaction");
                else {
                    const auto &port = fixture.ports[next_port++];
                    if (address != port.address) mismatch("I/O address", address, port.address);
                    if (write != bool(port.write)) errors.push_back("I/O direction differs from fixture");
                    if (value != port.value) mismatch("I/O byte", value, port.value);
                }
                active_input = value;
            } else if (write) {
                if (!final_addresses[address]) errors.push_back("write outside final fixture RAM");
                memory[address] = value; initialized[address] = true;
            } else if (!initialized[address]) errors.push_back("strobed read of RAM absent from initial fixture");
        }
        old_rd = dut.rd_n; old_wr = dut.wr_n;
        old_m1 = dut.m1_n; old_refresh = dut.rfsh_n;
    }

    bool run(const Fixture &fixture) {
        dut.ce_p = 0; dut.ce_n = 0; dut.wait_n = 1; dut.busrq_n = 1;
        dut.int_n = 1; dut.nmi_n = 1; dut.din = 0;
        dut.clk = 0; dut.reset = 1; dut.eval(); edge();
        dut.reset = 0; dut.eval(); seed(fixture.initial);
        next_transaction = 0; m1_count = 0; refreshes.clear();
        old_rd = old_wr = old_m1 = old_refresh = true;
        settle(fixture); observe(fixture);
        bool complete = false;
        unsigned ticks = 0;
        for (; ticks != 1024; ) {
            // One negative and one positive enable form an unstalled T state.
            dut.ce_n = 1; dut.ce_p = 0; settle(fixture); edge();
            settle(fixture); observe(fixture);
            dut.ce_n = 0; dut.ce_p = 1; settle(fixture); edge();
            settle(fixture); ++ticks;
            if (dut.illegal) { errors.push_back("NMOS instruction was rejected"); break; }
            if (dut.retired) { complete = true; break; }
            // Retirement may immediately present the next instruction's T1.
            // Stop before counting that following M1 or any following strobe.
            observe(fixture);
        }
        if (ticks != fixture.t_states) mismatch("total T states", ticks, fixture.t_states);
        if (next_transaction != fixture.transactions.size())
            mismatch("strobed transaction count", next_transaction, fixture.transactions.size());
        if (m1_count != fixture.refreshes.size()) mismatch("M1 count", m1_count, fixture.refreshes.size());
        if (refreshes.size() != fixture.refreshes.size())
            mismatch("refresh count", refreshes.size(), fixture.refreshes.size());
        for (unsigned index = 0; index < refreshes.size() && index < fixture.refreshes.size(); ++index)
            if (refreshes[index] != fixture.refreshes[index])
                mismatch("refresh[" + std::to_string(index) + "] address", refreshes[index], fixture.refreshes[index]);
        return complete;
    }
#endif

public:
    const std::vector<std::string> &check(const Fixture &fixture) {
        errors.clear(); next_port = 0;
        memory.fill(0); initialized.fill(false); final_addresses.fill(false);
        for (const auto &byte : fixture.initial_ram) {
            memory[byte.address] = byte.value;
            initialized[byte.address] = true;
        }
        for (const auto &byte : fixture.final_ram) final_addresses[byte.address] = true;
#ifdef PIN_QUALIFICATION
        const bool complete = run(fixture);
#else
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
#endif
        if (!complete) errors.push_back("instruction did not retire within simulation bound");
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
        std::cerr << "usage: NMOS-vector-checker fixture-stream max-detailed-failures\n";
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
        std::cout << "NMOS external "
#ifdef PIN_QUALIFICATION
                  << "pin/state"
#else
                  << "state"
#endif
                  << " vectors: " << total << " checked, "
                  << total - failed << " passed, " << failed << " failed\n";
        for (const auto &[opcode, count] : failures_by_opcode)
            std::cout << "  " << opcode << ": " << count << " failed\n";
        std::cout << "Compared all supplied architectural state, WZ, encoded Q, P, EI, IFF/IM, RAM and I/O.\n";
#ifdef PIN_QUALIFICATION
        std::cout << "Also compared total T states, ordered strobed memory/I/O addresses/data, M1 count and refresh addresses.\n"
                  << "Corpus simplifies memory strobes and omits M1/RFSH; M1/refresh expectations follow public prefix rules.\n"
                  << "Excluded strobe widths, half-cycle phases and unstrobed addresses; software oracle, no physical hardware acceptance.\n";
#else
        std::cout << "Excluded exact bus waveform; no physical hardware acceptance.\n";
#endif
        return failed == 0 ? 0 : 1;
    } catch (const std::exception &error) {
        std::cerr << error.what() << '\n';
        return 2;
    }
}
