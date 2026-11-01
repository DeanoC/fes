// SPDX-License-Identifier: GPL-2.0-or-later
// Boot cores/fes-zx81/diagnostic/firmware.py on the ZX81 machine: RAMTOP,
// every keyboard row, a captured frame against the display file and glyphs,
// mailbox LOAD with and without a tape, and SAVE of the loaded .p.
// Built with -DDIAGNOSTIC_EXPANSION it runs expansion_machine instead, which
// has no SAVE port. Usage: <firmware.hex> <expected RAMTOP>.
#ifdef DIAGNOSTIC_EXPANSION
#include "Vexpansion_machine.h"
using Machine = Vexpansion_machine;
constexpr unsigned kPeekTicks = 3;  // the cart's read crosses both bus registers
#else
#include "Vzx81_machine.h"
using Machine = Vzx81_machine;
constexpr unsigned kPeekTicks = 1;
#endif
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <string>
#include <vector>

namespace {

// Addresses the firmware docstring names.
constexpr uint16_t kRamtop = 0x4004, kDFilePtr = 0x400c, kTape = 0x4009;
constexpr uint16_t kReady = 0x4040, kFrames = 0x4041, kLoads = 0x4042,
                   kSaves = 0x4043, kErrors = 0x4044, kKeys = 0x4048;
constexpr uint16_t kDFile = 0x4080, kFont = 0x1e00;
constexpr unsigned kRows = 24, kColumns = 32;
constexpr uint64_t kAllUp = 0xffffffffffull;
constexpr uint64_t kFrameTicks = 1100000;  // 64584 T at 16 host clocks a T

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES ZX81 diagnostic: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

std::vector<uint8_t> load_hex(const std::string &path) {
    std::ifstream input(path);
    require(bool(input), "cannot open " + path);
    std::vector<uint8_t> bytes;
    std::string line;
    while (std::getline(input, line))
        if (!line.empty())
            bytes.push_back(uint8_t(std::stoul(line, nullptr, 16)));
    require(bytes.size() == 8192, "firmware is not 8 KiB");
    return bytes;
}

// Pixels by (line since VSYNC, 6.5 MHz tick since HSYNC) inside the active area.
struct Raster {
    static constexpr unsigned kLines = 330, kWidth = 420;
    bool on = false, hsync = false, vsync = false;
    unsigned frames = 0, y = 0, x = 0, halts = 0;
    std::vector<uint8_t> pixel = std::vector<uint8_t>(kLines * kWidth);
};
Raster raster;
const std::vector<uint8_t> *tape;

void tick(Machine &dut) {
    if (tape)
        dut.tape_data = (*tape)[dut.tape_addr_out < tape->size() ? dut.tape_addr_out : 0];
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
    if (!raster.on)
        return;
    raster.halts += !dut.halt_n;
    if (dut.vsync_out && !raster.vsync) {
        raster.frames++;
        raster.y = 0;
    }
    if (dut.hsync_out && !raster.hsync) {
        raster.y++;
        raster.x = 0;
    }
    raster.vsync = dut.vsync_out;
    raster.hsync = dut.hsync_out;
    if (dut.ce_6m5) {
        if (raster.frames == 1 && !dut.hblank && !dut.vblank && dut.video_pixel &&
            raster.y < Raster::kLines && raster.x < Raster::kWidth)
            raster.pixel[raster.y * Raster::kWidth + raster.x] = 1;
        raster.x++;
    }
}

uint8_t peek(Machine &dut, uint16_t addr) {
    dut.peek_addr = addr;
    dut.eval();
    // Port B q_b is registered, so the byte is valid after this edge.
    for (unsigned i = 0; i != kPeekTicks; ++i) tick(dut);
    return uint8_t(dut.peek_data);
}

uint16_t word_at(Machine &dut, uint16_t addr) {
    return uint16_t(peek(dut, addr)) | uint16_t(peek(dut, addr + 1)) << 8;
}

uint16_t cell(unsigned row, unsigned column) {
    return kDFile + 1 + row * (kColumns + 1) + column;
}

uint8_t hex_digit(unsigned nibble) { return uint8_t(0x1c + (nibble & 15)); }

uint64_t key(unsigned row, unsigned bit) { return kAllUp & ~(1ull << (row * 5 + bit)); }

void run(Machine &dut, uint64_t ticks) {
    for (uint64_t i = 0; i != ticks; ++i) tick(dut);
}

// Wait until the firmware has drawn n more frames.
void frames(Machine &dut, unsigned n, const std::string &what) {
    const uint8_t start = peek(dut, kFrames);
    for (uint64_t i = 0; i < (n + 2) * kFrameTicks; i += 0x10000) {
        run(dut, 0x10000);
        if (uint8_t(peek(dut, kFrames) - start) >= n)
            return;
    }
    fail(what + ": frame counter stopped at " + std::to_string(peek(dut, kFrames)));
}

void wait_byte(Machine &dut, uint16_t addr, uint8_t value, uint64_t ticks,
               const std::string &what) {
    for (uint64_t i = 0; i < ticks; i += 0x10000) {
        if (peek(dut, addr) == value)
            return;
        run(dut, 0x10000);
    }
    fail(what + ": $" + std::to_string(addr) + " is " + std::to_string(peek(dut, addr)));
}

// Capture one whole frame and match it to the display file drawn from the
// firmware's glyphs. The inverse space at row 0, column 0 marks the origin.
void check_frame(Machine &dut, const std::vector<uint8_t> &rom, const char *tag) {
    raster = Raster();
    raster.on = true;
    while (raster.frames == 0) tick(dut);
    // The display file is stable from VSYNC until the next bottom border.
    std::vector<uint8_t> codes(kRows * kColumns);
    for (unsigned row = 0; row != kRows; ++row)
        for (unsigned column = 0; column != kColumns; ++column)
            codes[row * kColumns + column] = peek(dut, cell(row, column));
    while (raster.frames == 1) tick(dut);
    raster.on = false;
    require(raster.halts > 0, std::string(tag) + ": CPU never halted at a row end");

    unsigned oy = 0, ox = 0, lit = 0;
    bool found = false;
    for (unsigned y = 0; y != Raster::kLines && !found; ++y)
        for (unsigned x = 0; x != Raster::kWidth; ++x)
            if (raster.pixel[y * Raster::kWidth + x]) {
                oy = y, ox = x, found = true;
                break;
            }
    require(found, std::string(tag) + ": ULA produced no visible pixels");
    require(oy + 8 * kRows <= Raster::kLines && ox + 8 * kColumns <= Raster::kWidth,
            std::string(tag) + ": display does not fit the active area");
    unsigned wrong = 0;
    for (unsigned y = 0; y != Raster::kLines; ++y)
        for (unsigned x = 0; x != Raster::kWidth; ++x) {
            const bool got = raster.pixel[y * Raster::kWidth + x];
            lit += got;
            bool want = false;
            if (y >= oy && y < oy + 8 * kRows && x >= ox && x < ox + 8 * kColumns) {
                const unsigned row = (y - oy) / 8, line = (y - oy) % 8;
                const unsigned column = (x - ox) / 8, bit = (x - ox) % 8;
                const uint8_t code = codes[row * kColumns + column];
                want = ((rom[kFont + (code & 0x3f) * 8 + line] >> (7 - bit)) & 1) ^ (code >> 7);
            }
            if (got != want && wrong++ < 8)
                std::cerr << tag << ": pixel line " << y - oy << " x " << int(x) - int(ox)
                          << " is " << got << '\n';
        }
    require(wrong == 0, std::string(tag) + ": " + std::to_string(wrong) + " pixels differ");
    std::cout << "FES ZX81 diagnostic: " << tag << " frame matches (" << lit
              << " pixels at line " << oy << ", tick " << ox << ")\n";
}

void check_cells(Machine &dut, unsigned row, unsigned column, const std::vector<uint8_t> &want,
                 const std::string &what) {
    for (unsigned i = 0; i != want.size(); ++i)
        require(peek(dut, cell(row, column + i)) == want[i], what + " cell " + std::to_string(i));
}

std::vector<uint8_t> hex_cells(const std::vector<uint8_t> &bytes) {
    std::vector<uint8_t> out;
    for (uint8_t byte : bytes) {
        out.push_back(hex_digit(byte >> 4));
        out.push_back(hex_digit(byte));
    }
    return out;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 3, "usage: <firmware.hex> <expected RAMTOP>");
    const std::vector<uint8_t> rom = load_hex(argv[1]);
    const unsigned expected_top = std::strtoul(argv[2], nullptr, 0);

    Machine dut;
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.keyboard = kAllUp;
    dut.tape_ready = 0;
    dut.tape_size = 0;
    dut.tape_data = 0;
    dut.peek_addr = 0;
#ifndef DIAGNOSTIC_EXPANSION
    dut.external_ram_data = 0;
    dut.external_peek_data = 0;
    dut.bus_rdata = 0;
    dut.bus_peek_data = 0;
    dut.bus_dsel = 0;
    dut.bus_romcs = 0;
    dut.bus_wait = 0;
    dut.bus_ram_present = 0;
    dut.save_addr = 0;
#endif
    dut.eval();
    run(dut, 2048);
    dut.reset = 0;

    wait_byte(dut, kReady, 0x5a, 4 * kFrameTicks, "first frame");
    const uint16_t ramtop = word_at(dut, kRamtop);
    std::cout << "FES ZX81 diagnostic: RAMTOP=" << std::hex << ramtop << std::dec << '\n';
    require(ramtop == expected_top, "RAMTOP probe");
    require(word_at(dut, kDFilePtr) == kDFile, "D_FILE pointer");
    require(peek(dut, kErrors) == 0, "error bits after boot");
    check_cells(dut, 2, 7, hex_cells({uint8_t(ramtop >> 8), uint8_t(ramtop)}), "RAMTOP");
    check_frame(dut, rom, "boot");

    // One key per row (not L or S); the row byte is A0-A4 plus the 50 Hz bit.
    for (unsigned row = 0; row != 8; ++row) {
        const unsigned bit = (row + 2) % 5;
        dut.keyboard = key(row, bit);
        frames(dut, 2, "key row " + std::to_string(row));
        for (unsigned other = 0; other != 8; ++other) {
            const uint8_t want = other == row ? uint8_t(0x5f & ~(1u << bit)) : 0x5f;
            require(peek(dut, kKeys + other) == want,
                    "key row " + std::to_string(row) + " read in row " + std::to_string(other));
        }
        std::vector<uint8_t> digits;
        for (unsigned b = 0; b != 5; ++b) digits.push_back(hex_digit(b != bit));
        check_cells(dut, 5 + row, 5, digits, "key row " + std::to_string(row));
        if (row == 7)
            check_frame(dut, rom, "key");
    }
    dut.keyboard = kAllUp;
    frames(dut, 2, "keys up");

    // A 16-byte .p body: the word at $4014 (offset 11) ends it at $4019.
    const std::vector<uint8_t> image = {0xf0, 0xe1, 0xd2, 0xc3, 0xb4, 0xa5, 0x96, 0x87,
                                        0x78, 0x69, 0x5a, 0x19, 0x40, 0x3c, 0x2d, 0x1e};
    tape = &image;
    dut.tape_ready = 1;
    dut.tape_size = image.size();
    dut.keyboard = key(6, 1);  // L
    bool busy = false;
    for (uint64_t i = 0; i < 4 * kFrameTicks && peek(dut, kLoads) != 1; i += 0x1000)
        for (unsigned j = 0; j != 0x1000; ++j) {
            tick(dut);
            busy |= dut.tape_busy;
        }
    require(busy, "LOAD never entered the mailbox loader");
    require(peek(dut, kLoads) == 1, "LOAD did not return through $0207");
    dut.keyboard = kAllUp;
    dut.tape_ready = 0;
    frames(dut, 2, "after LOAD");
    for (unsigned i = 0; i != image.size(); ++i)
        require(peek(dut, kTape + i) == image[i], "LOAD byte " + std::to_string(i));
    check_cells(dut, 15, 0, hex_cells(image), "tape row");

    // No tape: the loader sets carry at once and RAM keeps the last image.
    dut.keyboard = key(6, 1);
    wait_byte(dut, kLoads, 2, 4 * kFrameTicks, "empty LOAD");
    dut.keyboard = kAllUp;
    frames(dut, 2, "after empty LOAD");
    for (unsigned i = 0; i != image.size(); ++i)
        require(peek(dut, kTape + i) == image[i], "empty LOAD changed byte " + std::to_string(i));

#ifndef DIAGNOSTIC_EXPANSION
    dut.keyboard = key(1, 1);  // S
    for (uint64_t i = 0; i < 4 * kFrameTicks && !dut.save_ready; ++i) tick(dut);
    require(dut.save_ready, "SAVE did not publish a .p");
    dut.keyboard = kAllUp;
    require(dut.save_size == image.size(), "save_size is " + std::to_string(dut.save_size));
    for (unsigned i = 0; i != image.size(); ++i) {
        dut.save_addr = i;
        tick(dut);
        tick(dut);
        require(uint8_t(dut.save_q) == image[i], "SAVE byte " + std::to_string(i));
    }
    wait_byte(dut, kSaves, 1, 2 * kFrameTicks, "SAVE count");
    frames(dut, 2, "after SAVE");
    check_cells(dut, 16, 13, hex_cells({1}), "SAVE count");
#endif
    require(peek(dut, kErrors) == 0, "error bits " + std::to_string(peek(dut, kErrors)));
    check_cells(dut, 16, 5, hex_cells({2}), "LOAD count");
    check_frame(dut, rom, "tape");
    std::cout << "FES ZX81 diagnostic simulation passed\n";
    return 0;
}
