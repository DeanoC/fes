// SPDX-License-Identifier: GPL-2.0-or-later
// Play PZX images through the production tape deck and compare its EAR
// waveform, T state for T state, with an independent expansion of the
// format. The byte stream stalls at random, as the DDR store's does.
#include "Vspectrum_tape.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <random>
#include <string>
#include <utility>
#include <vector>

#ifndef TAPE_IS_48K
#define TAPE_IS_48K 0
#endif

namespace {
void require(bool condition, const std::string& message) {
    if (!condition) {
        std::cerr << "fes.spectrum tape: " << message << '\n';
        std::exit(EXIT_FAILURE);
    }
}

// ------------------------------------------------------------- PZX writer
struct Pzx {
    std::vector<uint8_t> bytes;
    void u8(unsigned v) { bytes.push_back(uint8_t(v)); }
    void u16(unsigned v) { u8(v); u8(v >> 8); }
    void u32(uint32_t v) { u16(v); u16(v >> 16); }
    size_t open(const char* tag) {
        for (int i = 0; i < 4; ++i) u8(uint8_t(tag[i]));
        u32(0);
        return bytes.size();
    }
    void close(size_t start) {
        const uint32_t size = uint32_t(bytes.size() - start);
        for (int i = 0; i < 4; ++i) bytes[start - 4 + i] = uint8_t(size >> (8 * i));
    }
    void header() { size_t b = open("PZXT"); u8(1); u8(0); for (char c : std::string("fes")) u8(c); close(b); }
    // One PULS entry: count, then duration, as the format's encoder writes it.
    void pulse(std::vector<uint8_t>& out, unsigned count, uint32_t duration) {
        auto w = [&](unsigned v) { out.push_back(uint8_t(v)); out.push_back(uint8_t(v >> 8)); };
        if (count > 1 || duration > 0x7fff) w(0x8000 | count);
        if (duration > 0x7fff) { w(0x8000 | (duration >> 16)); w(duration & 0xffff); }
        else w(duration);
    }
    void raw_puls(const std::vector<unsigned>& words) {
        size_t b = open("PULS");
        for (unsigned w : words) u16(w);
        close(b);
    }
    void puls(const std::vector<std::pair<unsigned, uint32_t>>& entries) {
        size_t b = open("PULS");
        for (auto [count, duration] : entries) pulse(bytes, count, duration);
        close(b);
    }
    void data(uint32_t bits, bool level, unsigned tail, const std::vector<unsigned>& s0,
              const std::vector<unsigned>& s1, const std::vector<uint8_t>& payload) {
        size_t b = open("DATA");
        u32(bits | (level ? 0x80000000u : 0));
        u16(tail); u8(s0.size()); u8(s1.size());
        for (unsigned v : s0) u16(v);
        for (unsigned v : s1) u16(v);
        for (size_t i = 0; i < (bits + 7) / 8; ++i) u8(payload[i]);
        close(b);
    }
    void paus(uint32_t duration, bool level) { size_t b = open("PAUS"); u32(duration | (level ? 0x80000000u : 0)); close(b); }
    void stop(unsigned flags) { size_t b = open("STOP"); u16(flags); close(b); }
    void other(const char* tag, size_t length) { size_t b = open(tag); for (size_t i = 0; i < length; ++i) u8(i * 7); close(b); }
};

// --------------------------------------------------------- reference model
// The image as runs of one level, in T states. A STOP that applies to this
// model ends the list; the caller restarts it for the next part.
struct Run { bool level; uint64_t length; };
struct Expansion { std::vector<Run> runs; bool stopped = false; size_t resume = 0; };

uint32_t le(const std::vector<uint8_t>& b, size_t at, int n) {
    uint32_t v = 0;
    for (int i = 0; i < n; ++i) v |= uint32_t(b[at + i]) << (8 * i);
    return v;
}

Expansion expand(const std::vector<uint8_t>& b, size_t from, bool initial) {
    Expansion e;
    bool level = initial;
    auto emit = [&](uint64_t duration) {
        if (duration == 0) { level = !level; return; }
        if (!e.runs.empty() && e.runs.back().level == level) e.runs.back().length += duration;
        else e.runs.push_back({level, duration});
        level = !level;
    };
    size_t at = from;
    while (at + 8 <= b.size()) {
        const std::string tag(b.begin() + at, b.begin() + at + 4);
        const uint32_t size = le(b, at + 4, 4);
        const size_t body = at + 8, end = body + size;
        if (end > b.size()) break;
        if (tag == "PULS") {
            level = false;
            size_t p = body;
            while (p + 2 <= end) {
                uint32_t count = 1, duration = le(b, p, 2); p += 2;
                if (duration > 0x8000) { count = duration & 0x7fff; duration = le(b, p, 2); p += 2; }
                if (duration >= 0x8000) { duration = ((duration & 0x7fff) << 16) | le(b, p, 2); p += 2; }
                for (uint32_t i = 0; i < count; ++i) emit(duration);
            }
        } else if (tag == "DATA") {
            const uint32_t word = le(b, body, 4), bits = word & 0x7fffffff;
            level = word >> 31;
            const unsigned tail = le(b, body + 4, 2), p0 = b[body + 6], p1 = b[body + 7];
            std::vector<unsigned> s0, s1;
            for (unsigned i = 0; i < p0; ++i) s0.push_back(le(b, body + 8 + 2 * i, 2));
            for (unsigned i = 0; i < p1; ++i) s1.push_back(le(b, body + 8 + 2 * (p0 + i), 2));
            const size_t payload = body + 8 + 2 * (p0 + p1);
            for (uint32_t i = 0; i < bits; ++i) {
                const bool bit = (b[payload + i / 8] >> (7 - i % 8)) & 1;
                for (unsigned d : bit ? s1 : s0) emit(d);
            }
            if (tail) emit(tail);
        } else if (tag == "PAUS") {
            const uint32_t word = le(b, body, 4);
            level = word >> 31;
            emit(word & 0x7fffffff);
        } else if (tag == "STOP") {
            const unsigned flags = le(b, body, 2);
            if (flags != 1 || TAPE_IS_48K) { e.stopped = true; e.resume = end; return e; }
        }
        at = end;
    }
    return e;
}

// ------------------------------------------------------------------ bench
struct Bench {
    Vspectrum_tape dut;
    std::vector<uint8_t> image;
    size_t stream_pos = 0;
    std::mt19937 rng{982};
    unsigned clocks_per_tick = 15;
    unsigned stall_percent = 30;
    unsigned phase = 0;
    uint64_t t = 0;      // T states since construction
    uint64_t last_read = 0;
    uint8_t b = 0;

    Bench() {
        dut.clk = 0; dut.reset = 1; dut.tick = 0; dut.ready = 0; dut.size = 0;
        dut.stream_valid = 0; dut.stream_data = 0; dut.ula_read = 0; dut.cpu_b = 0;
        dut.eval();
        for (int i = 0; i < 4; ++i) clock();
        dut.reset = 0;
    }
    void clock() {
        // The store presents the next byte unless it stalls this clock.
        const bool enabled = dut.stream_enable;
        if (!enabled) stream_pos = 0;
        const bool valid = enabled && stream_pos < image.size() && (rng() % 100) >= stall_percent;
        dut.stream_valid = valid;
        dut.stream_data = valid ? image[stream_pos] : 0;
        dut.eval();
        const bool took = valid && dut.stream_take;
        dut.clk = 1; dut.eval();
        dut.clk = 0; dut.eval();
        if (took) ++stream_pos;
        dut.ula_read = 0;
    }
    // One T state: clocks_per_tick clocks, the tick on the first. Returns EAR
    // after the T-state edge.
    bool step() {
        dut.tick = 1;
        clock();
        dut.tick = 0;
        const bool level = dut.ear;
        for (unsigned i = 1; i < clocks_per_tick; ++i) clock();
        ++t;
        return level;
    }
    void insert(const std::vector<uint8_t>& pzx) {
        dut.ready = 0;
        for (int i = 0; i < 4; ++i) clock();
        image = pzx;
        dut.size = image.size();
        dut.ready = 1;
        for (int i = 0; i < 8; ++i) clock();
    }
    void eject() { dut.ready = 0; for (int i = 0; i < 4; ++i) clock(); }
    // A read of a ULA port by the CPU, `gap` T states after the previous one.
    void read_port(unsigned gap, uint8_t b_now) {
        for (unsigned i = 0; i < gap; ++i) step();
        dut.cpu_b = b_now;
        dut.ula_read = 1;
        clock();
    }
    // The ROM edge loop: reads 59 T apart with B counting up.
    void loader(unsigned reads) {
        for (unsigned i = 0; i < reads; ++i) read_port(59, ++b);
    }
    // A slow read clears the count, as the first read after reset only sets
    // the baseline; then exactly ten loader reads start the tape.
    void start() {
        require(!dut.playing, "tape already playing");
        read_port(600, b);
        loader(9);
        require(!dut.playing, "nine loader reads started the tape");
        loader(1);
        require(dut.playing, "ten loader reads did not start the tape");
    }
    // Record EAR runs until the tape stops, at most `limit` T states.
    std::vector<Run> record(uint64_t limit) {
        std::vector<Run> runs;
        for (uint64_t i = 0; i < limit && dut.playing; ++i) {
            const bool level = step();
            if (!runs.empty() && runs.back().level == level) ++runs.back().length;
            else runs.push_back({level, 1});
        }
        require(!dut.playing, "tape still playing after " + std::to_string(limit) + " T states");
        return runs;
    }
};

uint64_t total(const std::vector<Run>& runs) {
    uint64_t sum = 0;
    for (const Run& r : runs) sum += r.length;
    return sum;
}

// Play starts within a T state, so the deck's first run is the expansion's or
// one T state short of it; after the last edge it holds the final level until
// the stop. Every run between must match exactly.
void compare(const std::vector<Run>& seen, const Expansion& want, const std::string& label) {
    const size_t n = want.runs.size();
    require(n >= 2, label + ": reference has no edges");
    if (seen.size() != n + 1) {
        size_t i = 1;
        while (i < n && i < seen.size() && seen[i].level == want.runs[i].level &&
               seen[i].length == want.runs[i].length) ++i;
        for (size_t j = i > 6 ? i - 6 : 0; j < i + 6; ++j)
            std::cerr << j << ": seen " << (j < seen.size() ? std::to_string(seen[j].level) + "/" +
                std::to_string(seen[j].length) : "-") << "  want " << (j < n ? std::to_string(want.runs[j].level) +
                "/" + std::to_string(want.runs[j].length) : "-") << '\n';
        require(false, label + ": recorded " + std::to_string(seen.size()) + " runs, expected " +
                std::to_string(n + 1) + "; first difference at run " + std::to_string(i) + ": " +
                (i < seen.size() ? std::to_string(seen[i].length) : std::string("none")) + " T against " +
                (i < n ? std::to_string(want.runs[i].length) : std::string("none")) + " T");
    }
    require(seen[0].level == want.runs[0].level &&
            (seen[0].length == want.runs[0].length || seen[0].length + 1 == want.runs[0].length),
            label + ": first run is " + std::to_string(seen[0].length) + " T, expected " +
            std::to_string(want.runs[0].length) + " or one less");
    for (size_t i = 1; i < n; ++i) {
        const Run& a = seen[i];
        const Run& w = want.runs[i];
        if (a.level != w.level || a.length != w.length)
            require(false, label + ": run " + std::to_string(i) + " is " + (a.level ? "high " : "low ") +
                    std::to_string(a.length) + " T, expected " + (w.level ? "high " : "low ") +
                    std::to_string(w.length));
    }
    require(seen[n].level != want.runs[n - 1].level, label + ": no final edge");
}

void play_and_compare(Bench& bench, const std::vector<uint8_t>& image, const std::string& label,
                      uint64_t limit) {
    bench.insert(image);
    bench.start();
    const auto seen = bench.record(limit);
    compare(seen, expand(image, 0, false), label);
    require(bench.dut.underruns == 0, label + ": " + std::to_string(bench.dut.underruns) + " late pulses");
}

std::vector<uint8_t> bytes(size_t n, unsigned seed) {
    std::vector<uint8_t> v(n);
    std::mt19937 r(seed);
    for (auto& x : v) x = uint8_t(r());
    return v;
}

// A ROM-format block as PULS + DATA + PAUS, with a shortened pilot.
void rom_block(Pzx& p, const std::vector<uint8_t>& payload, unsigned pilots) {
    p.puls({{pilots, 2168}, {1, 667}, {1, 735}});
    p.data(payload.size() * 8, true, 945, {855, 855}, {1710, 1710}, payload);
    p.paus(3500, false);
}
}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);

    {
        // Standard header and data blocks, the header with its full pilot.
        Bench bench;
        bench.clocks_per_tick = 3;
        Pzx p;
        p.header();
        std::vector<uint8_t> header = {0x00, 0x03, 'F', 'E', 'S', ' ', ' ', ' ', ' ', ' ', ' ', ' ',
                                       0x10, 0x00, 0x00, 0x40, 0x00, 0x80, 0x00};
        uint8_t check = 0;
        for (uint8_t x : header) check ^= x;
        header.push_back(check);
        p.puls({{8063, 2168}, {1, 667}, {1, 735}});
        p.data(header.size() * 8, true, 945, {855, 855}, {1710, 1710}, header);
        p.paus(3500000, false);
        auto data = bytes(300, 1);
        data[0] = 0xff;
        rom_block(p, data, 3223);
        play_and_compare(bench, p.bytes, "ROM blocks", 40000000);
    }
    {
        // PULS encodings: repeat counts, 32-bit durations, zero pulses with
        // odd and even repeat counts, the 0x8000 extended-duration prefix.
        Bench bench;
        Pzx p;
        p.header();
        p.puls({{1, 300}, {3, 120}, {1, 0}, {1, 250}, {4, 0}, {1, 90}, {2, 0}, {5, 0},
                {1, 100000}, {7, 40000}, {1, 0x7fff}, {1, 0x8000}, {1, 33}, {32767, 2}});
        p.puls({{1, 0}, {1, 500}, {1, 600}});
        // 0x8000 without a count: one pulse of the following 16 bits.
        p.raw_puls({0x8000, 0x9000, 0x8003, 0x8000, 0x8100, 0x0400, 0x8002, 0x8001, 0x0002});
        play_and_compare(bench, p.bytes, "PULS encodings", 2000000);
    }
    {
        // DATA: ZX81 sequences, a partial last byte, zero-length entries as
        // in direct recordings, both initial levels, with and without tail.
        Bench bench;
        Pzx p;
        p.header();
        p.data(13, true, 0, {530, 520, 530, 520, 530, 520, 530, 4689},
               {530, 520, 530, 520, 530, 520, 530, 520, 530, 520, 530, 520, 530, 520, 530, 520, 530, 4689},
               {0xa5, 0x38});
        p.data(64, false, 300, {79, 0}, {0, 79}, bytes(8, 2));
        p.data(24, true, 945, {855, 855}, {1710, 1710}, bytes(3, 3));
        p.data(9, false, 17, {200}, {400, 0, 100}, {0x80, 0xff});
        p.data(0, true, 700, {855, 855}, {1710, 1710}, {});
        p.paus(1000, true);
        p.paus(2000, false);
        play_and_compare(bench, p.bytes, "DATA blocks", 3000000);
    }
    {
        // Every pulse table entry, up to 255 per bit.
        Bench bench;
        Pzx p;
        p.header();
        std::vector<unsigned> s0, s1;
        for (unsigned i = 0; i < 255; ++i) { s0.push_back(20 + i); s1.push_back(i % 3 ? 300 - i : 0); }
        p.data(16, true, 50, s0, s1, {0x5a, 0xc3});
        play_and_compare(bench, p.bytes, "long sequences", 3000000);
    }
    {
        // Unknown and browse blocks are skipped; a custom block between
        // two data blocks leaves the timing untouched.
        Bench bench;
        Pzx p;
        p.header();
        p.other("BRWS", 20);
        rom_block(p, bytes(40, 4), 200);
        p.other("xyzw", 1000);
        p.other("BRWS", 0);
        rom_block(p, bytes(40, 5), 200);
        play_and_compare(bench, p.bytes, "skipped blocks", 4000000);
    }
    {
        // STOP: flag 0 always stops; flag 1 stops only the 48K. The tape
        // waits until the loader starts it again, then continues.
        Bench bench;
        Pzx p;
        p.header();
        rom_block(p, bytes(20, 6), 100);
        const size_t first_stop = p.bytes.size();
        p.stop(1);
        rom_block(p, bytes(20, 7), 100);
        const size_t second_stop = p.bytes.size();
        p.stop(0);
        rom_block(p, bytes(20, 8), 100);
        bench.insert(p.bytes);
        bench.start();
        auto seen = bench.record(2000000);
        const Expansion part1 = expand(p.bytes, 0, false);
        require(part1.stopped, "reference did not stop");
        compare(seen, part1, "before STOP");
        require(part1.resume == (TAPE_IS_48K ? first_stop + 10 : second_stop + 10), "STOP flag handling");
        bench.start();
        seen = bench.record(2000000);
        const Expansion part2 = expand(p.bytes, part1.resume, false);
        compare(seen, part2, "after STOP");
    }
    {
        // The end of the tape stops and rewinds it: started again, it plays
        // from the first block.
        Bench bench;
        Pzx p;
        p.header();
        rom_block(p, bytes(16, 9), 120);
        bench.insert(p.bytes);
        bench.start();
        auto first = bench.record(2000000);
        bench.start();
        auto second = bench.record(2000000);
        require(first.size() == second.size(), "rewound tape played differently");
        for (size_t i = 1; i + 1 < first.size(); ++i)
            require(first[i].level == second[i].level && first[i].length == second[i].length,
                    "rewound tape run " + std::to_string(i) + " differs");
    }
    {
        // Loader detection: reads more than 500 T apart, or with B steady,
        // never start the tape; two successive non-loader reads stop it, one
        // does not.
        Bench bench;
        Pzx p;
        p.header();
        p.puls({{30000, 1000}});
        bench.insert(p.bytes);
        for (int i = 0; i < 30; ++i) bench.read_port(501, ++bench.b);
        require(!bench.dut.playing, "slow reads started the tape");
        for (int i = 0; i < 30; ++i) bench.read_port(100, bench.b);
        require(!bench.dut.playing, "reads with B unchanged started the tape");
        for (int i = 0; i < 9; ++i) bench.read_port(100, --bench.b);
        bench.read_port(100, bench.b + 2);
        bench.loader(9);
        require(!bench.dut.playing, "a B jump did not restart the count");
        bench.loader(1);
        require(bench.dut.playing, "B counting down and up did not start the tape");
        bench.read_port(1001, bench.b);
        require(bench.dut.playing, "one slow read stopped the tape");
        bench.read_port(10, bench.b);
        require(bench.dut.playing, "a fast read after a slow one stopped the tape");
        bench.read_port(10, bench.b + 5);
        require(bench.dut.playing, "one B jump stopped the tape");
        bench.read_port(1001, bench.b);
        require(!bench.dut.playing, "two successive non-loader reads did not stop the tape");
        // Ejecting mid-pulse drops EAR; the reinserted tape starts over.
        bench.start();
        for (int i = 0; i < 2500; ++i) bench.step();
        bench.eject();
        require(!bench.dut.ear && !bench.dut.playing, "eject left the tape running");
    }
    {
        // A heavily stalled stream still never delays a pulse.
        Bench bench;
        bench.stall_percent = 70;
        Pzx p;
        p.header();
        for (int i = 0; i < 6; ++i) p.puls({{1, 40}, {1, 35}, {1, 0}, {1, 45}, {2, 50}});
        p.data(256, true, 0, {79, 0}, {0, 79}, bytes(32, 10));
        play_and_compare(bench, p.bytes, "stalled stream", 1000000);
    }
    std::cout << "fes.spectrum tape: PZX deck matches the reference expansion"
              << (TAPE_IS_48K ? " (48K STOP)" : "") << '\n';
    return 0;
}
