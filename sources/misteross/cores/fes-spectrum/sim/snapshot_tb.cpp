// SPDX-License-Identifier: GPL-2.0-or-later
// Host simulation of a snapshot restore on one Spectrum model: the bench
// streams a restore stream (written by FogCast's fes-spectrum-restore) through
// the cassette unit with random gaps, then checks every register, the paging,
// border and AY state and all RAM at the first opcode fetch from real memory,
// and that the restored program then runs.
//
// usage: Vspectrum_sim_top stream.bin expect.txt banks.bin
//        Vspectrum_sim_top --refuse stream.bin
// expect.txt holds "name hex" lines; "banks n..." gives the order of the
// 16 KiB banks in banks.bin, and "marker bank offset value" a byte the
// restored program writes. --refuse checks that a stream of a class this
// model cannot run is left to the deck and never restored.
#include "Vspectrum_sim_top.h"
#include "Vspectrum_sim_top___024root.h"
#include "spectrum_ram_banks.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <iterator>
#include <map>
#include <random>
#include <sstream>
#include <string>
#include <vector>

#define MACHINE(member) spectrum_sim_top__DOT__machine__DOT__##member
#define ENGINE(member) spectrum_sim_top__DOT__machine__DOT__cpu__DOT__cpu__DOT__##member

namespace {

#ifndef SPECTRUM_MODEL_NAME
#define SPECTRUM_MODEL_NAME "48K"
#endif

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "fes.spectrum " SPECTRUM_MODEL_NAME " snapshot: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

std::vector<uint8_t> read_file(const char* path) {
    std::ifstream file(path, std::ios::binary);
    require(file.good(), std::string("cannot read ") + path);
    return std::vector<uint8_t>((std::istreambuf_iterator<char>(file)), {});
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    const bool refuse = argc == 3 && std::string(argv[1]) == "--refuse";
    require(argc == 4 || refuse, "usage: stream.bin expect.txt banks.bin | --refuse stream.bin");
    const std::vector<uint8_t> stream = read_file(argv[refuse ? 2 : 1]);
    const std::vector<uint8_t> banks = refuse ? std::vector<uint8_t>() : read_file(argv[3]);
    std::map<std::string, unsigned> expect;
    std::vector<unsigned> bank_order;
    unsigned marker_bank = 0, marker_offset = 0, marker_value = 0;
    if (!refuse) {
        std::ifstream file(argv[2]);
        std::string line;
        while (std::getline(file, line)) {
            std::istringstream words(line);
            std::string name;
            words >> name;
            if (name == "banks") {
                for (unsigned b; words >> b;) bank_order.push_back(b);
            } else if (name == "marker") {
                words >> std::hex >> marker_bank >> marker_offset >> marker_value;
            } else if (!name.empty()) {
                unsigned value;
                words >> std::hex >> value;
                expect[name] = value;
            }
        }
    }
    require(banks.size() == bank_order.size() * 16384, "banks.bin does not match its bank list");

    Vspectrum_sim_top dut;
    auto& root = *dut.rootp;
    std::mt19937 random(1084);
    size_t pos = 0;
    uint64_t clocks = 0;
    bool played = false;
    unsigned gap = 0;
    auto clock = [&]() {
        if (!dut.tape_stream_enable) pos = 0;
        // As the media ring presents it: a byte stays valid until it is
        // taken, and the next one follows after a random gap of 0-15 clocks.
        if (gap) --gap;
        dut.tape_stream_valid = dut.tape_stream_enable && pos < stream.size() && !gap;
        dut.tape_stream_data = pos < stream.size() ? stream[pos] : 0;
        dut.eval();
        const bool took = dut.tape_stream_valid && dut.tape_stream_take;
        dut.clk_sys = 1; dut.eval();
        dut.clk_sys = 0; dut.eval();
        if (took) {
            ++pos;
            gap = random() % 4 == 0 ? random() % 16 : 0;
        }
        played |= dut.tape_playing;
        ++clocks;
    };
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.matrix = 0xffffffffffULL;
    dut.kempston = 0;
    dut.tape_ready = 0;
    dut.tape_size = stream.size();
    for (int i = 0; i < 64; ++i) clock();
    dut.reset = 0;
    // The machine boots its ROM for a while before the snapshot arrives.
    for (int i = 0; i < 200000; ++i) clock();
    dut.tape_ready = 1;

    if (refuse) {
        for (int i = 0; i < 2000000; ++i) {
            clock();
            require(!root.MACHINE(restoring) && !root.MACHINE(restore_reset), "restored a stream of another class");
        }
        require(root.MACHINE(deck_ready), "the deck did not get the refused stream");
        std::cout << "fes.spectrum " SPECTRUM_MODEL_NAME " snapshot: refused a stream of another class\n";
        return 0;
    }
    const uint64_t limit = 200000000ULL;
    while (!root.MACHINE(restoring) && clocks < limit) clock();
    require(root.MACHINE(restoring), "restore never started");
    const uint64_t started = clocks;
    while (root.MACHINE(restoring) && clocks < limit) clock();
    require(!root.MACHINE(restoring), "restore never finished");
    require(pos == stream.size(), "restore ended before the stream did");
    // The first opcode fetch from real memory.
    while (root.MACHINE(cpu_m1_n) && clocks < limit) clock();
    require(!root.MACHINE(cpu_m1_n), "no fetch after the restore");
    std::cout << "fes.spectrum " SPECTRUM_MODEL_NAME " snapshot: restored " << stream.size()
              << " stream bytes in " << (clocks - started) << " clocks\n";

    auto check = [&](const std::string& name, unsigned got) {
        auto it = expect.find(name);
        require(it != expect.end(), "expect.txt has no " + name);
        if (got != it->second) {
            std::ostringstream message;
            message << name << " = " << std::hex << got << ", want " << it->second;
            fail(message.str());
        }
    };
    // Every register, the paging, the AY and the RAM the stream restores.
    auto check_restored = [&]() {
        check("pc", root.MACHINE(cpu_a));
        check("af", root.ENGINE(a_reg) << 8 | root.ENGINE(f_reg));
        check("bc", root.ENGINE(b_reg) << 8 | root.ENGINE(c_reg));
        check("de", root.ENGINE(d_reg) << 8 | root.ENGINE(e_reg));
        check("hl", root.ENGINE(h_reg) << 8 | root.ENGINE(l_reg));
        check("af2", root.ENGINE(af_alt));
        check("bc2", root.ENGINE(bc_alt));
        check("de2", root.ENGINE(de_alt));
        check("hl2", root.ENGINE(hl_alt));
        check("ix", root.ENGINE(ix));
        check("iy", root.ENGINE(iy));
        check("sp", root.ENGINE(sp));
        check("i", root.ENGINE(i_reg));
        check("r", root.ENGINE(r_reg));
        check("im", root.ENGINE(im));
        check("iff1", root.ENGINE(iff1));
        check("border", root.MACHINE(border_q));
#if defined(SPECTRUM_MODEL_128K) || defined(SPECTRUM_MODEL_PLUS3) || defined(SPECTRUM_MODEL_PENTAGON)
        check("p7ffd", root.MACHINE(port_7ffd));
        auto ay = [&](unsigned r) { return root.MACHINE(psg__DOT__ay__DOT__registers)[r]; };
        if (expect.count("ay0"))
            for (unsigned r = 0; r < 16; ++r) check("ay" + std::to_string(r), ay(r));
#endif
#ifdef SPECTRUM_MODEL_PLUS3
        check("p1ffd", root.MACHINE(port_1ffd));
#endif
        for (size_t i = 0; i < bank_order.size(); ++i) {
            const uint8_t* memory = spectrum_ram_bank(root, bank_order[i]);
            require(memory != nullptr, "no RAM bank " + std::to_string(bank_order[i]));
            for (unsigned offset = 0; offset < 16384; ++offset)
                if (memory[offset] != banks[i * 16384 + offset]) {
                    std::ostringstream message;
                    message << "bank " << bank_order[i] << " offset " << std::hex << offset << " is "
                            << unsigned(memory[offset]) << ", want " << unsigned(banks[i * 16384 + offset]);
                    fail(message.str());
                }
        }
    };
    check_restored();
    require(!played, "the deck played the restore stream");

    // Three frames of the restored program.
    for (int i = 0; i < 3 * 71000 * 15; ++i) clock();
    const uint8_t* marked = spectrum_ram_bank(root, marker_bank);
    require(marked != nullptr && marked[marker_offset] == marker_value, "the restored program did not run");
    require(!root.MACHINE(restoring), "a second restore started");

    // Reset the machine without ejecting the snapshot. The same inserted
    // stream must be probed and restored again after reset.
    dut.reset = 1;
    for (int i = 0; i < 64; ++i) clock();
    dut.reset = 0;
    while (!root.MACHINE(restoring) && clocks < limit) clock();
    require(root.MACHINE(restoring), "snapshot did not restart after machine reset");
    while (root.MACHINE(restoring) && clocks < limit) clock();
    require(!root.MACHINE(restoring), "repeat restore never finished");
    require(pos == stream.size(), "repeat restore ended before the stream did");
    while (root.MACHINE(cpu_m1_n) && clocks < limit) clock();
    require(!root.MACHINE(cpu_m1_n), "no fetch after the repeated restore");
    check_restored();
    std::cout << "fes.spectrum " SPECTRUM_MODEL_NAME " snapshot: registers, paging, AY and RAM restored; program ran and restored again after reset\n";
    return 0;
}
