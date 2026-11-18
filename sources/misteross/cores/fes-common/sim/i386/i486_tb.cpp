// SPDX-License-Identifier: MIT
// Runs the directed 486 cases (scripts/fes_i486_cases.py) on the level-4
// oracle alone and prints what it did with each, for the runner to compare
// with what the documents say a 486 does. It links only against the level-4
// oracle (scripts/fes_i486_oracle.py) and refuses any other.
//
// stdin: one case a line, "<label> <probe> <address>:<hex>[;<address>:<hex>]..."
// stdout:
//   RESET <field>=<hex>...   the state after Oracle::reset()
//   CASE <label> end=<halt|shutdown|limit> exception=<vector|-> error=<hex|->
//        int=<vector|-> <register>=<hex>...
// Each case starts in real mode at 0000:1000 with SP 0F00h and CR0 60050030h
// and runs until the CPU halts or shuts down, at most LIMIT steps. The
// exception reported is the first one delivered, with its error code; the
// software interrupt is the last one taken.
#include "i386_oracle.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>

namespace {

constexpr uint32_t RAM_BYTES = 16u << 20;
constexpr unsigned LIMIT = 4096;

class RamBus final : public fes_i386::Bus {
public:
    fes_i386::PageMemory ram{RAM_BYTES};
    uint8_t *page(uint32_t paddr, bool) override { return paddr < RAM_BYTES ? &ram[paddr & ~0xfffu] : nullptr; }
    void read(uint32_t, unsigned length, uint8_t *data) override { std::memset(data, 0xff, length); }
    void write(uint32_t, unsigned, const uint8_t *) override {}
    uint32_t in(uint16_t, unsigned length) override { return length == 4 ? ~0u : (1u << (8 * length)) - 1; }
    void out(uint16_t, unsigned, uint32_t) override {}
    uint8_t interrupt_acknowledge() override { throw std::runtime_error("unexpected INTA"); }
};

void print_registers(const fes_i386::State &s) {
    static const char *const names[] = {"eax", "ecx", "edx", "ebx", "esp", "ebp", "esi", "edi"};
    for (int n = 0; n < fes_i386::REG_COUNT; ++n) std::printf(" %s=%x", names[n], s.gpr[n]);
    std::printf(" eflags=%x eip=%x cs=%x cs.base=%x cr0=%x cr2=%x cr3=%x dr6=%x dr7=%x cpl=%x", s.eflags, s.eip,
                s.seg[fes_i386::SEG_CS].selector, s.seg[fes_i386::SEG_CS].base, s.cr0, s.cr2, s.cr3, s.dr6, s.dr7, s.cpl);
}

}  // namespace

int main() {
    RamBus bus;
    std::unique_ptr<fes_i386::Oracle> oracle = fes_i386::make_oracle(bus);
    if (oracle->name() != "bochs-level4") {
        std::fprintf(stderr, "i486_tb needs the level-4 oracle, not %s\n", oracle->name().c_str());
        return 2;
    }
    oracle->reset();
    std::printf("RESET");
    print_registers(oracle->get_state());
    std::printf("\n");

    std::string line;
    while (std::getline(std::cin, line)) {
        std::istringstream words(line);
        std::string label, probe, image;
        if (!(words >> label >> probe >> image)) continue;
        std::vector<std::pair<uint32_t, uint32_t>> placed;  // address, length
        std::istringstream runs(image);
        for (std::string run; std::getline(runs, run, ';');) {
            const size_t colon = run.find(':');
            if (colon == std::string::npos || (run.size() - colon - 1) % 2) throw std::runtime_error("bad image: " + label);
            const uint32_t address = uint32_t(std::stoul(run.substr(0, colon), nullptr, 16));
            const uint32_t length = uint32_t((run.size() - colon - 1) / 2);
            if (address + length > RAM_BYTES) throw std::runtime_error("image outside RAM: " + label);
            for (uint32_t n = 0; n < length; ++n)
                bus.ram[address + n] = uint8_t(std::stoul(run.substr(colon + 1 + 2 * n, 2), nullptr, 16));
            placed.emplace_back(address, length);
        }
        oracle->invalidate(0, RAM_BYTES);
        fes_i386::State state;
        for (auto &segment : state.seg) segment = fes_i386::real_mode_segment(0);
        state.eip = 0x1000;
        state.gpr[fes_i386::REG_ESP] = 0x0f00;
        state.cr0 = 0x60050030;  // fes_i486_cases.START_CR0
        oracle->reset();
        oracle->set_state(state);
        const char *end = "limit";
        int exception = -1, soft = -1;
        uint32_t error = 0;
        for (unsigned step = 0; step < LIMIT; ++step) {
            const fes_i386::StepResult result = oracle->step();
            for (const fes_i386::Event &event : result.events) {
                if (event.kind == fes_i386::EventKind::Exception && exception < 0) {
                    exception = event.vector;
                    error = event.error_code;
                }
                if (event.kind == fes_i386::EventKind::SoftwareInterrupt) soft = event.vector;
            }
            if (result.shutdown) { end = "shutdown"; break; }
            if (result.halted) { end = "halt"; break; }
        }
        std::printf("CASE %s end=%s", label.c_str(), end);
        if (exception < 0) std::printf(" exception=- error=-");
        else std::printf(" exception=%d error=%x", exception, error);
        if (soft < 0) std::printf(" int=-");
        else std::printf(" int=%d", soft);
        print_registers(oracle->get_state());
        std::printf("\n");
        for (auto [address, length] : placed) std::memset(&bus.ram[address], 0, length);
        // What the program wrote: its data, the stacks and the page tables.
        std::memset(&bus.ram[0], 0, 0x30000);
    }
    return 0;
}
