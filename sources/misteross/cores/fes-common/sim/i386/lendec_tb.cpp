// SPDX-License-Identifier: MIT
// Exhaustive check of the per-byte length decoders (fes_i386_lendec), and of
// each decoder's end (12 + slot + length, as L counts it), against
// the oracle's decoder. Every prefix (none or one of the eleven), every
// opcode (one-byte and 0Fh two-byte), every ModRM and every SIB byte, in
// both 16- and 32-bit default sizes. Lengths depend on no other byte.
//
// A case is compared when the oracle defines the encoding at CPU level 3.
// For LOCK the definition is taken without the LOCK prefix: whether LOCK is
// allowed is D's check, not a length. Each Verilator evaluation decodes up to
// four cases at once, in decoder slots k, k + 5, k + 10 and k + 15, and k
// rotates so every one of the 16 decoders is exercised.
//
// Output: one JSON line per default size, then mismatches (at most 20 per
// size). Exit status 1 on any mismatch.
#include "Vfes_i386_lendec.h"
#include "i386_oracle.h"
#include "verilated.h"

#include <array>
#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

namespace {

constexpr std::array<uint8_t, 11> kPrefixes = {0x26, 0x2e, 0x36, 0x3e, 0x64, 0x65, 0x66, 0x67, 0xf0, 0xf2, 0xf3};

bool is_prefix(uint8_t b) {
    for (uint8_t p : kPrefixes)
        if (p == b) return true;
    return false;
}

struct NullBus final : fes_i386::Bus {
    uint8_t *page(uint32_t, bool) override { return nullptr; }
    void read(uint32_t, unsigned length, uint8_t *data) override { std::fill(data, data + length, 0xff); }
    void write(uint32_t, unsigned, const uint8_t *) override {}
    uint32_t in(uint16_t, unsigned) override { return ~0u; }
    void out(uint16_t, unsigned, uint32_t) override {}
    uint8_t interrupt_acknowledge() override { return 0; }
};

// The opcodes: 244 one-byte (neither prefix nor 0Fh) and 256 after 0Fh.
std::vector<std::vector<uint8_t>> opcodes() {
    std::vector<std::vector<uint8_t>> list;
    for (int b = 0; b < 256; ++b)
        if (b != 0x0f && !is_prefix(uint8_t(b))) list.push_back({uint8_t(b)});
    for (int b = 0; b < 256; ++b) list.push_back({0x0f, uint8_t(b)});
    return list;
}

struct Counts {
    uint64_t cases = 0, compared = 0, undefined = 0, mismatches = 0;
    void add(const Counts &o) {
        cases += o.cases; compared += o.compared; undefined += o.undefined; mismatches += o.mismatches;
    }
};

struct Shared {
    const fes_i386::Oracle *oracle;
    std::vector<std::vector<uint8_t>> opcodes;
    std::atomic<size_t> next{0};
    std::mutex lock;
    Counts counts[2];
    std::vector<std::string> failures[2];
    uint64_t directed_checks = 0, directed_failures = 0;
};

struct Slot {
    bool prefix, run, opsize, adsize;
    uint8_t len[4];  // len[{os32, as32}]
    uint8_t end[4];  // where L counts the instruction's end: 12 + slot + len
};

Slot slot(const Vfes_i386_lendec &top, unsigned k) {
    Slot s;
    uint32_t lengths = top.len[k / 2] >> (16 * (k % 2));
    for (unsigned i = 0; i < 4; ++i) s.len[i] = uint8_t(lengths >> (4 * i)) & 0xf;
    for (unsigned i = 0; i < 4; ++i) {
        unsigned bit = 24 * k + 6 * i;
        uint64_t word = top.ends[bit / 32] | (bit / 32 + 1 < 12 ? uint64_t(top.ends[bit / 32 + 1]) << 32 : 0);
        s.end[i] = uint8_t(word >> (bit % 32)) & 0x3f;
    }
    s.prefix = (top.prefix >> k) & 1;
    s.run = (top.run >> k) & 1;
    s.opsize = (top.opsize >> k) & 1;
    s.adsize = (top.adsize >> k) & 1;
    return s;
}

void set_window(Vfes_i386_lendec &top, const uint8_t (&w)[20]) {
    for (unsigned i = 0; i < 5; ++i)
        top.w[i] = uint32_t(w[4 * i]) | uint32_t(w[4 * i + 1]) << 8 | uint32_t(w[4 * i + 2]) << 16
                 | uint32_t(w[4 * i + 3]) << 24;
    top.eval();
}

std::string hex(const uint8_t *bytes, unsigned n) {
    std::string text;
    char buffer[4];
    for (unsigned i = 0; i < n; ++i) {
        std::snprintf(buffer, sizeof buffer, "%s%02x", i ? " " : "", bytes[i]);
        text += buffer;
    }
    return text;
}

void worker(Shared &shared) {
    VerilatedContext context;
    Vfes_i386_lendec top(&context);
    Counts counts[2];
    std::vector<std::string> failures[2];
    const size_t work = shared.opcodes.size() * (kPrefixes.size() + 1);
    unsigned rotation = 0;

    for (size_t item; (item = shared.next++) < work;) {
        const std::vector<uint8_t> &opcode = shared.opcodes[item % shared.opcodes.size()];
        const int prefix = int(item / shared.opcodes.size()) - 1;  // -1: none
        const uint8_t p = prefix < 0 ? 0 : kPrefixes[size_t(prefix)];

        // Instruction bytes: prefix, opcode, ModRM, SIB, then zero bytes.
        uint8_t head[3];
        unsigned head_length = 0;
        if (prefix >= 0) head[head_length++] = p;
        for (uint8_t b : opcode) head[head_length++] = b;

        for (unsigned pair = 0; pair < 65536;) {
            unsigned base = rotation++ % 5;
            uint8_t window[20] = {};
            uint8_t cases[4][16];
            unsigned slots[4], count = 0;
            for (unsigned k = base; k < 16 && pair < 65536; k += 5, ++pair, ++count) {
                uint8_t *bytes = cases[count];
                std::fill(bytes, bytes + 16, 0);
                std::copy(head, head + head_length, bytes);
                bytes[head_length] = uint8_t(pair >> 8);      // ModRM
                bytes[head_length + 1] = uint8_t(pair);       // SIB
                std::copy(bytes, bytes + 5, window + k);
                slots[count] = k;
            }
            set_window(top, window);
            for (unsigned c = 0; c < count; ++c) {
                Slot s = slot(top, slots[c]);
                const uint8_t *bytes = cases[c];
                bool flags_ok = s.prefix == (prefix >= 0) && !s.run && s.opsize == (p == 0x66) && s.adsize == (p == 0x67);
                for (unsigned i = 0; i < 4; ++i) flags_ok = flags_ok && s.end[i] == 12 + slots[c] + s.len[i];
                uint8_t unlocked[16];
                std::copy(bytes, bytes + 16, unlocked);
                if (p == 0xf0) unlocked[0] = 0x26;
                for (int code32 = 0; code32 < 2; ++code32) {
                    Counts &n = counts[code32];
                    ++n.cases;
                    fes_i386::Decoded oracle = shared.oracle->decode(bytes, 15, code32);
                    bool defined = oracle.complete
                        && !(p == 0xf0 ? shared.oracle->decode(unlocked, 15, code32) : oracle).undefined;
                    unsigned os32 = unsigned(code32) ^ (p == 0x66), as32 = unsigned(code32) ^ (p == 0x67);
                    unsigned rtl = s.len[os32 * 2 + as32];
                    bool length_ok = true;
                    if (defined) {
                        ++n.compared;
                        length_ok = rtl == oracle.length;
                    } else {
                        ++n.undefined;
                    }
                    if (!flags_ok || !length_ok) {
                        ++n.mismatches;
                        if (failures[code32].size() < 20) {
                            char line[160];
                            std::snprintf(line, sizeof line,
                                          "%d-bit [%s] slot %u: rtl length %u prefix %d run %d, oracle length %u%s",
                                          code32 ? 32 : 16, hex(bytes, head_length + 2).c_str(), slots[c], rtl,
                                          s.prefix, s.run, oracle.length, defined ? "" : " (undefined)");
                            failures[code32].push_back(line);
                        }
                    }
                }
            }
        }
    }

    // Prefix runs: a prefix followed by a prefix, in every slot. MOV to and
    // from TR6/TR7 (0F 24, 0F 26), which the oracle rejects and the design
    // accepts, take the CR/DR rule: ModRM is always a register form.
    uint64_t directed_checks = 0, directed_failures = 0;
    if (shared.next.load() >= work) {
        for (uint8_t op : {uint8_t(0x24), uint8_t(0x26)}) {
            for (unsigned modrm = 0; modrm < 256; ++modrm) {
                for (unsigned k = 0; k < 16; ++k) {
                    uint8_t window[20] = {};
                    window[k] = 0x0f;
                    window[k + 1] = op;
                    window[k + 2] = uint8_t(modrm);
                    window[k + 3] = 0x25;  // a SIB that would add a disp32
                    set_window(top, window);
                    Slot s = slot(top, k);
                    ++directed_checks;
                    if (s.prefix || s.len[0] != 3 || s.len[1] != 3 || s.len[2] != 3 || s.len[3] != 3) ++directed_failures;
                }
            }
        }
        for (uint8_t first : kPrefixes) {
            for (uint8_t second : kPrefixes) {
                for (unsigned k = 0; k < 16; ++k) {
                    uint8_t window[20] = {};
                    window[k] = first;
                    window[k + 1] = second;
                    set_window(top, window);
                    Slot s = slot(top, k);
                    ++directed_checks;
                    if (!s.prefix || !s.run || s.opsize != (first == 0x66) || s.adsize != (first == 0x67)) ++directed_failures;
                }
            }
        }
    }

    std::lock_guard<std::mutex> guard(shared.lock);
    for (int m = 0; m < 2; ++m) {
        shared.counts[m].add(counts[m]);
        for (auto &line : failures[m])
            if (shared.failures[m].size() < 20) shared.failures[m].push_back(line);
    }
    // Every worker repeats the same run checks; keep one copy.
    if (!shared.directed_checks) {
        shared.directed_checks = directed_checks;
        shared.directed_failures = directed_failures;
    }
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    unsigned threads = std::thread::hardware_concurrency();
    if (const char *value = std::getenv("FES_I386_THREADS")) threads = unsigned(std::atoi(value));
    if (threads < 1) threads = 1;

    NullBus bus;
    std::unique_ptr<fes_i386::Oracle> oracle = fes_i386::make_oracle(bus);
    Shared shared;
    shared.oracle = oracle.get();
    shared.opcodes = opcodes();

    std::vector<std::thread> pool;
    for (unsigned t = 0; t < threads; ++t) pool.emplace_back(worker, std::ref(shared));
    for (auto &thread : pool) thread.join();

    bool failed = shared.directed_failures != 0;
    for (int m = 0; m < 2; ++m) {
        const Counts &n = shared.counts[m];
        std::printf("{\"default_size\": %d, \"opcodes\": %zu, \"prefix_choices\": %zu, \"cases\": %llu, "
                    "\"compared\": %llu, \"oracle_undefined\": %llu, \"mismatches\": %llu}\n",
                    m ? 32 : 16, shared.opcodes.size(), kPrefixes.size() + 1,
                    (unsigned long long)n.cases, (unsigned long long)n.compared,
                    (unsigned long long)n.undefined, (unsigned long long)n.mismatches);
        for (auto &line : shared.failures[m]) std::printf("MISMATCH %s\n", line.c_str());
        failed |= n.mismatches != 0;
    }
    std::printf("{\"directed\": %llu, \"directed_failures\": %llu}\n",
                (unsigned long long)shared.directed_checks, (unsigned long long)shared.directed_failures);
    return failed ? 1 : 0;
}
