#pragma once

#include "retire_bytes.hpp"
#include "x87_stalls.h"

#include <algorithm>
#include <cstdint>
#include <fstream>
#include <stdexcept>

// One greedy, non-overlapping U/V issue opportunity per adjacent eligible pair.
// The machine bench calls snapshot() at the exact boundaries used in cpu.prof.
class PairWindow {
public:
    void snapshot(std::uint64_t cycle, bool doom) {
        ++snapshots;
        if (doom && snapshots == 3) finish(cycle);
        if (snapshots == (doom ? 2u : 1u)) {
            begin(cycle);
        }
    }

    void begin(std::uint64_t cycle) {
        start = cycle;
        active = true;
        x87.reset();
    }

    template<class Root> void sample(const Root* root) {
        if (active) {
            ++cpu_edges;
            x87.sample(root);
        }
    }
    void park() { if (active) ++parked; }

    void retire(std::uint32_t linear, std::uint64_t tag, bool fault,
                bool segment_change, bool paging, const std::uint8_t (&memory)[15],
                std::uint32_t memory_linear, unsigned memory_length) {
        if (!active) return;
        ++retired;
        if (paging) ++paged;
        if (fault) ++exceptions;
        pentium_pairing::Instruction current{};
        const bool observed = pentium_pairing::from_retire_tag(tag, current);
        if (!fault && !pentium_pairing::has_fetch_tag(tag)) ++untagged;
        if (!fault && !segment_change && observed && !paging) {
            ++crosschecked;
            if (linear != memory_linear || current.length != memory_length ||
                !std::equal(current.bytes.begin(), current.bytes.begin() + std::min<unsigned>(3, current.length), memory))
                ++disagreements;
        }
        if (!observed || fault || segment_change) {
            pending = false;
            return;
        }
        current.boundary_before = linear != expected || previous.default32 != current.default32;
        if (pending && pentium_pairing::can_pair(previous, current)) {
            ++pairs;
            pending = false;
        } else {
            previous = current;
            expected = linear + current.length;
            pending = true;
        }
    }

    void finish(std::uint64_t cycle) {
        if (!active) return;
        end = cycle;
        active = false;
    }

    void write(const char* filename) const {
        if (end <= start || !retired || cpu_edges + parked != end - start)
            throw std::runtime_error("incomplete pairing window or uncovered machine clocks");
        std::ofstream out(filename);
        out << "{\"from_cycle\":" << start << ",\"to_cycle\":" << end
            << ",\"cycles\":" << end - start << ",\"retired\":" << retired
            << ",\"pairs\":" << pairs << ",\"exceptions\":" << exceptions
            << ",\"untagged_nonexception_retirements\":" << untagged
            << ",\"unpaged_memory_crosschecks\":" << crosschecked
            << ",\"unpaged_memory_disagreements\":" << disagreements
            << ",\"paged_retirements\":" << paged << ",\"parked_cycles\":" << parked << ",\"x87\":";
        x87.write_json(out);
        out << "}\n";
        out.close();
        if (!out) throw std::runtime_error("cannot write pairing.json");
    }

private:
    bool active = false, pending = false;
    unsigned snapshots = 0;
    std::uint64_t start = 0, end = 0, cpu_edges = 0, parked = 0;
    std::uint64_t retired = 0, pairs = 0, exceptions = 0, paged = 0;
    std::uint64_t untagged = 0, crosschecked = 0, disagreements = 0;
    std::uint32_t expected = 0;
    pentium_pairing::Instruction previous{};
    PentiumX87Stalls x87;
};


// Keep the complete G5 stream, including pairs crossing the frame boundary.
// Start an independent greedy stream after the first screenshot confirmed as
// rendered. Its first retirement cannot pair with the preceding startup code.
class ReplayPairWindows {
public:
    void snapshot(std::uint64_t cycle, bool doom) {
        full.snapshot(cycle, doom);
        if (!doom && cycle == 1678571583) {
            tail.begin(cycle);
            have_tail = true;
        }
    }
    template<class Root> void sample(const Root* root) {
        full.sample(root);
        if (have_tail) tail.sample(root);
    }
    void park() {
        full.park();
        if (have_tail) tail.park();
    }
    void retire(std::uint32_t linear, std::uint64_t tag, bool fault, bool segment_change,
                bool paging, const std::uint8_t (&memory)[15], std::uint32_t memory_linear, unsigned memory_length) {
        full.retire(linear, tag, fault, segment_change, paging, memory, memory_linear, memory_length);
        if (have_tail)
            tail.retire(linear, tag, fault, segment_change, paging, memory, memory_linear, memory_length);
    }
    void finish(std::uint64_t cycle) {
        full.finish(cycle);
        if (have_tail) tail.finish(cycle);
    }
    void write() const {
        full.write("pairing.json");
        if (have_tail) tail.write("post-first-render.json");
    }
private:
    PairWindow full, tail;
    bool have_tail = false;
};
