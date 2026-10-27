// SPDX-License-Identifier: MIT
// Runs SingleStepTests/80386 states through the i386 oracle and compares
// architectural state only. scripts/test_fes_i386_vectors.py reads the MOO
// files and writes one line per test on stdin:
//
//   <label> <20 initial registers> <final mask> <final registers...>
//           <n> <address value>... <n> <address value>... <exception or -1>
//
// Registers are hexadecimal in MOO RG32 order: cr0 cr3 eax ebx ecx edx esi edi
// ebp esp cs ds es fs gs ss eip eflags dr6 dr7. Each failing test prints one
// "FAIL <label> <field> <expected> <actual>..." line; the last line is
// "SUMMARY <passed> <failed>".
#include "i386_oracle.h"

#include <array>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <map>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

using fes_i386::State;

constexpr std::array<const char *, 20> register_names = {
    "cr0", "cr3", "eax", "ebx", "ecx", "edx", "esi", "edi", "ebp", "esp",
    "cs", "ds", "es", "fs", "gs", "ss", "eip", "eflags", "dr6", "dr7"};
using Registers = std::array<uint32_t, register_names.size()>;
constexpr uint32_t RAM_BYTES = 16u << 20;  // the corpus maps 16 MiB of RAM
constexpr uint32_t EFLAGS_MASK = 0x0003ffff;  // the 386's architectural bits
constexpr int STEP_LIMIT = 64;

struct Test {
    std::string label;
    Registers initial{}, final{};
    uint32_t final_mask = 0;
    std::vector<std::pair<uint32_t, uint8_t>> initial_ram, final_ram;
    int exception = -1;
};

// 16 MiB RAM; I/O reads return all ones as on the capture board.
class CorpusBus final : public fes_i386::Bus {
public:
    fes_i386::PageMemory ram{RAM_BYTES};
    uint8_t *page(uint32_t paddr, bool) override {
        return paddr < RAM_BYTES ? &ram[paddr & ~0xfffu] : nullptr;
    }
    void read(uint32_t, unsigned length, uint8_t *data) override { std::memset(data, 0xff, length); }
    void write(uint32_t, unsigned, const uint8_t *) override {}
    uint32_t in(uint16_t, unsigned length) override { return length == 4 ? ~0u : (1u << (8 * length)) - 1; }
    void out(uint16_t, unsigned, uint32_t) override {}
    uint8_t interrupt_acknowledge() override { throw std::runtime_error("unexpected INTA"); }
};

uint32_t hex(std::istream &input) {
    std::string token;
    if (!(input >> token)) throw std::runtime_error("truncated test line");
    size_t used = 0;
    unsigned long value = std::stoul(token, &used, 16);
    if (used != token.size() || value > 0xffffffffUL) throw std::runtime_error("bad number: " + token);
    return uint32_t(value);
}

Test parse(const std::string &line) {
    std::istringstream input(line);
    Test test;
    if (!(input >> test.label)) throw std::runtime_error("empty test line");
    for (auto &value : test.initial) value = hex(input);
    test.final_mask = hex(input);
    test.final = test.initial;
    for (size_t n = 0; n < test.final.size(); ++n)
        if (test.final_mask & (1u << n)) test.final[n] = hex(input);
    for (auto *ram : {&test.initial_ram, &test.final_ram}) {
        uint32_t count = hex(input);
        if (count > 4096) throw std::runtime_error("RAM list too long");
        for (uint32_t n = 0; n < count; ++n) {
            uint32_t address = hex(input), value = hex(input);
            if (address >= RAM_BYTES || value > 0xff) throw std::runtime_error("bad RAM entry");
            ram->emplace_back(address, uint8_t(value));
        }
    }
    std::string exception;
    if (!(input >> exception)) throw std::runtime_error("missing exception field");
    test.exception = exception == "-1" ? -1 : int(std::stoul(exception, nullptr, 16));
    return test;
}

State to_state(const Registers &r) {
    State state;
    state.cr0 = r[0];
    state.cr3 = r[1];
    state.gpr[fes_i386::REG_EAX] = r[2];
    state.gpr[fes_i386::REG_EBX] = r[3];
    state.gpr[fes_i386::REG_ECX] = r[4];
    state.gpr[fes_i386::REG_EDX] = r[5];
    state.gpr[fes_i386::REG_ESI] = r[6];
    state.gpr[fes_i386::REG_EDI] = r[7];
    state.gpr[fes_i386::REG_EBP] = r[8];
    state.gpr[fes_i386::REG_ESP] = r[9];
    const int order[] = {fes_i386::SEG_CS, fes_i386::SEG_DS, fes_i386::SEG_ES,
                         fes_i386::SEG_FS, fes_i386::SEG_GS, fes_i386::SEG_SS};
    for (int n = 0; n < 6; ++n) state.seg[order[n]] = fes_i386::real_mode_segment(uint16_t(r[10 + n]));
    state.eip = r[16];
    state.eflags = r[17] & EFLAGS_MASK;
    state.dr6 = r[18];
    state.dr7 = r[19];
    return state;
}

Registers from_state(const State &state) {
    Registers r{};
    r[0] = state.cr0;
    r[1] = state.cr3;
    r[2] = state.gpr[fes_i386::REG_EAX];
    r[3] = state.gpr[fes_i386::REG_EBX];
    r[4] = state.gpr[fes_i386::REG_ECX];
    r[5] = state.gpr[fes_i386::REG_EDX];
    r[6] = state.gpr[fes_i386::REG_ESI];
    r[7] = state.gpr[fes_i386::REG_EDI];
    r[8] = state.gpr[fes_i386::REG_EBP];
    r[9] = state.gpr[fes_i386::REG_ESP];
    const int order[] = {fes_i386::SEG_CS, fes_i386::SEG_DS, fes_i386::SEG_ES,
                         fes_i386::SEG_FS, fes_i386::SEG_GS, fes_i386::SEG_SS};
    for (int n = 0; n < 6; ++n) r[10 + n] = state.seg[order[n]].selector;
    r[16] = state.eip;
    r[17] = state.eflags;
    r[18] = state.dr6;
    r[19] = state.dr7;
    return r;
}

std::string hex_string(uint32_t value) {
    std::ostringstream out;
    out << std::hex << value;
    return out.str();
}

// Returns the mismatches as "field expected actual" triples.
std::vector<std::string> run(fes_i386::Oracle &oracle, CorpusBus &bus, const Test &test, bool trace) {
    for (auto [address, value] : test.initial_ram) bus.ram[address] = value;
    for (auto [address, value] : test.initial_ram) oracle.invalidate(address, 1);
    oracle.set_state(to_state(test.initial));

    std::vector<fes_i386::Access> writes;
    int first_event = -1;
    bool halted = false;
    for (int step = 0; step < STEP_LIMIT && !halted; ++step) {
        fes_i386::StepResult result = oracle.step();
        if (trace) {
            State s = oracle.get_state();
            std::cerr << test.label << " step " << step << " retired=" << result.retired << " halted=" << result.halted
                      << " cs:eip=" << hex_string(s.seg[fes_i386::SEG_CS].selector) << ':' << hex_string(s.eip)
                      << " ecx=" << hex_string(s.gpr[fes_i386::REG_ECX]) << " esp=" << hex_string(s.gpr[fes_i386::REG_ESP])
                      << " eflags=" << hex_string(s.eflags);
            for (const auto &event : result.events)
                std::cerr << " event=" << int(event.kind) << '/' << hex_string(event.vector);
            for (const auto &write : result.writes)
                std::cerr << " write=" << hex_string(write.physical) << '/' << int(write.length);
            std::cerr << '\n';
        }
        writes.insert(writes.end(), result.writes.begin(), result.writes.end());
        if (first_event < 0 && !result.events.empty()) first_event = result.events.front().vector;
        halted = result.halted || result.shutdown;
    }

    std::vector<std::string> mismatches;
    auto mismatch = [&](const std::string &field, uint32_t expected, uint32_t actual) {
        mismatches.push_back(field + " " + hex_string(expected) + " " + hex_string(actual));
    };
    if (!halted) mismatches.push_back("halt 1 0");
    Registers actual = from_state(oracle.get_state());
    for (size_t n = 0; n < actual.size(); ++n) {
        if ((test.final_mask & (1u << n)) == 0) continue;
        uint32_t mask = n == 17 ? EFLAGS_MASK : 10 <= n && n <= 15 ? 0xffff : ~0u;
        if ((actual[n] & mask) != (test.final[n] & mask))
            mismatch(register_names[n], test.final[n] & mask, actual[n] & mask);
    }

    std::map<uint32_t, uint8_t> expected;
    for (auto [address, value] : test.initial_ram) expected[address] = value;
    for (auto [address, value] : test.final_ram) {
        expected[address] = value;
        if (bus.ram[address] != value) mismatch("ram." + hex_string(address), value, bus.ram[address]);
    }
    // The corpus lists every byte the 386EX read or wrote; a write anywhere
    // else did not happen on hardware.
    std::set<uint32_t> unlisted;
    for (const auto &write : writes)
        for (uint32_t n = 0; n < write.length; ++n)
            if (!expected.count(write.physical + n)) unlisted.insert(write.physical + n);
    for (uint32_t address : unlisted)
        mismatch("write." + hex_string(address), 0, address < RAM_BYTES ? bus.ram[address] : 0);

    if (first_event != test.exception)
        mismatch("exception", uint32_t(test.exception), uint32_t(first_event));

    for (uint32_t address : unlisted)
        if (address < RAM_BYTES) bus.ram[address] = 0;
    return mismatches;
}

// Leave RAM zeroed for the next test.
void clear(CorpusBus &bus, const Test &test) {
    for (auto [address, value] : test.initial_ram) bus.ram[address] = 0;
    for (auto [address, value] : test.final_ram) bus.ram[address] = 0;
}

}  // namespace

int main() {
    std::ios::sync_with_stdio(false);
    CorpusBus bus;
    auto oracle = fes_i386::make_oracle(bus);
    // FES_I386_TRACE=1 prints every step to stderr.
    bool trace = std::getenv("FES_I386_TRACE") != nullptr;
    unsigned long passed = 0, failed = 0;
    std::string line;
    while (std::getline(std::cin, line)) {
        if (line.empty()) continue;
        Test test = parse(line);
        std::vector<std::string> mismatches;
        try {
            mismatches = run(*oracle, bus, test, trace);
        } catch (const std::exception &error) {
            std::cerr << test.label << ": " << error.what() << '\n';
            mismatches = {"oracle-error 0 0"};
            oracle->reset();
        }
        clear(bus, test);
        if (mismatches.empty()) {
            ++passed;
            continue;
        }
        ++failed;
        std::cout << "FAIL " << test.label;
        for (const auto &item : mismatches) std::cout << ' ' << item;
        std::cout << '\n';
    }
    std::cout << "SUMMARY " << passed << ' ' << failed << std::endl;
    return 0;
}
