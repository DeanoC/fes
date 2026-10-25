// SPDX-License-Identifier: MIT
// Exhaustive check of the per-byte length decode, L1's decoders
// (fes_i386_lendec) and L2's sums (fes_i386_lstage) through lendec_tb_top,
// against the oracle's decoder. Every prefix (none or one of the eleven),
// every opcode (one-byte and 0Fh two-byte), every ModRM and every SIB byte, in
// both 16- and 32-bit default sizes. Lengths depend on no other byte.
//
// L2 reports each byte's length for the four prefix-accumulator variants
// (66h and/or 67h consumed before the byte). Variant 0 is compared with the
// oracle's decode of the case; variants 1-3 with its decode of the case behind
// those extra prefixes, less them. Each variant's end must be 12 + slot +
// length modulo 32 (as L counts it) and its next-half bit must match.
//
// A case is compared when the oracle defines the encoding at CPU level 3.
// For LOCK the definition is taken without the LOCK prefix: whether LOCK is
// allowed is D's check, not a length. Each Verilator evaluation decodes up to
// four cases at once, in decoder slots k, k + 5, k + 10 and k + 15, and k
// rotates so every one of the 16 decoders is exercised.
//
// Output: one JSON line per default size, then mismatches (at most 20 per
// size). Exit status 1 on any mismatch.
#include "Vlendec_tb_top.h"
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
    uint64_t cases = 0, compared = 0, undefined = 0, variants_compared = 0, mismatches = 0;
    void add(const Counts &o) {
        cases += o.cases; compared += o.compared; undefined += o.undefined;
        variants_compared += o.variants_compared; mismatches += o.mismatches;
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
    uint8_t len[4];  // by variant {66h seen, 67h seen}
    uint8_t fin[4];  // where L counts the end: 12 + slot + len, modulo 32
    bool far[4];     // ends past the slot's half
};

Slot slot(const Vlendec_tb_top &top, unsigned k) {
    Slot s;
    uint32_t lengths = top.len[k / 2] >> (16 * (k % 2));
    for (unsigned v = 0; v < 4; ++v) s.len[v] = uint8_t(lengths >> (4 * v)) & 0xf;
    for (unsigned v = 0; v < 4; ++v) {
        unsigned bit = 20 * k + 5 * v;
        uint64_t word = top.fin[bit / 32] | (bit / 32 + 1 < 10 ? uint64_t(top.fin[bit / 32 + 1]) << 32 : 0);
        s.fin[v] = uint8_t(word >> (bit % 32)) & 0x1f;
        s.far[v] = (top.past_half >> (4 * k + v)) & 1;
    }
    s.prefix = (top.prefix >> k) & 1;
    s.run = (top.run >> k) & 1;
    s.opsize = (top.opsize >> k) & 1;
    s.adsize = (top.adsize >> k) & 1;
    return s;
}

// L's end for a length: its position as L counts it, modulo 32, and whether
// that is past the slot's own half.
bool end_ok(const Slot &s, unsigned k, unsigned v, unsigned length) {
    unsigned end = 12 + k + length;
    return s.fin[v] == (end & 31) && s.far[v] == (end > (k < 4 ? 16u : 32u));
}

void set_window(Vlendec_tb_top &top, const uint8_t (&w)[20], bool code32) {
    for (unsigned i = 0; i < 5; ++i)
        top.w[i] = uint32_t(w[4 * i]) | uint32_t(w[4 * i + 1]) << 8 | uint32_t(w[4 * i + 2]) << 16
                 | uint32_t(w[4 * i + 3]) << 24;
    top.code32 = code32;
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

// The oracle's length of bytes behind the variant's extra 66h and 67h, less
// them; 0 when it does not define the encoding (for LOCK, without LOCK).
unsigned oracle_length(const fes_i386::Oracle &oracle, const uint8_t *bytes, bool code32, unsigned variant) {
    uint8_t buffer[16] = {}, unlocked[16];
    unsigned added = 0;
    if (variant & 2) buffer[added++] = 0x66;
    if (variant & 1) buffer[added++] = 0x67;
    std::copy(bytes, bytes + 16 - added, buffer + added);
    fes_i386::Decoded decoded = oracle.decode(buffer, 15, code32);
    if (!decoded.complete || decoded.undefined) {
        if (!decoded.complete || bytes[0] != 0xf0) return 0;
        std::copy(buffer, buffer + 16, unlocked);
        unlocked[added] = 0x26;
        if (oracle.decode(unlocked, 15, code32).undefined) return 0;
    }
    return decoded.length - added;
}

void worker(Shared &shared) {
    VerilatedContext context;
    Vlendec_tb_top top(&context);
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
            for (int code32 = 0; code32 < 2; ++code32) {
                set_window(top, window, code32);
                Counts &n = counts[code32];
                for (unsigned c = 0; c < count; ++c) {
                    Slot s = slot(top, slots[c]);
                    const uint8_t *bytes = cases[c];
                    bool flags_ok = s.prefix == (prefix >= 0) && !s.run && s.opsize == (p == 0x66) && s.adsize == (p == 0x67);
                    for (unsigned v = 0; v < 4; ++v) flags_ok = flags_ok && end_ok(s, slots[c], v, s.len[v]);
                    ++n.cases;
                    unsigned want[4];
                    bool length_ok = true;
                    for (unsigned v = 0; v < 4; ++v) {
                        want[v] = oracle_length(*shared.oracle, bytes, code32, v);
                        if (!want[v]) continue;
                        ++(v ? n.variants_compared : n.compared);
                        length_ok = length_ok && s.len[v] == want[v];
                    }
                    if (!want[0]) ++n.undefined;
                    if (!flags_ok || !length_ok) {
                        ++n.mismatches;
                        if (failures[code32].size() < 20) {
                            char line[200];
                            std::snprintf(line, sizeof line,
                                          "%d-bit [%s] slot %u: rtl lengths %u %u %u %u prefix %d run %d, "
                                          "oracle lengths %u %u %u %u (0: undefined)",
                                          code32 ? 32 : 16, hex(bytes, head_length + 2).c_str(), slots[c],
                                          s.len[0], s.len[1], s.len[2], s.len[3], s.prefix, s.run,
                                          want[0], want[1], want[2], want[3]);
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
                    for (int code32 = 0; code32 < 2; ++code32) {
                        set_window(top, window, code32);
                        Slot s = slot(top, k);
                        ++directed_checks;
                        bool ok = !s.prefix;
                        for (unsigned v = 0; v < 4; ++v) ok = ok && s.len[v] == 3 && end_ok(s, k, v, 3);
                        if (!ok) ++directed_failures;
                    }
                }
            }
        }
        for (uint8_t first : kPrefixes) {
            for (uint8_t second : kPrefixes) {
                for (unsigned k = 0; k < 16; ++k) {
                    uint8_t window[20] = {};
                    window[k] = first;
                    window[k + 1] = second;
                    set_window(top, window, false);
                    Slot s = slot(top, k);
                    ++directed_checks;
                    // Q consumes the first prefix alone: its next start is the byte after it.
                    bool ok = s.prefix && s.run && s.opsize == (first == 0x66) && s.adsize == (first == 0x67);
                    for (unsigned v = 0; v < 4; ++v) ok = ok && end_ok(s, k, v, 1);
                    if (!ok) ++directed_failures;
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
                    "\"compared\": %llu, \"oracle_undefined\": %llu, \"variants_compared\": %llu, "
                    "\"mismatches\": %llu}\n",
                    m ? 32 : 16, shared.opcodes.size(), kPrefixes.size() + 1,
                    (unsigned long long)n.cases, (unsigned long long)n.compared,
                    (unsigned long long)n.undefined, (unsigned long long)n.variants_compared,
                    (unsigned long long)n.mismatches);
        for (auto &line : shared.failures[m]) std::printf("MISMATCH %s\n", line.c_str());
        failed |= n.mismatches != 0;
    }
    std::printf("{\"directed\": %llu, \"directed_failures\": %llu}\n",
                (unsigned long long)shared.directed_checks, (unsigned long long)shared.directed_failures);
    return failed ? 1 : 0;
}
