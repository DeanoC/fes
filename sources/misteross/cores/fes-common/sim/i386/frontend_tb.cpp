// SPDX-License-Identifier: MIT
// fes_i386_frontend (fetch buffer, per-byte length decoders, Q) against the
// oracle's decoder, through frontend_tb_top.
//
// Every instruction Q hands to D is checked at the address Q gives: its
// linear address follows from the last one, its prefixes and opcode-aligned
// bytes match memory, and its length matches the oracle's decode whenever the
// oracle defines the encoding (for LOCK, the encoding without LOCK). An
// instruction the oracle cannot fit in 15 bytes must come out too_long.
//
// Streams:
//   random-bytes     uniform random memory
//   prefix-bytes     random memory where 30% of bytes are prefixes
//   prefix-runs      runs of 8-20 prefixes, so many instructions exceed 15 bytes
//   instructions     defined instructions with 0-4 random prefixes, D always ready:
//                    each instruction must issue max(1, prefixes) cycles after
//                    the one before it (the last prefix is free, each earlier
//                    one costs a cycle)
//   prefix-free      defined instructions without prefixes, D always ready:
//                    one instruction every cycle
//   prefix-free-long the same with every instruction 8 bytes or longer (5 in
//                    16-bit code, where the longest is 6)
// Timed streams also count the cycles from each run's redirect to its first
// issue, the front end's share of a mispredict.
// Random streams redirect to random addresses and, in half the runs, stall D
// at random. The *-fetch-latency streams delay each memory block by a random
// 0-12 cycles, so fetch stalls and L2 can be empty as Q leaves a half; their
// instructions are checked as above but not their timing, and each counts the
// cycles in which Q left a half that L2 held no block for (the case Q's
// next-half availability term decides). Output: one JSON line per stream;
// exit status 1 on any failure.
#include "Vfrontend_tb_top.h"
#include "i386_oracle.h"
#include "verilated.h"

#include <algorithm>
#include <array>
#include <cstdio>
#include <cstdlib>
#include <map>
#include <random>
#include <string>
#include <vector>

namespace {

constexpr std::array<uint8_t, 11> kPrefixes = {0x26, 0x2e, 0x36, 0x3e, 0x64, 0x65, 0x66, 0x67, 0xf0, 0xf2, 0xf3};
constexpr uint32_t kMemoryBytes = 1u << 20;  // memory repeats every 1 MiB of linear space

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

struct Prefixes {
    bool opsize = false, adsize = false, seg_valid = false, lock = false;
    unsigned seg = 0, rep = 0;
    void add(uint8_t b) {
        switch (b) {
        case 0x26: seg_valid = true; seg = 0; break;
        case 0x2e: seg_valid = true; seg = 1; break;
        case 0x36: seg_valid = true; seg = 2; break;
        case 0x3e: seg_valid = true; seg = 3; break;
        case 0x64: seg_valid = true; seg = 4; break;
        case 0x65: seg_valid = true; seg = 5; break;
        case 0x66: opsize = true; break;
        case 0x67: adsize = true; break;
        case 0xf0: lock = true; break;
        case 0xf2: rep = 2; break;
        case 0xf3: rep = 3; break;
        default: break;
        }
    }
    bool operator==(const Prefixes &o) const {
        return opsize == o.opsize && adsize == o.adsize && seg_valid == o.seg_valid && lock == o.lock
            && (!seg_valid || seg == o.seg) && rep == o.rep;
    }
};

struct Issued {
    uint64_t cycle;
    uint32_t linear;
    unsigned length, prefix_count;
    Prefixes prefixes;
    bool too_long;
    uint8_t bytes[11];
};

class Bench {
public:
    explicit Bench(VerilatedContext &context) : top(&context), memory(kMemoryBytes) {}

    Vfrontend_tb_top top;
    std::vector<uint8_t> memory;
    uint64_t cycle = 0;
    bool code32 = false;
    std::mt19937_64 *latency = nullptr;  // delays memory blocks when set
    uint64_t l2_empty_releases = 0;
    uint64_t fetch_wait_cycles = 0;
    bool last_fetch_waiting = false;

    uint8_t byte(uint32_t linear) const { return memory[linear % kMemoryBytes]; }

    void delay_block(uint32_t block, unsigned cycles) { response_stalls[block] = cycles; }

    void reset() {
        waiting = 0;
        fetch_wait_cycles = 0;
        address = 0;
        top.reset = 1;
        top.redirect = 0;
        top.d_ready = 1;
        top.mem_valid = 1;
        for (int n = 0; n < 3; ++n) clock(false);
        top.reset = 0;
    }

    // One clock. Returns whether D took an instruction this cycle.
    bool clock(bool d_ready, bool redirect = false, uint32_t target = 0, Issued *taken = nullptr) {
        top.redirect = redirect;
        top.redirect_linear = target;
        top.code32 = code32;
        top.d_ready = d_ready;
        top.clk = 0;
        top.eval();
        const bool load = top.mem_load;
        const uint32_t block = top.mem_block;
        last_fetch_waiting = !top.mem_valid;
        const bool handshake = top.q_valid && d_ready && !redirect;
        if (handshake && taken) capture(*taken);
        l2_empty_releases += top.l2_empty_release;
        top.clk = 1;
        top.eval();
        if (load) {
            address = block;
            waiting = 0;
            if (response_stalls.count(address)) waiting = response_stalls[address];
            else if (latency && (*latency)() % 2) waiting = unsigned((*latency)() % 12) + 1;
        } else if (waiting) {
            --waiting;
        }
        top.mem_valid = waiting == 0;
        if (last_fetch_waiting) ++fetch_wait_cycles;
        // Until the block arrives the port carries garbage.
        for (unsigned word = 0; word < 4; ++word) {
            uint32_t value = 0;
            for (unsigned i = 0; i < 4; ++i) value |= uint32_t(byte((address << 4) + 4 * word + i)) << (8 * i);
            top.mem_data[word] = waiting == 0 ? value : latency ? uint32_t((*latency)()) : ~value;
        }
        ++cycle;
        return handshake;
    }

private:
    uint32_t address = 0;  // the memory model's registered block address
    unsigned waiting = 0;  // cycles until its block arrives
    std::map<uint32_t, unsigned> response_stalls;  // fixed delays by block, ahead of latency

    void capture(Issued &out) {
        out.cycle = cycle;
        out.linear = top.q_linear;
        out.length = top.q_length;
        out.prefix_count = top.q_prefix_count;
        out.too_long = top.q_too_long;
        out.prefixes.opsize = top.q_opsize;
        out.prefixes.adsize = top.q_adsize;
        out.prefixes.seg_valid = top.q_seg_valid;
        out.prefixes.seg = top.q_seg;
        out.prefixes.rep = top.q_rep;
        out.prefixes.lock = top.q_lock;
        for (unsigned i = 0; i < 11; ++i) out.bytes[i] = uint8_t(top.q_bytes[i / 4] >> (8 * (i % 4)));
    }
};

// What the oracle says about the instruction at a linear address.
struct Expected {
    enum Kind { Defined, Undefined, TooLong } kind;
    unsigned length;   // Defined only
    unsigned prefixes; // prefix bytes before the opcode, at most 15
};

Expected expect(const fes_i386::Oracle &oracle, const Bench &bench, uint32_t linear) {
    uint8_t bytes[16];
    for (unsigned i = 0; i < 16; ++i) bytes[i] = bench.byte(linear + i);
    unsigned prefixes = 0;
    while (prefixes < 15 && is_prefix(bytes[prefixes])) ++prefixes;
    fes_i386::Decoded decoded = oracle.decode(bytes, 15, bench.code32);
    // Whether the encoding is defined is decided on the opcode with only the
    // size prefixes, which also fits for an over-long instruction. Segment
    // and REP prefixes do not change it at CPU level 3, and LOCK's validity
    // is D's check.
    Prefixes kinds;
    unsigned run = 0;
    while (run < 32 && is_prefix(bench.byte(linear + run))) kinds.add(bench.byte(linear + run++));
    uint8_t canonical[16] = {};
    unsigned n = 0;
    if (kinds.opsize) canonical[n++] = 0x66;
    if (kinds.adsize) canonical[n++] = 0x67;
    for (unsigned i = 0; n < 16; ++i) canonical[n++] = bench.byte(linear + run + i);
    fes_i386::Decoded unlocked = oracle.decode(canonical, 15, bench.code32);
    if (!unlocked.complete || unlocked.undefined) return {Expected::Undefined, 0, prefixes};
    if (!decoded.complete) return {Expected::TooLong, 0, prefixes};
    return {Expected::Defined, decoded.length, prefixes};
}

struct Report {
    std::string stream;
    bool code32 = false, fetch_latency = false;
    uint64_t l2_empty_releases = 0;
    uint64_t instructions = 0, compared = 0, undefined = 0, too_long = 0, escape = 0;
    std::map<unsigned, uint64_t> by_prefixes;
    uint64_t failures = 0;
    std::vector<std::string> examples;
    uint64_t cycles = 0, timing_checked = 0, timing_failures = 0;
    uint64_t issue_cycles = 0;  // timed streams: first to last issue of each run, inclusive
    std::map<unsigned, uint64_t> redirect_to_issue;  // timed streams: cycles from a redirect to its first issue
    uint64_t fetch_wait_cycles = 0, boundary_wait_cycles = 0, cross_half_instructions = 0;

    void fail(const std::string &text) {
        ++failures;
        if (examples.size() < 10) examples.push_back(text);
    }

    void print() const {
        auto histogram = [](const std::map<unsigned, uint64_t> &map) {
            std::string text;
            for (auto &[k, v] : map)
                text += (text.empty() ? "" : ", ") + std::string("\"") + std::to_string(k) + "\": " + std::to_string(v);
            return text;
        };
        std::string counts = histogram(by_prefixes), latency = histogram(redirect_to_issue);
        std::printf("{\"stream\": \"%s\", \"default_size\": %d, \"fetch_latency\": %s, "
                    "\"l2_empty_releases\": %llu, \"instructions\": %llu, \"compared\": %llu, "
                    "\"oracle_undefined\": %llu, \"too_long\": %llu, \"two_byte_opcode\": %llu, "
                    "\"by_prefix_count\": {%s}, \"cycles\": %llu, \"issue_cycles\": %llu, "
                    "\"redirect_to_issue\": {%s}, \"timing_checked\": %llu, \"timing_failures\": %llu, "
                    "\"fetch_wait_cycles\": %llu, \"boundary_wait_cycles\": %llu, "
                    "\"cross_half_instructions\": %llu, "
                    "\"failures\": %llu}\n",
                    stream.c_str(), code32 ? 32 : 16, fetch_latency ? "true" : "false",
                    (unsigned long long)l2_empty_releases, (unsigned long long)instructions,
                    (unsigned long long)compared, (unsigned long long)undefined, (unsigned long long)too_long,
                    (unsigned long long)escape, counts.c_str(), (unsigned long long)cycles,
                    (unsigned long long)issue_cycles, latency.c_str(), (unsigned long long)timing_checked,
                    (unsigned long long)timing_failures,
                    (unsigned long long)fetch_wait_cycles, (unsigned long long)boundary_wait_cycles,
                    (unsigned long long)cross_half_instructions,
                    (unsigned long long)failures);
        for (auto &line : examples) std::printf("FAIL %s: %s\n", stream.c_str(), line.c_str());
    }
};

std::string describe(const Bench &bench, const Issued &got, const char *what) {
    char text[256];
    int n = std::snprintf(text, sizeof text, "%s at %08x [", what, got.linear);
    for (unsigned i = 0; i < 15 && n < 200; ++i) n += std::snprintf(text + n, sizeof text - n, "%s%02x", i ? " " : "", bench.byte(got.linear + i));
    std::snprintf(text + n, sizeof text - n, "]: rtl length %u prefixes %u too_long %d", got.length, got.prefix_count, got.too_long);
    return text;
}

// Check one instruction; returns the oracle's view.
Expected check(const fes_i386::Oracle &oracle, const Bench &bench, const Issued &got, Report &report) {
    Expected want = expect(oracle, bench, got.linear);
    ++report.instructions;
    ++report.by_prefixes[std::min(want.prefixes, 15u)];
    if (bench.byte(got.linear + want.prefixes) == 0x0f) ++report.escape;
    switch (want.kind) {
    case Expected::Undefined:
        ++report.undefined;
        // Bochs cannot provide a length for some #UD encodings. Check Q's
        // limit flag against its low length and prefix count, plus the
        // definite case where 15 leading prefixes leave no room for an opcode.
        // A non-prefix overlength wraps the four-bit total below its prefix count.
        if (got.too_long != (got.length < got.prefix_count || want.prefixes == 15)) {
            report.fail(describe(bench, got, got.too_long ? "unexpected too_long" : "expected too_long"));
            return want;
        }
        if (want.prefixes == 15) return want;
        {
            Prefixes prefixes;
            for (unsigned i = 0; i < want.prefixes; ++i) prefixes.add(bench.byte(got.linear + i));
            if (got.prefix_count != want.prefixes || !(got.prefixes == prefixes))
                report.fail(describe(bench, got, "prefix mismatch"));
        }
        if (!got.too_long && got.length >= want.prefixes) {
            for (unsigned i = 0; i < got.length - want.prefixes && i < 11; ++i) {
                if (got.bytes[i] != bench.byte(got.linear + want.prefixes + i)) {
                    report.fail(describe(bench, got, "aligned bytes mismatch"));
                    break;
                }
            }
        }
        return want;
    case Expected::TooLong:
        ++report.too_long;
        if (!got.too_long) report.fail(describe(bench, got, "expected too_long"));
        return want;
    case Expected::Defined:
        ++report.compared;
        if (got.too_long || got.length != want.length) {
            char text[48];
            std::snprintf(text, sizeof text, "oracle length %u", want.length);
            report.fail(describe(bench, got, text));
            return want;
        }
        break;
    }
    // Prefixes and opcode-aligned bytes.
    Prefixes prefixes;
    for (unsigned i = 0; i < want.prefixes; ++i) prefixes.add(bench.byte(got.linear + i));
    if (got.prefix_count != want.prefixes || !(got.prefixes == prefixes)) report.fail(describe(bench, got, "prefix mismatch"));
    for (unsigned i = 0; got.length >= want.prefixes && i < got.length - want.prefixes && i < 11; ++i) {
        if (got.bytes[i] != bench.byte(got.linear + want.prefixes + i)) {
            report.fail(describe(bench, got, "aligned bytes mismatch"));
            break;
        }
    }
    return want;
}

enum class Fill {
    Uniform,     // uniform random bytes
    Prefixes,    // 30% of bytes are prefixes
    PrefixRuns,  // runs of 8-20 prefixes, each followed by six random bytes
};

// Random memory: follow Q's own stream, redirecting now and then.
Report random_stream(const fes_i386::Oracle &oracle, Bench &bench, std::mt19937_64 &random, const char *name,
                     Fill fill, uint64_t count, bool stall_d, bool fetch_latency = false) {
    Report report;
    report.stream = name;
    report.code32 = bench.code32;
    report.fetch_latency = fetch_latency;
    std::uniform_int_distribution<int> any(0, 255), which(0, int(kPrefixes.size()) - 1);
    std::uniform_int_distribution<unsigned> run_length(8, 20);
    std::bernoulli_distribution prefix(0.3), ready(stall_d ? 0.7 : 1.0), jump(1.0 / 300);
    for (uint32_t at = 0; at < kMemoryBytes;) {
        if (fill == Fill::PrefixRuns) {
            for (unsigned n = run_length(random); n && at < kMemoryBytes; --n) bench.memory[at++] = kPrefixes[size_t(which(random))];
            for (unsigned n = 6; n && at < kMemoryBytes; --n) bench.memory[at++] = uint8_t(any(random));
        } else {
            bench.memory[at++] = fill == Fill::Prefixes && prefix(random) ? kPrefixes[size_t(which(random))] : uint8_t(any(random));
        }
    }
    bench.latency = fetch_latency ? &random : nullptr;
    bench.reset();
    const uint64_t begin = bench.cycle, releases = bench.l2_empty_releases;

    uint32_t target = uint32_t(random()) % kMemoryBytes;
    bool redirect = true;
    uint32_t next = target;
    uint64_t progress = bench.cycle;
    while (report.instructions < count) {
        Issued got;
        bool d_ready = ready(random);
        bool taken = bench.clock(d_ready, redirect, target, &got);
        redirect = false;
        if (!taken) {
            if (bench.cycle - progress > 200) {
                report.fail("front end stopped issuing");
                break;
            }
            continue;
        }
        progress = bench.cycle;
        if (got.linear != next) {
            report.fail(describe(bench, got, "unexpected linear address"));
            break;
        }
        check(oracle, bench, got, report);
        next = got.linear + got.length;
        if (got.too_long || jump(random)) {
            target = uint32_t(random()) % kMemoryBytes;
            next = target;
            redirect = true;
        }
    }
    report.cycles = bench.cycle - begin;
    report.l2_empty_releases = bench.l2_empty_releases - releases;
    return report;
}

// A defined instruction at most 15 bytes long with the given prefixes.
std::vector<uint8_t> instruction(const fes_i386::Oracle &oracle, bool code32, std::mt19937_64 &random,
                                 unsigned prefix_count, unsigned min_length) {
    std::uniform_int_distribution<int> any(0, 255), which(0, int(kPrefixes.size()) - 1);
    for (;;) {
        uint8_t bytes[16];
        for (unsigned i = 0; i < prefix_count; ++i) bytes[i] = kPrefixes[size_t(which(random))];
        for (unsigned i = prefix_count; i < 16; ++i) bytes[i] = uint8_t(any(random));
        if (is_prefix(bytes[prefix_count])) continue;
        fes_i386::Decoded decoded = oracle.decode(bytes, 15, code32);
        uint8_t unlocked[16];
        std::copy(bytes, bytes + 16, unlocked);
        for (unsigned i = 0; i < prefix_count; ++i)
            if (unlocked[i] == 0xf0) unlocked[i] = 0x26;
        if (!decoded.complete || oracle.decode(unlocked, 15, code32).undefined) continue;
        if (decoded.length < min_length + prefix_count) continue;
        return std::vector<uint8_t>(bytes, bytes + decoded.length);
    }
}

// Laid-out defined instructions, D always ready: exact issue timing, unless
// fetch_latency delays memory, when only the instructions are checked.
Report timed_stream(const fes_i386::Oracle &oracle, Bench &bench, std::mt19937_64 &random, const char *name,
                    unsigned max_prefixes, unsigned min_length, uint64_t count, bool fetch_latency = false) {
    Report report;
    report.stream = name;
    report.code32 = bench.code32;
    report.fetch_latency = fetch_latency;
    std::uniform_int_distribution<unsigned> prefixes(0, max_prefixes);
    std::uniform_int_distribution<int> any(0, 255);
    bench.latency = fetch_latency ? &random : nullptr;
    bench.reset();
    const uint64_t begin = bench.cycle, releases = bench.l2_empty_releases;
    while (report.instructions < count) {
        // A run of instructions from a random start, then random bytes after it.
        uint32_t start = uint32_t(random()) % (kMemoryBytes - 65536);
        std::vector<unsigned> expected_prefixes;
        uint32_t at = start;
        while (at - start < 60000 && report.instructions + expected_prefixes.size() < count) {
            unsigned k = prefixes(random);
            std::vector<uint8_t> bytes = instruction(oracle, bench.code32, random, k, min_length);
            for (uint8_t b : bytes) bench.memory[at++ % kMemoryBytes] = b;
            expected_prefixes.push_back(k);
        }
        for (unsigned i = 0; i < 64; ++i) bench.memory[at++ % kMemoryBytes] = uint8_t(any(random));

        uint32_t next = start;
        bool redirect = true;
        const uint64_t redirected = bench.cycle;  // the clock that carries the redirect
        uint64_t previous = bench.cycle, first = 0;
        size_t index = 0;
        while (index < expected_prefixes.size()) {
            Issued got;
            bool taken = bench.clock(true, redirect, start, &got);
            redirect = false;
            if (!taken) {
                if (bench.cycle - previous > 256) {
                    report.fail("front end stopped issuing");
                    return report;
                }
                continue;
            }
            if (got.linear != next) {
                report.fail(describe(bench, got, "unexpected linear address"));
                return report;
            }
            check(oracle, bench, got, report);
            unsigned k = expected_prefixes[index];
            if (index > 0 && !fetch_latency) {
                ++report.timing_checked;
                uint64_t gap = got.cycle - previous;
                if (gap != std::max(1u, k)) {
                    ++report.timing_failures;
                    char text[96];
                    std::snprintf(text, sizeof text, "issued %llu cycles after the previous; %u prefixes",
                                  (unsigned long long)gap, k);
                    report.fail(describe(bench, got, text));
                }
            }
            if (index == 0) {
                first = got.cycle;
                if (!fetch_latency) ++report.redirect_to_issue[unsigned(got.cycle - redirected)];
            }
            previous = got.cycle;
            next = got.linear + got.length;
            ++index;
        }
        if (!fetch_latency) report.issue_cycles += previous - first + 1;
    }
    report.cycles = bench.cycle - begin;
    report.l2_empty_releases = bench.l2_empty_releases - releases;
    return report;
}

// Hold block 2 while Q releases half 0. The far CALL at byte 15 moves Q
// directly to byte 23 in half 1; the 10-byte MOV there crosses into byte 32
// in the delayed block. It must not issue using half 0's pre-edge valid bit.
Report stallable_fetch_stream(const fes_i386::Oracle &oracle, Bench &bench) {
    Report report;
    report.stream = "fetch-stalled";
    report.code32 = bench.code32;
    std::fill(bench.memory.begin(), bench.memory.end(), 0x90);
    const uint32_t long_start = 23;
    bench.memory[15] = 0x2e;  // Segment override + 32-bit far CALL: 8 bytes total.
    bench.memory[16] = 0x9a;
    for (unsigned i = 0; i < 6; ++i) bench.memory[17 + i] = uint8_t(0x31 + i);
    bench.memory[23] = 0xc7;  // MOV dword ptr [disp32], imm32: 10 bytes total.
    bench.memory[24] = 0x05;
    for (unsigned i = 0; i < 8; ++i) bench.memory[25 + i] = uint8_t(0x71 + i);

    bench.latency = nullptr;
    bench.delay_block(2, 100);
    bench.reset();
    const uint64_t begin = bench.cycle;
    uint32_t next = 0;
    bool redirect = true;
    uint32_t target = 0;
    uint64_t progress = bench.cycle;
    while (next < long_start + 10 + 4) {
        Issued got;
        const uint32_t waiting_at = next;
        const bool taken = bench.clock(true, redirect, target, &got);
        redirect = false;
        if (waiting_at == long_start && !taken && bench.last_fetch_waiting) {
            ++report.boundary_wait_cycles;
        }
        if (!taken) {
            if (bench.cycle - progress > 200) {
                report.fail("front end stopped issuing after a delayed fetch");
                break;
            }
            continue;
        }
        progress = bench.cycle;
        if (got.linear != next) {
            report.fail(describe(bench, got, "unexpected linear address"));
            break;
        }
        Expected want = check(oracle, bench, got, report);
        if (got.linear == long_start) {
            ++report.cross_half_instructions;
            if (bench.last_fetch_waiting)
                report.fail(describe(bench, got, "issued across a half before the fetch response was ready"));
            if (want.kind != Expected::Defined || got.length != 10)
                report.fail(describe(bench, got, "expected 10-byte cross-half instruction"));
        }
        next = got.linear + got.length;
    }
    report.fetch_wait_cycles = bench.fetch_wait_cycles;
    report.cycles = bench.cycle - begin;
    if (!report.fetch_wait_cycles) report.fail("memory model did not stall a fetch response");
    if (!report.boundary_wait_cycles) report.fail("Q did not wait at the cross-half instruction");
    if (report.cross_half_instructions != 1) report.fail("cross-half instruction did not issue exactly once");
    return report;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    uint64_t count = 400000;
    uint64_t seed = 1;
    if (const char *value = std::getenv("FES_I386_INSTRUCTIONS")) count = std::strtoull(value, nullptr, 10);
    if (const char *value = std::getenv("FES_I386_SEED")) seed = std::strtoull(value, nullptr, 10);

    NullBus bus;
    std::unique_ptr<fes_i386::Oracle> oracle = fes_i386::make_oracle(bus);
    VerilatedContext context;
    Bench bench(context);
    std::mt19937_64 random(seed);

    bool failed = false;
    for (int code32 = 0; code32 < 2; ++code32) {
        bench.code32 = code32;
        std::vector<Report> reports;
        reports.push_back(random_stream(*oracle, bench, random, "random-bytes", Fill::Uniform, count, false));
        reports.push_back(random_stream(*oracle, bench, random, "random-bytes-stalled", Fill::Uniform, count, true));
        reports.push_back(random_stream(*oracle, bench, random, "prefix-bytes", Fill::Prefixes, count, true));
        reports.push_back(random_stream(*oracle, bench, random, "prefix-runs", Fill::PrefixRuns, count / 4, true));
        reports.push_back(timed_stream(*oracle, bench, random, "instructions", 4, 0, count));
        reports.push_back(timed_stream(*oracle, bench, random, "prefix-free", 0, 0, count));
        // Without prefixes no 16-bit instruction is longer than 6 bytes.
        reports.push_back(timed_stream(*oracle, bench, random, "prefix-free-long", 0, code32 ? 8 : 5, count / 4));
        reports.push_back(random_stream(*oracle, bench, random, "random-bytes-fetch-latency", Fill::Uniform, count / 4,
                                        true, true));
        reports.push_back(timed_stream(*oracle, bench, random, "instructions-fetch-latency", 4, 0, count / 4, true));
        reports.push_back(timed_stream(*oracle, bench, random, "prefix-free-long-fetch-latency", 0, code32 ? 8 : 5,
                                       count / 4, true));
        if (code32) reports.push_back(stallable_fetch_stream(*oracle, bench));
        for (const Report &report : reports) {
            report.print();
            failed |= report.failures != 0;
        }
    }
    return failed ? 1 : 0;
}
