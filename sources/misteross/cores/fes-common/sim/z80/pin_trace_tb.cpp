// SPDX-License-Identifier: GPL-2.0-or-later
// Original raw-pin replay. All expectations come from the supplied trace;
// physical captures and synthetic manual fixtures are classified by the runner.
#include "Vfes_z80_nmos.h"
#include "verilated.h"
#include <array>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {
constexpr std::array<const char *, 10> names = {
    "a", "dout", "m1_n", "mreq_n", "iorq_n", "rd_n", "wr_n", "rfsh_n", "halt_n", "busak_n"
};
struct Row {
    unsigned rise = 0;
    std::array<unsigned, 5> inputs{};
    std::array<int, 10> outputs{};
};
int number(std::istream &input, int minimum, int maximum) {
    int value;
    if (!(input >> value)) throw std::runtime_error("truncated pin stream");
    if (value < minimum || value > maximum) throw std::runtime_error("pin value outside valid range");
    return value;
}
bool read(std::istream &input, Row &row) {
    if (input.peek() == std::char_traits<char>::eof()) return false;
    input >> std::ws;
    if (input.peek() == std::char_traits<char>::eof()) return false;
    row.rise = number(input, 0, 1);
    for (unsigned index = 0; index != row.inputs.size(); ++index)
        row.inputs[index] = number(input, 0, index == 0 ? 255 : 1);
    for (unsigned index = 0; index != row.outputs.size(); ++index)
        row.outputs[index] = number(input, index < 2 ? -1 : 0, index == 0 ? 65535 : index == 1 ? 255 : 1);
    return true;
}
} // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 3) { std::cerr << "usage: NMOS-pin-replay stream detail-limit\n"; return 2; }
    try {
        std::ifstream input(argv[1]);
        if (!input) throw std::runtime_error("cannot open pin stream");
        const unsigned detail_limit = std::stoul(argv[2]);
        Vfes_z80_nmos dut;
        dut.clk = 0; dut.reset = 1; dut.ce_p = 0; dut.ce_n = 0;
        dut.wait_n = 1; dut.int_n = 1; dut.nmi_n = 1; dut.busrq_n = 1; dut.din = 0;
        dut.eval(); dut.clk = 1; dut.eval(); dut.clk = 0; dut.eval();
        dut.reset = 0; dut.eval();
        unsigned rows = 0, failures = 0, observations = 0, addresses = 0, writes = 0;
        unsigned rising = 0, falling = 0, fetches = 0, refreshes = 0, irq = 0, dma = 0;
        bool old_m1 = true, old_rfsh = true, old_irq = false, old_dma = false;
        Row row;
        while (read(input, row)) {
            if (row.rise != unsigned(rows % 2 == 0)) throw std::runtime_error("clock edges must alternate from rise");
            dut.din = row.inputs[0]; dut.wait_n = row.inputs[1]; dut.int_n = row.inputs[2];
            dut.nmi_n = row.inputs[3]; dut.busrq_n = row.inputs[4];
            dut.eval();
            if (rows != 0) {
                dut.ce_p = row.rise; dut.ce_n = !row.rise;
                dut.clk = 1; dut.eval(); dut.clk = 0; dut.eval();
                dut.ce_p = 0; dut.ce_n = 0; dut.eval();
            }
            const std::array<unsigned, 10> actual = {
                dut.a, dut.dout, dut.m1_n, dut.mreq_n, dut.iorq_n,
                dut.rd_n, dut.wr_n, dut.rfsh_n, dut.halt_n, dut.busak_n
            };
            for (unsigned index = 0; index != actual.size(); ++index) {
                if (row.outputs[index] < 0) continue;
                ++observations;
                if (actual[index] == unsigned(row.outputs[index])) continue;
                ++failures;
                if (failures <= detail_limit)
                    std::cerr << "sample " << rows << ' ' << (row.rise ? "rise " : "fall ") << names[index]
                              << " got " << actual[index] << " expected " << row.outputs[index] << '\n';
            }
            if (dut.illegal) throw std::runtime_error("NMOS RTL rejected a captured opcode");
            row.rise ? ++rising : ++falling;
            if (row.outputs[0] >= 0) ++addresses;
            if (row.outputs[1] >= 0) ++writes;
            if (!dut.m1_n && old_m1) ++fetches;
            if (!dut.rfsh_n && old_rfsh) ++refreshes;
            const bool ack = !dut.m1_n && !dut.iorq_n, grant = !dut.busak_n;
            if (ack && !old_irq) ++irq;
            if (grant && !old_dma) ++dma;
            old_m1 = dut.m1_n; old_rfsh = dut.rfsh_n; old_irq = ack; old_dma = grant;
            ++rows;
        }
        if (rows < 9) throw std::runtime_error("pin stream lacks a full fetch with both edge phases");
        std::cout << "NMOS raw-pin replay: " << rows << " samples, " << observations << " observations, "
                  << failures << " mismatches\n"
                  << "Coverage: rise=" << rising << " fall=" << falling << " address=" << addresses
                  << " write-data=" << writes << " M1=" << fetches << " refresh=" << refreshes
                  << " IRQ-ack=" << irq << " DMA-grant=" << dma << '\n';
        return failures == 0 ? 0 : 1;
    } catch (const std::exception &error) {
        std::cerr << error.what() << '\n'; return 2;
    }
}
