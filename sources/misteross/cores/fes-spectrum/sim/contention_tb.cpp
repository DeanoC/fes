// SPDX-License-Identifier: GPL-2.0-or-later
// Replays Fuse's Z80 core tests (z80/tests/tests.in) through the NMOS Z80 and
// the Sinclair contention classifier. Fuse logs every point at which a 48K
// ULA would apply contention: MC for memory and internal T states (any
// address) and PC for I/O (high byte $40-$7F, or A0 low at T2). Matching
// those points per T state makes the stretched timing equal to Fuse's for
// any contention pattern. The fixtures are read from files; none are stored
// in this repository.
#include "Vcontention_top.h"
#include "Vcontention_top___024root.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <set>
#include <sstream>
#include <string>
#include <utility>
#include <vector>

#define CPU(member) contention_top__DOT__cpu__DOT__cpu__DOT__##member

namespace {

struct Test {
    std::string name;
    unsigned af = 0, bc = 0, de = 0, hl = 0, af_ = 0, bc_ = 0, de_ = 0, hl_ = 0;
    unsigned ix = 0, iy = 0, sp = 0, pc = 0, memptr = 0;
    unsigned i = 0, r = 0, iff1 = 0, iff2 = 0, im = 0, halted = 0, tstates = 0;
    std::vector<std::pair<unsigned, unsigned>> memory;
};

struct Expected {
    std::set<std::pair<unsigned, unsigned>> points;  // (time, address)
    unsigned final_tstates = 0;
};

bool read_test(std::istream &in, Test &t) {
    std::string line;
    while (std::getline(in, line) && line.find_first_not_of(" \t\r") == std::string::npos) {}
    if (!in) return false;
    std::istringstream name(line);
    name >> t.name;
    in >> std::hex >> t.af >> t.bc >> t.de >> t.hl >> t.af_ >> t.bc_ >> t.de_ >> t.hl_ >>
        t.ix >> t.iy >> t.sp >> t.pc >> t.memptr;
    in >> std::hex >> t.i >> t.r >> std::dec >> t.iff1 >> t.iff2 >> t.im >> t.halted >> t.tstates;
    t.memory.clear();
    for (;;) {
        std::string word;
        in >> word;
        if (word == "-1") break;
        unsigned address = std::stoul(word, nullptr, 16);
        for (;;) {
            in >> word;
            if (word == "-1") break;
            t.memory.push_back({address++ & 0xffff, std::stoul(word, nullptr, 16)});
        }
    }
    return bool(in);
}

bool read_expected(std::istream &in, std::string &name, Expected &e) {
    std::string line;
    while (std::getline(in, line) && line.find_first_not_of(" \t\r") == std::string::npos) {}
    if (!in) return false;
    std::istringstream header(line);
    header >> name;
    e.points.clear();
    // Events are indented; the register line is not.
    while (std::getline(in, line)) {
        if (line.empty() || line[0] != ' ') break;
        std::istringstream ev(line);
        unsigned time;
        std::string type, address;
        ev >> std::dec >> time >> type >> address;
        if (type == "MC" || type == "PC") e.points.insert({time, std::stoul(address, nullptr, 16)});
    }
    // line holds the main registers; the next line ends with the final T count.
    std::getline(in, line);
    std::istringstream second(line);
    std::string field;
    std::vector<std::string> fields;
    while (second >> field) fields.push_back(field);
    e.final_tstates = std::stoul(fields.back());
    // Changed memory lines until a blank line.
    while (std::getline(in, line) && line.find_first_not_of(" \t\r") != std::string::npos) {}
    return true;
}

class Runner {
    Vcontention_top dut;
    std::array<uint8_t, 65536> memory{};

    void edge() {
        dut.clk = 1; dut.eval();
        dut.clk = 0; dut.eval();
    }
    void settle() {
        dut.eval();
        dut.din = (!dut.iorq_n && !dut.rd_n) ? uint8_t(dut.a >> 8) : memory[dut.a];
        dut.eval();
    }

public:
    std::string run(const Test &t, const Expected &e) {
        memory.fill(0);
        for (const auto &[address, value] : t.memory) memory[address] = uint8_t(value);
        dut.ce_p = 0; dut.ce_n = 0; dut.din = 0;
        dut.clk = 0; dut.reset = 1; dut.eval(); edge();
        dut.reset = 0; dut.eval();
        auto *root = dut.rootp;
        root->CPU(a_reg) = t.af >> 8; root->CPU(f_reg) = t.af & 0xff;
        root->CPU(b_reg) = t.bc >> 8; root->CPU(c_reg) = t.bc & 0xff;
        root->CPU(d_reg) = t.de >> 8; root->CPU(e_reg) = t.de & 0xff;
        root->CPU(h_reg) = t.hl >> 8; root->CPU(l_reg) = t.hl & 0xff;
        root->CPU(af_alt) = t.af_; root->CPU(bc_alt) = t.bc_;
        root->CPU(de_alt) = t.de_; root->CPU(hl_alt) = t.hl_;
        root->CPU(ix) = t.ix; root->CPU(iy) = t.iy; root->CPU(sp) = t.sp;
        root->CPU(pc) = t.pc; root->CPU(wz) = t.memptr;
        root->CPU(i_reg) = t.i; root->CPU(r_reg) = t.r;
        root->CPU(iff1) = t.iff1; root->CPU(iff2) = t.iff2; root->CPU(im) = t.im;
        settle();

        std::set<std::pair<unsigned, unsigned>> points;
        unsigned time = 0;
        bool done = false;
        while (!done && time < 4096) {
            // High half of T state 'time': the classifier sees the presented cycle.
            settle();
            const bool io = dut.cycle_kind == 3 || dut.cycle_kind == 4;
            const bool contended = (dut.a & 0xc000) == 0x4000;
            if (dut.ula_io_point || (dut.mem_point && (!io || contended)))
                points.insert({time, dut.a});
            dut.ce_n = 1; dut.ce_p = 0; settle(); edge();
            settle();
            if (dut.iorq_n && !dut.mreq_n && !dut.wr_n) memory[dut.a] = dut.dout;
            dut.ce_n = 0; dut.ce_p = 1; settle(); edge();
            ++time;
            if (dut.illegal) return "instruction rejected";
            if (dut.retired && time >= t.tstates) done = true;
        }
        if (!done) return "did not finish";
        if (time != e.final_tstates)
            return "T states " + std::to_string(time) + " expected " + std::to_string(e.final_tstates);
        // During internal T states after a refresh the NMOS core keeps the
        // refresh address (I and R before its increment) on the bus; Fuse's
        // harness logs R after the increment. Contention depends only on the
        // high address bits, so accept exactly that difference in R[6:0].
        const auto refresh_alias = [](unsigned actual) {
            return (actual & 0xff80) | ((actual + 1) & 0x7f);
        };
        std::set<std::pair<unsigned, unsigned>> matched;
        std::ostringstream diff;
        for (const auto &p : points) {
            if (p.first >= e.final_tstates) continue;
            if (e.points.count(p)) matched.insert(p);
            else if (e.points.count({p.first, refresh_alias(p.second)}))
                matched.insert({p.first, refresh_alias(p.second)});
            else diff << " extra " << std::dec << p.first << ":" << std::hex << p.second;
        }
        for (const auto &p : e.points)
            if (!matched.count(p)) diff << " missing " << std::dec << p.first << ":" << std::hex << p.second;
        return diff.str();
    }
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc < 3) {
        std::cerr << "usage: contention tests.in tests.expected\n";
        return 2;
    }
    std::ifstream in(argv[1]), expected(argv[2]);
    if (!in || !expected) {
        std::cerr << "cannot open Fuse test files\n";
        return 2;
    }
    Runner runner;
    unsigned passed = 0, failed = 0, skipped = 0;
    Test t;
    while (read_test(in, t)) {
        std::string name;
        Expected e;
        if (!read_expected(expected, name, e) || name != t.name) {
            std::cerr << "fixture streams disagree at " << t.name << '\n';
            return 2;
        }
        if (t.halted) { ++skipped; continue; }
        const std::string result = runner.run(t, e);
        if (result.empty()) ++passed;
        else {
            if (failed < 40) std::cerr << t.name << ":" << result << '\n';
            ++failed;
        }
    }
    std::cout << "spectrum contention points: " << passed << " passed, " << failed
              << " failed, " << skipped << " halted skipped\n";
    return failed ? 1 : 0;
}
