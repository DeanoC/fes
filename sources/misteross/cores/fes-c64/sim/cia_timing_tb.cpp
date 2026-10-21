// SPDX-License-Identifier: MIT
// Replay pinned VICE old-6526 fixtures at native and boundary bus phases.
#include "Vc64_cia.h"
#include "Vc64_cia___024root.h"
#include "verilated.h"
#include "cia_timing_program.h"
#include "cia_timing_expected.h"

#include <array>
#include <cinttypes>
#include <cstdio>
#include <stdexcept>
#include <string>

struct Schedule {
    const char *name;
    unsigned write_phase, read_phase, period;
    bool fractional;
};

static void require(bool condition, const std::string &message) {
    if (!condition) throw std::runtime_error(message);
}

static uint64_t hash_field(uint64_t hash, uint32_t value) {
    for (unsigned byte = 0; byte < 4; ++byte) {
        hash ^= (value >> (8 * byte)) & 255;
        hash *= UINT64_C(1099511628211);
    }
    return hash;
}

static void run_case(unsigned id, const Schedule &schedule) {
    const auto &program = cia_timing_cases[id];
    const auto &expected = cia_timing_expected[id];
    const auto *events = cia_timing_events + program.first_event;
    const auto *reads = cia_timing_reads + expected.first_read;
    const auto *edges = cia_timing_edges + expected.first_edge;
    require(program.duration == expected.states, "fixture duration mismatch");
    Vc64_cia top;
    top.clk = top.phi = top.cs = top.we = top.reading = top.read_sample = 0;
    top.rs = top.din = 0;
    top.pa_pin = top.pb_pin = 255;
    top.tod_tick = 0;
    top.reset = 1;
    auto tick = [&]() {
        top.clk = 0;
        top.eval();
        top.clk = 1;
        top.eval();
    };
    tick();
    top.reset = 0;
    auto value = [&](unsigned reg) {
        top.rs = reg;
        top.eval();
        return unsigned(top.dout);
    };
    unsigned next_event = 0, next_read = 0, next_edge = 0;
    bool previous_pin = false;
    uint64_t accumulator = 0, hash = UINT64_C(14695981039346656037);
    for (unsigned cycle = 1; cycle <= program.duration; ++cycle) {
        CIATimingEvent event{};
        const bool active = next_event < program.event_count && events[next_event].cycle == cycle;
        if (active) event = events[next_event++];
        const bool writing = active && event.op == 'W';
        const bool reading = active && event.op == 'R';
        const bool resetting = active && event.op == 'X';
        unsigned period = schedule.period;
        if (schedule.fractional) {
            // Match the motherboard's fractional 52.224 MHz / 985248 Hz enable.
            period = 0;
            do {
                accumulator += 985248;
                ++period;
            } while (accumulator < 52224000);
            accumulator -= 52224000;
        }
        for (unsigned phase = 0; phase < period; ++phase) {
            top.reset = resetting;
            top.phi = phase == 0;
            top.tod_tick = active && event.op == 'T' && phase == 0;
            top.cs = active;
            top.we = writing && phase == schedule.write_phase;
            top.rs = event.reg;
            top.din = event.value;
            top.reading = reading;
            top.read_sample = reading && phase == schedule.read_phase;
            if (top.read_sample) {
                top.eval();
                require(next_read < expected.read_count, "unexpected register read");
                const auto &want = reads[next_read++];
                require(want.cycle == cycle && want.reg == event.reg && want.value == top.dout,
                        "read mismatch at cycle " + std::to_string(cycle) +
                        ", register " + std::to_string(event.reg) +
                        ", got " + std::to_string(top.dout) +
                        ", expected " + std::to_string(want.value));
            }
            tick();
            if (bool(top.irq) != previous_pin) {
                require(next_edge < expected.edge_count, "unexpected IRQ edge");
                const auto &want = edges[next_edge++];
                require(want.cycle == cycle && want.value == top.irq,
                        "IRQ edge mismatch at cycle " + std::to_string(cycle));
                // No timer or mask change may move the pin in a SYS-only gap.
                // Read clears occur on read_sample; reset and assertions on Phi2.
                const unsigned expected_phase = top.irq || resetting ? 0 : schedule.read_phase;
                require(phase == expected_phase && (top.irq || resetting || reading),
                        "IRQ system phase mismatch at cycle " + std::to_string(cycle));
                previous_pin = top.irq;
            }
        }
        top.tod_tick = 0;
        top.cs = top.we = top.reading = top.read_sample = top.phi = top.reset = 0;
        const unsigned ta = value(4) | (value(5) << 8);
        const unsigned tb = value(6) | (value(7) << 8);
        const unsigned cra = value(14), crb = value(15), icr = value(13);
        const auto *root = top.rootp;
        const std::array<uint32_t, 10> state = {
            cycle, ta, unsigned(root->c64_cia__DOT__ta_latch),
            tb, unsigned(root->c64_cia__DOT__tb_latch), cra, crb, icr,
            unsigned(root->c64_cia__DOT__icr_mask), unsigned(top.irq)
        };
        for (auto field : state) hash = hash_field(hash, field);
    }
    require(next_event == program.event_count, "unconsumed program events");
    require(next_read == expected.read_count, "missing register read");
    require(next_edge == expected.edge_count, "missing IRQ edge");
    if (hash != expected.state_hash) {
        char message[160];
        std::snprintf(message, sizeof(message), "state trajectory mismatch: got %016" PRIx64
                      ", expected %016" PRIx64, hash, expected.state_hash);
        throw std::runtime_error(message);
    }
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    static_assert(CIA_TIMING_CASE_COUNT == sizeof(cia_timing_expected) / sizeof(cia_timing_expected[0]),
                  "program and oracle case counts differ");
    const Schedule schedules[] = {
        {"native 53/54, write27/read44", 27, 44, 0, true},
        {"early 53, write1/read2", 1, 2, 53, false},
        {"late 54, write51/read52", 51, 52, 54, false},
    };
    unsigned failed = 0, passed = 0;
    uint64_t states = 0, reads = 0, edges = 0;
    for (const auto &schedule : schedules) {
        for (unsigned id = 0; id < CIA_TIMING_CASE_COUNT; ++id) {
            try {
                run_case(id, schedule);
                ++passed;
                states += cia_timing_expected[id].states;
                reads += cia_timing_expected[id].read_count;
                edges += cia_timing_expected[id].edge_count;
            } catch (const std::exception &error) {
                if (++failed <= 8)
                    std::fprintf(stderr, "%s / %s: %s\n", schedule.name,
                                 cia_timing_cases[id].name, error.what());
            }
        }
    }
    if (failed) {
        std::fprintf(stderr, "CIA reference: %u passed, %u failed\n", passed, failed);
        return 1;
    }
    std::printf("CIA reference: %u cases x 3 schedules passed; %" PRIu64
                " cycle states, %" PRIu64 " reads, %" PRIu64 " ordered IRQ edges\n",
                unsigned(CIA_TIMING_CASE_COUNT), states, reads, edges);
    return 0;
}
