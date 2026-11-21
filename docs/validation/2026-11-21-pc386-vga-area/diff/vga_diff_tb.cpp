// SPDX-License-Identifier: MIT
// Differential bench: the base commit's pc386_vga and this branch's on the
// same random traffic, compared every clock (vga_diff_top.sv). Port bytes
// cover every VGA register, with index registers biased to the indices that
// exist; MMIO reads and writes cover every memory map, chain-4, odd/even and
// planar mode, read and write mode and byte-enable pattern the registers
// select. Port 1 answers in order after a random latency with random
// waitrequest; capture sees random waitrequest; reset is pulsed at random.
//
//   vga_diff_tb SEED CLOCKS
#include "Vvga_diff_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <deque>
#include <random>
#include <unordered_map>

namespace {

struct Beat {
    uint64_t due;
    uint64_t data;
};

const char *const kGroup[] = {"none", "io_done/io_rdata", "mem_ready/mem_rvalid", "mem_rdata", "p1_read/p1_write",
                              "p1_address/p1_burstcount", "p1_writedata/p1_byteenable", "cap_write",
                              "cap_address/burstcount/writedata", "frame buffer/size/sequence",
                              "frame strobes/frame_count", "underflows/capture_drops", "", "p1_writedata/p1_byteenable while p1_write is low"};

}  // namespace

int main(int argc, char **argv) {
    if (argc != 3) {
        std::fprintf(stderr, "usage: %s SEED CLOCKS\n", argv[0]);
        return 2;
    }
    const uint64_t seed = std::strtoull(argv[1], nullptr, 0);
    const uint64_t clocks = std::strtoull(argv[2], nullptr, 0);
    std::mt19937_64 rng(seed);
    auto chance = [&](unsigned per_thousand) { return rng() % 1000 < per_thousand; };

    auto *top = new Vvga_diff_top;
    std::unordered_map<uint64_t, uint64_t> vram;
    std::deque<Beat> beats;
    uint64_t now = 0;
    bool io_idle = true, io_busy = false, mem_busy = false, mem_reading = false;
    uint64_t io_count = 0, mem_writes = 0, mem_reads = 0, resets = 0, p1_beats = 0, cap_beats = 0;
    // A mode phase keeps a register setting for a while, so planar traffic
    // runs under each mode rather than under a new one every access.
    static const uint16_t kPorts[] = {0x3c0, 0x3c0, 0x3c1, 0x3c2, 0x3c3, 0x3c4, 0x3c5, 0x3c5, 0x3c6, 0x3c7,
                                      0x3c8, 0x3c9, 0x3c9, 0x3ca, 0x3cc, 0x3ce, 0x3cf, 0x3cf, 0x3cf, 0x3b4,
                                      0x3b5, 0x3ba, 0x3d4, 0x3d5, 0x3d5, 0x3da, 0x3da, 0x3bb, 0x3d0, 0x3df};
    auto index_for = [&](uint16_t port) -> uint8_t {
        switch (port) {
            case 0x3c4: return rng() % 8;
            case 0x3ce: return rng() % 12;
            case 0x3b4: case 0x3d4: return chance(900) ? rng() % 26 : (chance(500) ? 0x22 : rng() & 0x3f);
            case 0x3c0: return chance(900) ? rng() % 0x16 | (chance(800) ? 0x20 : 0) : rng() & 0xff;
            default: return rng() & 0xff;
        }
    };
    uint32_t mem_window = 0;
    uint64_t burst_address = 0;
    unsigned burst_left = 0;

    top->reset = 1;
    for (int i = 0; i < 8; ++i) {
        top->clk = 0; top->eval(); top->clk = 1; top->eval();
    }
    top->reset = 0;

    for (now = 0; now < clocks; ++now) {
        // Inputs for this clock, from the reference's registered outputs.
        top->clk = 0;
        top->reset = now > 1000 && rng() % 200000 == 0;
        if (top->reset) {
            ++resets;
            io_busy = mem_busy = mem_reading = false;
            beats.clear();
            burst_left = 0;
            top->io_valid = 0;
            top->mem_valid = 0;
        }
        // A port byte drops io_valid for a clock between handshakes, as the decoder does.
        if (!io_busy && io_idle && !top->reset && chance(60)) {
            io_busy = true;
            const uint16_t port = kPorts[rng() % (sizeof kPorts / sizeof kPorts[0])];
            top->io_port = port | (chance(20) ? (rng() & 0xfc00) : 0);
            top->io_write = chance(700);
            top->io_wdata = (port == 0x3c3) ? (chance(900) ? 1 : 0) : index_for(port);
            top->io_valid = 1;
            ++io_count;
        }
        if (!mem_busy && !top->reset && chance(250)) {
            mem_busy = true;
            if (chance(5)) mem_window = rng() & 0x7fff;
            const uint32_t span = chance(800) ? 64 : 0x8000;
            top->mem_address = (mem_window + rng() % span) & 0x7fff;
            top->mem_write = chance(650);
            uint8_t enables = rng() & 0xf;
            if (chance(500)) enables = 0xf;
            if (!enables) enables = 1u << (rng() % 4);
            top->mem_byteenable = enables;
            top->mem_wdata = static_cast<uint32_t>(rng());
            top->mem_valid = 1;
            mem_reading = !top->mem_write;
        }
        top->p1_waitrequest = chance(300);
        top->cap_waitrequest = chance(300);
        if (chance(1)) top->present_buffer = rng() % 3;
        top->p1_readdatavalid = 0;
        if (!beats.empty() && beats.front().due <= now && chance(800)) {
            top->p1_readdatavalid = 1;
            top->p1_readdata = beats.front().data;
            beats.pop_front();
            ++p1_beats;
        }
        top->eval();

        // Port 1 requests the reference makes this clock.
        if (!top->reset && !top->p1_waitrequest) {
            if (top->p1_read) {
                uint64_t due = now + 2 + rng() % 12;
                if (!beats.empty() && beats.back().due > due) due = beats.back().due;
                for (unsigned n = 0; n < top->p1_burstcount; ++n) {
                    const uint64_t address = top->p1_address + n;
                    const auto found = vram.find(address);
                    beats.push_back({due + n, found == vram.end() ? address * 0x9e3779b97f4a7c15ull : found->second});
                }
            }
            if (top->p1_write) {
                // One beat per accepted clock; each carries its own address.
                if (burst_left == 0) { burst_address = top->p1_address; burst_left = top->p1_burstcount; }
                uint64_t &word = vram[burst_address];
                for (int lane = 0; lane < 8; ++lane)
                    if (top->p1_byteenable >> lane & 1) {
                        word &= ~(0xffull << (8 * lane));
                        word |= top->p1_writedata & (0xffull << (8 * lane));
                    }
                ++burst_address;
                --burst_left;
            }
        }
        if (top->cap_write && !top->cap_waitrequest) ++cap_beats;
        if (top->diff) {
            std::fprintf(stderr, "FAIL seed %llu clock %llu: %s differs\n", static_cast<unsigned long long>(seed),
                         static_cast<unsigned long long>(now), kGroup[top->diff < 14 ? top->diff : 0]);
            return 1;
        }
        // Handshakes complete on the reference's strobes.
        const bool io_done = top->io_done;
        const bool mem_ready = top->mem_ready, mem_rvalid = top->mem_rvalid;
        top->clk = 1;
        top->eval();
        io_idle = !io_busy;
        if (io_busy && io_done) { io_busy = false; io_idle = false; top->io_valid = 0; }
        if (mem_busy && top->mem_valid && mem_ready) {
            top->mem_valid = 0;
            if (mem_reading) ++mem_reads; else { ++mem_writes; mem_busy = false; }
        }
        if (mem_busy && mem_reading && !top->mem_valid && mem_rvalid) mem_busy = mem_reading = false;
    }
    std::printf("PASS seed %llu: %llu clocks, %llu port bytes, %llu MMIO writes, %llu MMIO reads, %llu port-1 read beats, "
                "%llu capture beats, %llu resets, frames %u\n",
                static_cast<unsigned long long>(seed), static_cast<unsigned long long>(clocks),
                static_cast<unsigned long long>(io_count), static_cast<unsigned long long>(mem_writes),
                static_cast<unsigned long long>(mem_reads), static_cast<unsigned long long>(p1_beats),
                static_cast<unsigned long long>(cap_beats), static_cast<unsigned long long>(resets),
                static_cast<unsigned>(top->frame_count));
    delete top;
    return 0;
}
