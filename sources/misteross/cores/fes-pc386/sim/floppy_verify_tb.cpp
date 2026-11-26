// SPDX-License-Identifier: MIT
// pc386_floppy_verify against a port 2 model (fes.media.pc-floppy's commit
// CRC pass, docs/design/pc386-removable-media.md). The port stalls at random
// and returns read beats after a random latency, in order. The controller
// side keeps issuing 64-beat reads and single-beat writes throughout. Checks:
//   - a pass over A: with the image's CRC matches, over B: with a wrong CRC
//     does not, and each answers on its own drive's done bit;
//   - a pass abandoned when verify falls drains its beats and answers
//     nothing, and a new pass then runs from the start;
//   - the controller's reads return exactly its own words, in order, and
//     its writes reach the memory, while a pass shares the port.
// The window constants are fes_application.vh's and fes_computer.vh's
// (tests/test_pc386_machine.py checks they still match).
#include "Vpc386_floppy_verify.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <deque>
#include <iostream>
#include <map>
#include <random>
#include <string>
#include <vector>

namespace {
constexpr uint64_t WINDOW_BASE = 0x30000000u;
constexpr uint64_t FLOPPY_OFFSET = 0x01800000u, FLOPPY_STRIDE = 0x00400000u;
constexpr uint32_t IMAGE0_WORD = uint32_t((WINDOW_BASE + FLOPPY_OFFSET) >> 3);
constexpr uint32_t DRIVE_WORDS = uint32_t(FLOPPY_STRIDE >> 3);

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "FAIL floppy-verify: " << message << '\n';
    std::exit(1);
}

std::map<uint32_t, uint64_t> written;  // words the controller wrote
uint64_t pattern(uint32_t address) {
    uint64_t z = uint64_t(address) * 0x9e3779b97f4a7c15ull;
    z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9ull;
    return z ^ (z >> 31);
}
uint64_t word_at(uint32_t address) {
    auto it = written.find(address);
    return it == written.end() ? pattern(address) : it->second;
}

uint32_t image_crc(unsigned drive, unsigned sectors) {
    uint32_t c = 0xffffffffu;
    for (uint32_t w = 0; w < sectors * 64u; ++w) {
        uint64_t v = word_at(IMAGE0_WORD + drive * DRIVE_WORDS + w);
        for (int b = 0; b < 8; ++b) {
            c ^= uint8_t(v >> (8 * b));
            for (int i = 0; i < 8; ++i) c = (c & 1) ? (c >> 1) ^ 0xedb88320u : c >> 1;
        }
    }
    return ~c;
}

struct Beat { uint64_t data; int due; };
}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vpc386_floppy_verify dut;
    std::mt19937 random(1694);
    std::deque<Beat> returning;        // read beats owed, in order
    long now = 0;

    // The controller's side: one transaction at a time, as pc386_fdc.
    bool fdc_active = false, fdc_is_read = false;
    uint32_t fdc_address = 0;
    unsigned fdc_owed = 0, fdc_next = 0;
    long fdc_reads = 0, fdc_writes = 0;
    auto fdc_start = [&]() {
        fdc_active = true;
        fdc_is_read = random() % 4 != 0;
        // Anywhere in A:'s or B:'s region, away from the passes' images is
        // not required: a write lands before a pass that reads it starts.
        fdc_address = IMAGE0_WORD + (random() % 2) * DRIVE_WORDS + 64 * (3000 + random() % 100);
        dut.fdc_address = fdc_address;
        dut.fdc_burstcount = fdc_is_read ? 64 : 1;
        dut.fdc_read = fdc_is_read;
        dut.fdc_write = !fdc_is_read;
        dut.fdc_writedata = fdc_is_read ? 0 : (uint64_t(now) << 20 | 0xabcdu);
        dut.fdc_byteenable = 0xff;
    };

    auto tick = [&]() {
        // Port 2: stall at random, take a command, return its beats later.
        dut.port_waitrequest = random() % 3 == 0;
        bool beat = !returning.empty() && returning.front().due <= now;
        dut.port_readdatavalid = beat;
        dut.readdata = beat ? returning.front().data : 0;
        dut.eval();
        const bool taken_read = dut.port_read && !dut.port_waitrequest;
        const bool taken_write = dut.port_write && !dut.port_waitrequest;
        const uint32_t address = dut.port_address;
        const unsigned burst = dut.port_burstcount;
        const uint64_t wdata = dut.port_writedata;
        // The controller's view this clock.
        const bool fdc_taken = (dut.fdc_read || dut.fdc_write) && !dut.fdc_waitrequest;
        if (dut.fdc_readdatavalid) {
            if (!fdc_owed) fail("a beat reached the controller that it did not ask for");
            uint64_t want = word_at(fdc_address + fdc_next);
            if (dut.readdata != want) fail("the controller got another master's beat");
            ++fdc_next;
            if (--fdc_owed == 0) { fdc_active = false; ++fdc_reads; }
        }
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
        if (beat) returning.pop_front();
        int due = int(now) + 2 + int(random() % 4);
        if (taken_read)
            for (unsigned i = 0; i < burst; ++i) returning.push_back({word_at(address + i), due + int(i)});
        if (taken_write) written[address] = wdata;
        if (fdc_taken) {
            dut.fdc_read = 0;
            dut.fdc_write = 0;
            if (fdc_is_read) { fdc_owed = 64; fdc_next = 0; }
            else { fdc_active = false; ++fdc_writes; }
        }
        if (!fdc_active && random() % 50 == 0) fdc_start();
        ++now;
    };

    dut.reset = 1; dut.verify = 0; dut.sectors = 0; dut.crc = 0;
    dut.fdc_read = 0; dut.fdc_write = 0;
    for (int i = 0; i < 8; ++i) tick();
    dut.reset = 0;

    auto run_pass = [&](unsigned drive, unsigned sectors, uint32_t crc, bool expect) {
        uint64_t s = 0, c = 0;
        s = uint64_t(sectors) << (13 * drive);
        c = uint64_t(crc) << (32 * drive);
        dut.sectors = s;
        dut.crc = c;
        dut.verify = 1u << drive;
        for (long i = 0; i < 4000000; ++i) {
            tick();
            if (dut.done & (1u << (drive ^ 1))) fail("the other drive's done bit pulsed");
            if (dut.done & (1u << drive)) {
                if (bool(dut.match) != expect) fail("drive " + std::to_string(drive) + " match " +
                                                    std::to_string(dut.match));
                dut.verify = 0;
                for (int k = 0; k < 4; ++k) tick();
                return;
            }
        }
        fail("no answer from drive " + std::to_string(drive));
    };

    // A:, 1.44M, its own CRC: a match.
    run_pass(0, 2880, image_crc(0, 2880), true);
    // B:, 1.2M, a wrong CRC: no match.
    run_pass(1, 2400, image_crc(1, 2400) ^ 1, false);
    // Abandoned: verify falls partway; nothing answers and the beats drain.
    dut.sectors = 720; dut.crc = 0; dut.verify = 1;
    for (int i = 0; i < 3000; ++i) tick();
    dut.verify = 0;
    for (int i = 0; i < 2000; ++i) {
        tick();
        if (dut.done) fail("an abandoned pass answered");
    }
    // Then the same drive from the start: a match.
    run_pass(0, 720, image_crc(0, 720), true);
    // Let the controller finish what it has in flight.
    for (int i = 0; i < 5000 && (fdc_active || !returning.empty()); ++i) tick();
    if (fdc_reads < 10 || fdc_writes < 3) fail("the controller hardly used the port: " + std::to_string(fdc_reads) +
                                               " reads, " + std::to_string(fdc_writes) + " writes");
    std::cout << "PASS floppy-verify: A: match, B: mismatch, abandon and rerun; controller " << fdc_reads
              << " reads and " << fdc_writes << " writes beside the passes\n";
    return 0;
}
