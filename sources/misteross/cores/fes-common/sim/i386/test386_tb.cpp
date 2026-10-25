// SPDX-License-Identifier: MIT
// Boots a test386.asm ROM image in the i386 oracle. The ROM sits below 1 MiB
// and is aliased below 4 GiB, as test386's README requires; the rest of the
// first 1 MiB is RAM. scripts/test_fes_i386_test386.py builds the ROM and reads
// this program's stdout:
//
//   POST <code> <step>        each byte written to the POST port
//   ERROR <linear> <step>     the ROM's error routine was entered from <linear>
//   FORGIVEN <linear> <step>  a listed check failed and was stepped over
//   STRAY <kind> <address>    an access outside RAM and ROM (first 16 kept)
//   END <reason> <steps> <cs> <eip>
//
// test386 stops at its first failed check. A divergence the harness already
// records would hide every later test, so --forgive names a check by the
// linear address and length of its instruction: a conditional branch there
// that is taken, or an exception raised there, resumes at the next
// instruction instead. Each use prints FORGIVEN.
//
// usage: test386_tb --rom <file> --post <port> --output <port> --text <file>
//                   --dump <file> --dump-bytes <n> --limit <steps> --error <linear>
//                   [--forgive <linear>:<length>]...
// FES_I386_TRACE=<step> prints every step from that step to stderr.
#include "i386_oracle.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <iterator>
#include <map>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

constexpr uint32_t RAM_BYTES = 1u << 20;

class RomBus final : public fes_i386::Bus {
public:
    RomBus(const std::vector<uint8_t> &image, uint16_t post_port, uint16_t output_port)
        : rom(checked_size(image.size())), post(post_port), output(output_port) {
        std::memcpy(rom.data(), image.data(), image.size());
        low = RAM_BYTES - uint32_t(rom.size());
        high = uint32_t(0x100000000ull - rom.size());
    }

    fes_i386::PageMemory ram{RAM_BYTES};
    fes_i386::PageMemory rom;
    std::string text;
    unsigned long step = 0;
    unsigned strays = 0;

    uint8_t *page(uint32_t paddr, bool write) override {
        if (paddr >= low && paddr < RAM_BYTES) return write ? nullptr : &rom[(paddr - low) & ~0xfffu];
        if (paddr >= high) return write ? nullptr : &rom[(paddr - high) & ~0xfffu];
        if (paddr < low) return &ram[paddr & ~0xfffu];
        return nullptr;
    }
    void read(uint32_t paddr, unsigned length, uint8_t *data) override {
        stray("read", paddr);
        std::memset(data, 0xff, length);
    }
    void write(uint32_t paddr, unsigned, const uint8_t *) override {
        // Writes to ROM are discarded, as on a board.
        if (!((paddr >= low && paddr < RAM_BYTES) || paddr >= high)) stray("write", paddr);
    }
    uint32_t in(uint16_t, unsigned length) override { return length == 4 ? ~0u : (1u << (8 * length)) - 1; }
    void out(uint16_t port, unsigned, uint32_t value) override {
        if (port == post) std::printf("POST %02x %lu\n", value & 0xff, step);
        else if (output && port == output) text.push_back(char(value & 0xff));
    }
    uint8_t interrupt_acknowledge() override { throw std::runtime_error("unexpected INTA"); }

private:
    uint16_t post, output;
    uint32_t low = 0, high = 0;

    static size_t checked_size(size_t bytes) {
        if (bytes != 0x10000 && bytes != 0x20000) throw std::runtime_error("ROM must be 64 KiB or 128 KiB");
        return bytes;
    }
    void stray(const char *kind, uint32_t paddr) {
        if (strays++ < 16) std::printf("STRAY %s %08x\n", kind, paddr);
    }
};

std::vector<uint8_t> read_file(const std::string &path) {
    std::ifstream input(path, std::ios::binary);
    if (!input) throw std::runtime_error("cannot read " + path);
    return std::vector<uint8_t>(std::istreambuf_iterator<char>(input), {});
}

uint32_t linear_eip(const fes_i386::State &state) {
    return state.seg[fes_i386::SEG_CS].base + state.eip;
}

void trace(unsigned long step, const fes_i386::State &s) {
    std::fprintf(stderr, "%lu %04x:%08x eax=%08x ebx=%08x ecx=%08x edx=%08x esi=%08x edi=%08x ebp=%08x esp=%08x"
                 " fl=%08x ds=%04x es=%04x ss=%04x cr0=%08x\n", step, s.seg[fes_i386::SEG_CS].selector, s.eip,
                 s.gpr[fes_i386::REG_EAX], s.gpr[fes_i386::REG_EBX], s.gpr[fes_i386::REG_ECX], s.gpr[fes_i386::REG_EDX],
                 s.gpr[fes_i386::REG_ESI], s.gpr[fes_i386::REG_EDI], s.gpr[fes_i386::REG_EBP], s.gpr[fes_i386::REG_ESP],
                 s.eflags, s.seg[fes_i386::SEG_DS].selector, s.seg[fes_i386::SEG_ES].selector,
                 s.seg[fes_i386::SEG_SS].selector, s.cr0);
}

struct Options {
    std::string rom, text, dump;
    uint16_t post = 0, output = 0;
    uint32_t dump_bytes = 0, error = 0;
    unsigned long limit = 0;
    std::map<uint32_t, uint32_t> forgive;  // linear address -> instruction length
};

Options parse(int argc, char **argv) {
    Options options;
    for (int n = 1; n < argc; ++n) {
        std::string flag = argv[n];
        if (n + 1 >= argc) throw std::runtime_error("missing value for " + flag);
        std::string value = argv[++n];
        auto number = [](const std::string &text) { return std::stoul(text, nullptr, 0); };
        if (flag == "--rom") options.rom = value;
        else if (flag == "--text") options.text = value;
        else if (flag == "--dump") options.dump = value;
        else if (flag == "--post") options.post = uint16_t(number(value));
        else if (flag == "--output") options.output = uint16_t(number(value));
        else if (flag == "--dump-bytes") options.dump_bytes = uint32_t(number(value));
        else if (flag == "--limit") options.limit = number(value);
        else if (flag == "--error") options.error = uint32_t(number(value));
        else if (flag == "--forgive") {
            size_t colon = value.find(':');
            if (colon == std::string::npos) throw std::runtime_error("--forgive needs <linear>:<length>");
            options.forgive[uint32_t(number(value.substr(0, colon)))] = uint32_t(number(value.substr(colon + 1)));
        } else throw std::runtime_error("unknown option " + flag);
    }
    if (options.rom.empty() || options.text.empty() || options.dump.empty() || !options.limit || !options.error)
        throw std::runtime_error("--rom, --text, --dump, --limit and --error are required");
    if (options.dump_bytes > RAM_BYTES) throw std::runtime_error("dump larger than RAM");
    return options;
}

}  // namespace

int main(int argc, char **argv) {
    try {
        Options options = parse(argc, argv);
        RomBus bus(read_file(options.rom), options.post, options.output);
        auto oracle = fes_i386::make_oracle(bus);
        oracle->reset();
        const char *trace_env = std::getenv("FES_I386_TRACE");
        unsigned long trace_from = trace_env ? std::stoul(trace_env, nullptr, 0) : ~0ul;
        const char *reason = "limit";
        for (; bus.step < options.limit; ++bus.step) {
            fes_i386::State before = oracle->get_state();
            uint32_t from = linear_eip(before);
            if (bus.step >= trace_from) trace(bus.step, before);
            fes_i386::StepResult result = oracle->step();
            if (bus.step >= trace_from)
                for (const auto &event : result.events)
                    std::fprintf(stderr, "  event kind=%d vector=%02x error=%04x\n", int(event.kind), event.vector, event.error_code);
            fes_i386::State after = oracle->get_state();
            auto forgiven = options.forgive.find(from);
            if (forgiven != options.forgive.end() && linear_eip(after) != from + forgiven->second) {
                std::printf("FORGIVEN %08x %lu\n", from, bus.step);
                before.eip += forgiven->second;
                oracle->set_state(before);
                continue;
            }
            if (linear_eip(after) == options.error && from != options.error)
                std::printf("ERROR %08x %lu\n", from, bus.step);
            if (result.shutdown) { reason = "shutdown"; break; }
            // test386 stops with CLI; HLT on completion and on error.
            if (result.halted && !(after.eflags & 0x200)) { ++bus.step; reason = "halt"; break; }
        }
        fes_i386::State state = oracle->get_state();
        std::printf("END %s %lu %04x %08x\n", reason, bus.step, state.seg[fes_i386::SEG_CS].selector, state.eip);
        std::ofstream(options.text, std::ios::binary) << bus.text;
        std::ofstream(options.dump, std::ios::binary).write(reinterpret_cast<const char *>(bus.ram.data()), options.dump_bytes);
        return 0;
    } catch (const std::exception &error) {
        std::cerr << "test386_tb: " << error.what() << '\n';
        return 2;
    }
}
