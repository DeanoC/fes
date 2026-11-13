// SPDX-License-Identifier: GPL-2.0-or-later
// Run a synthetic ROM through the motherboard's real RDY-as-enable path.
#include "Vapple2_cpu_sim_top.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <initializer_list>
#include <vector>

struct Cycle { uint16_t addr; uint8_t data; bool write; };

static void firmware() {
    std::array<uint8_t, 16384> rom{};
    rom.fill(0xea);
    auto bytes = [&](uint16_t addr, std::initializer_list<uint8_t> data) {
        for (auto byte : data) rom[addr++ & 0x3fff] = byte;
    };
    // Set up SP, then JSR to a subroutine with a zero-page INC. The IRQ
    // is raised by the simulated slot after the loop begins, between enables.
    bytes(0xd000, {0x78, 0xd8, 0xa2, 0xff, 0x9a, 0xa9, 0x40, 0x85, 0x20,
                   0x20, 0x40, 0xd0, 0xa5, 0x20, 0x85, 0x21,
                   0x58, 0xea, 0xea, 0x4c, 0x13, 0xd0});
    bytes(0xd040, {0xe6, 0x20, 0x60});       // INC $20; RTS
    bytes(0xd100, {0xa9, 0xa5, 0x85, 0x22, 0x40}); // marker; RTI
    bytes(0xfffc, {0x00, 0xd0, 0x00, 0xd1});
    std::filesystem::create_directories("build/diagnostics/fes-apple2");
    std::ofstream out("build/diagnostics/fes-apple2/firmware.hex");
    for (auto byte : rom) {
        char line[4];
        std::snprintf(line, sizeof(line), "%02x\n", byte);
        out << line;
    }
    if (!out) { std::fprintf(stderr, "cannot write synthetic ROM\n"); std::exit(1); }
}

static bool same(const Cycle& a, uint16_t addr, bool write, uint8_t data = 0) {
    return a.addr == addr && a.write == write && (!write || a.data == data);
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    firmware();
    Vapple2_cpu_sim_top top;
    top.clk_sys = 0; top.reset = 1; top.irq = 0; top.eval();
    std::vector<Cycle> trace;
    unsigned last_cycle = 0, min_gap = 1000, max_gap = 0;
    bool irq_sent = false, irq_released = false, marker = false, resumed = false;
    for (unsigned tick = 0; tick < 200000; ++tick) {
        if (tick == 100) top.reset = 0;
        top.clk_sys = 0; top.eval();
        top.clk_sys = 1; top.eval();
        if (!top.reset && top.bus_cycle) {
            if (last_cycle) {
                unsigned gap = tick - last_cycle;
                if (gap < min_gap) min_gap = gap;
                if (gap > max_gap) max_gap = gap;
            }
            last_cycle = tick;
            trace.push_back({top.bus_addr, top.bus_data, bool(top.bus_write)});
            if (!irq_sent && top.bus_addr == 0xd013 && !top.bus_write) {
                top.irq = 1; // stays high across the next disabled system clocks
                irq_sent = true;
            }
            if (irq_sent && top.bus_addr == 0xfffe && !top.bus_write) {
                top.irq = 0;
                irq_released = true;
            }
            if (top.bus_write && top.bus_addr == 0x22 && top.bus_data == 0xa5) marker = true;
            if (marker && !top.bus_write && top.bus_addr == 0xd013) { resumed = true; break; }
        }
    }
    // Transistor-reference JSR bus order: opcode, low byte, stack dummy read,
    // return high, return low, high byte, first target fetch.
    const std::array<Cycle, 7> jsr = {{{0xd009,0,false}, {0xd00a,0,false},
        {0x01ff,0,false}, {0x01ff,0xd0,true}, {0x01fe,0x0b,true},
        {0xd00b,0,false}, {0xd040,0,false}}};
    bool jsr_ok = false;
    for (size_t i = 0; i + jsr.size() <= trace.size(); ++i) {
        bool match = true;
        for (size_t j = 0; j < jsr.size(); ++j)
            match &= same(trace[i+j], jsr[j].addr, jsr[j].write, jsr[j].data);
        jsr_ok |= match;
    }
    std::vector<uint8_t> zp_writes;
    bool result = false, vector_low = false, vector_high = false;
    for (const auto& cycle : trace) {
        if (cycle.write && cycle.addr == 0x20) zp_writes.push_back(cycle.data);
        if (cycle.write && cycle.addr == 0x21 && cycle.data == 0x41) result = true;
        if (!cycle.write && cycle.addr == 0xfffe) vector_low = true;
        if (!cycle.write && cycle.addr == 0xffff) vector_high = true;
    }
    bool rmw_ok = zp_writes == std::vector<uint8_t>{0x40, 0x40, 0x41};
    bool enabled = min_gap >= 51 && max_gap <= 52;
    if (!(jsr_ok && rmw_ok && result && irq_sent && irq_released && vector_low &&
          vector_high && marker && resumed && enabled)) {
        std::fprintf(stderr, "FAIL JSR=%d RMW=%d result=%d IRQ=%d/%d vector=%d/%d marker=%d resume=%d gap=%u..%u\n",
                     jsr_ok, rmw_ok, result, irq_sent, irq_released, vector_low, vector_high,
                     marker, resumed, min_gap, max_gap);
        for (const auto& c : trace)
            std::fprintf(stderr, "%04x %c %02x\n", c.addr, c.write ? 'W' : 'R', c.data);
        return 1;
    }
    std::printf("PASS Apple II CPU: JSR bus order, INC dummy/final writes, slot IRQ and RTI; enable gaps %u..%u clocks\n",
                min_gap, max_gap);
}
