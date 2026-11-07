// SPDX-License-Identifier: MIT
// pc386 EMS board bench: rtl/pc386_ems.sv on pc386_ddr_model.h's port 2.
//
// Directed cases, then a random run against a reference board: page
// registers written at random (in and out of range), frame reads and writes
// with random byte enables, each read compared with the reference, and the
// DDR window compared byte for byte at the end. The window outside the
// board's 4 MiB must never change.
//
// Prints PASS/FAIL per case; exit 0 when every case passes.
#include "Vpc386_ems.h"
#include "pc386_ddr_model.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

namespace {

constexpr uint32_t BOARD_OFFSET = 0x01400000u;  // window offset of board page 0
constexpr uint32_t BOARD_BYTES = 0x00400000u;   // reserved: 256 pages

class Bench {
public:
    explicit Bench(unsigned pages) {
        top.pages = uint8_t(pages);
        top.reset = 1;
        for (int n = 0; n < 4; ++n) step();
        top.reset = 0;
        step();
    }
    Vpc386_ems top;
    pc386::Window window;
    pc386::Port ddr{window, 0x5eed00e5, "port 2"};
    uint8_t burstcount = 1;
    uint64_t cycles = 0;

    void step() {
        pc386::PortSignals port{&top.p2_address, &burstcount, &top.p2_read, &top.p2_write, &top.p2_writedata,
                                &top.p2_byteenable, &top.p2_waitrequest, &top.p2_readdata,
                                &top.p2_readdatavalid};
        top.clk = 0;
        top.eval();
        ddr.drive(port, cycles++);
        top.clk = 1;
        top.eval();
        top.reg_write = 0;
        top.mem_valid = 0;
    }
    void set_register(unsigned index, uint8_t value) {
        top.reg_write = 1;
        top.reg_index = index;
        top.reg_wdata = value;
        step();
    }
    // Present an access until it is taken; a read then waits for its answer.
    uint32_t access(bool write, uint32_t offset, uint8_t enables, uint32_t value = 0) {
        uint64_t start = cycles;
        for (;;) {
            top.mem_valid = 1;
            top.mem_write = write;
            top.mem_address = (offset & 0xffff) >> 2;
            top.mem_byteenable = enables;
            top.mem_wdata = value;
            top.clk = 0;
            top.eval();
            const bool taken = top.mem_ready;
            step();
            if (taken) break;
            if (cycles - start > 10000) throw std::runtime_error("frame access not taken");
        }
        if (write) return 0;
        while (!top.mem_rvalid) {
            step();
            if (cycles - start > 10000) throw std::runtime_error("frame read not answered");
        }
        return top.mem_rdata;
    }
    void drain() {
        for (int n = 0; n < 200 || !ddr.idle() || !top.mem_ready; ++n) step();
    }
    uint8_t board(uint32_t page, uint32_t at) { return window.byte(BOARD_OFFSET + page * 0x4000 + at); }
};

int failures = 0;
void check(bool pass, const std::string &what) {
    std::printf("%s %s\n", pass ? "PASS" : "FAIL", what.c_str());
    if (!pass) ++failures;
}

// A window byte outside the board region, as a sum, to show nothing else changed.
uint64_t outside_sum(pc386::Window &window) {
    uint64_t sum = 0;
    for (uint32_t at = BOARD_OFFSET - 0x10000; at < BOARD_OFFSET; at += 4) sum += window.byte(at);
    for (uint32_t at = BOARD_OFFSET + BOARD_BYTES; at < BOARD_OFFSET + BOARD_BYTES + 0x10000; at += 4)
        sum += window.byte(at);
    return sum;
}

void directed() {
    Bench b(128);
    // After reset every register is FFh: the frame is unmapped.
    bool unmapped = true;
    for (uint32_t page = 0; page < 4; ++page)
        unmapped &= b.access(false, page * 0x4000 + 0x123c, 0xf) == 0xffffffffu;
    b.access(true, 0x0000, 0xf, 0xdeadbeef);
    b.drain();
    check(unmapped && b.ddr.commands == 0, "reset: the four frame pages read FFh and send nothing to DDR");

    // Register 0 to board page 5: a dword, then bytes in the high half of a beat.
    b.set_register(0, 5);
    b.access(true, 0x0010, 0xf, 0x11223344);
    b.access(true, 0x0014, 0x6, 0xaabbccdd);
    b.drain();
    check(b.board(5, 0x10) == 0x44 && b.board(5, 0x13) == 0x11 && b.board(5, 0x14) == 0x00 &&
              b.board(5, 0x15) == 0xcc && b.board(5, 0x16) == 0xbb && b.board(5, 0x17) == 0x00,
          "writes land at board page 5 with their byte enables, in either half of a beat");
    check(b.access(false, 0x0010, 0xf) == 0x11223344 && b.access(false, 0x0014, 0xf) == 0x00bbcc00,
          "reads return the low and high dwords of a beat");

    // Two frame pages on one board page alias.
    b.set_register(2, 5);
    check(b.access(false, 0x8010, 0xf) == 0x11223344, "frame pages 0 and 2 on board page 5 alias");

    // The last page is mapped; the page count and above are not.
    b.set_register(1, 127);
    b.access(true, 0x7ffc, 0xf, 0x55667788);
    b.drain();
    check(b.board(127, 0x3ffc) == 0x88 && b.access(false, 0x7ffc, 0xf) == 0x55667788,
          "board page 127, the last of 128, is mapped");
    const uint64_t before = b.ddr.commands;
    b.set_register(3, 128);
    b.access(true, 0xc000, 0xf, 0x01020304);
    const uint32_t value = b.access(false, 0xc000, 0xf);
    b.drain();
    check(value == 0xffffffffu && b.ddr.commands == before && b.board(128, 0) == 0,
          "board page 128 of a 128-page board is unmapped: FFh, write dropped");

    // A page-register write right after a posted write never moves it.
    b.set_register(0, 9);
    b.access(true, 0x0100, 0xf, 0xcafef00d);
    b.set_register(0, 10);
    b.drain();
    check(b.board(9, 0x100) == 0x0d && b.board(10, 0x100) == 0x00,
          "a write taken before a page-register write lands on the old page");

    // Reset unmaps again.
    b.top.reset = 1;
    b.step();
    b.top.reset = 0;
    b.step();
    check(b.access(false, 0x0010, 0xf) == 0xffffffffu, "machine reset returns every register to FFh");
}

void not_fitted() {
    Bench b(0);
    for (unsigned n = 0; n < 4; ++n) b.set_register(n, uint8_t(n));
    b.access(true, 0x0000, 0xf, 0x12345678);
    const uint32_t value = b.access(false, 0x0000, 0xf);
    b.drain();
    check(value == 0xffffffffu && b.ddr.commands == 0, "no board (0 pages): every page unmapped");
}

void random_run(unsigned pages, uint64_t seed, unsigned accesses) {
    Bench b(pages);
    pc386::Random random(seed);
    std::vector<uint8_t> model(BOARD_BYTES, 0);
    std::array<unsigned, 4> reg{0xff, 0xff, 0xff, 0xff};
    const uint64_t outside = outside_sum(b.window);
    unsigned mismatches = 0, reads = 0, writes = 0, unmapped = 0;
    for (unsigned n = 0; n < accesses; ++n) {
        const uint32_t draw = random.next();
        if ((draw & 15) == 0) {
            // Mostly in range; sometimes at or past the end, or FFh.
            const uint32_t pick = random.next();
            const unsigned value = (pick & 7) == 0 ? 0xff : (pick & 7) == 1 ? pages + (pick >> 8) % (256 - pages)
                                                                            : (pick >> 8) % (pages ? pages : 1);
            const unsigned index = (pick >> 4) & 3;
            reg[index] = value & 0xff;
            b.set_register(index, uint8_t(value));
            continue;
        }
        const uint32_t offset = random.next() & 0xfffc;
        const unsigned frame = offset >> 14;
        const bool mapped = reg[frame] < pages;
        const uint32_t at = reg[frame] * 0x4000u + (offset & 0x3fff);
        if (draw & 16) {
            uint8_t enables = uint8_t(random.next() & 15);
            if (!enables) enables = 15;
            const uint32_t value = random.next();
            b.access(true, offset, enables, value);
            ++writes;
            if (mapped)
                for (int lane = 0; lane < 4; ++lane)
                    if (enables >> lane & 1) model[at + lane] = uint8_t(value >> (8 * lane));
        } else {
            const uint32_t got = b.access(false, offset, 0xf);
            uint32_t want = 0xffffffffu;
            if (mapped) std::memcpy(&want, &model[at], 4);
            else ++unmapped;
            ++reads;
            if (got != want && mismatches++ < 4)
                std::printf("  read %04x (frame page %u on %u): %08x, want %08x\n", offset, frame, reg[frame], got,
                            want);
        }
    }
    b.drain();
    unsigned wrong = 0;
    for (uint32_t at = 0; at < BOARD_BYTES; ++at)
        if (b.window.byte(BOARD_OFFSET + at) != model[at]) ++wrong;
    char line[200];
    std::snprintf(line, sizeof line,
                  "random, %u pages: %u reads (%u unmapped) and %u writes match the reference; "
                  "board memory matches; nothing outside it written",
                  pages, reads, unmapped, writes);
    check(mismatches == 0 && wrong == 0 && outside_sum(b.window) == outside, line);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    try {
        directed();
        not_fitted();
        random_run(128, 0x0e350001, 40000);
        random_run(255, 0x0e350002, 40000);
    } catch (const std::exception &error) {
        std::printf("FAIL %s\n", error.what());
        return 1;
    }
    return failures ? 1 : 0;
}
