// SPDX-License-Identifier: GPL-2.0-or-later
// Host simulation of one Spectrum model with the open diagnostic firmware:
// paging, AY read-back, keyboard matrix, Kempston, probe card, tape pilot,
// the captured frame and the T state of a contended timing marker.
#include "Vspectrum_sim_top.h"
#include "Vspectrum_sim_top___024root.h"
#include "spectrum_ram_banks.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

#define MACHINE(member) spectrum_sim_top__DOT__machine__DOT__##member

namespace {

#ifndef SPECTRUM_MODEL_NAME
#define SPECTRUM_MODEL_NAME "48K"
#endif

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "fes.spectrum " SPECTRUM_MODEL_NAME ": " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

std::string hex(unsigned value) {
    static const char digits[] = "0123456789abcdef";
    std::string out = "$";
    for (int shift = 12; shift >= 0; shift -= 4)
        if (value >> shift || shift < 8) out += digits[(value >> shift) & 15];
    return out;
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vspectrum_sim_top dut;
    auto& root = *dut.rootp;
    auto bank = [&](unsigned number, unsigned offset) -> unsigned {
        const uint8_t* memory = spectrum_ram_bank(root, number);
        require(memory != nullptr, "no RAM bank " + std::to_string(number));
        return memory[offset];
    };
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.matrix = 0xffffffffffULL & ~(1ULL << 5);  // A held
    dut.kempston = 0x11;                          // right + fire
    dut.unit_state = 3;
    dut.unit_size = 3;
    dut.eval();
    std::vector<uint32_t> frame(240 * 40);
    auto clock = [&]() {
        dut.clk_sys = 1; dut.eval();
        dut.clk_sys = 0; dut.eval();
        if (dut.pixel_we) frame[dut.pixel_row * 40 + dut.pixel_chunk] = dut.pixel_data;
    };
    for (int i = 0; i < 32; ++i) clock();
    dut.reset = 0;

    int frames = 0, matched = 0, last_ear = 0;
    uint32_t since = 0;
    bool seen_edge = false;
    long marker_t = -1;
    unsigned marker_frame = 0;
    while (frames < 4 || matched < 3) {
        clock();
        if (dut.tick && seen_edge) ++since;
        if (dut.ear != last_ear) {
            matched = seen_edge && since == 2168 ? matched + 1 : seen_edge ? 0 : matched;
            seen_edge = true;
            since = 0;
            last_ear = dut.ear;
        }
        if (marker_t < 0 && bank(2, 6) == 0xc3) {
            marker_t = root.MACHINE(ula__DOT__t);
            marker_frame = frames;
        }
        if (dut.frame_done) ++frames;
        if (dut.cpu_illegal) fail("CPU entered an illegal state");
        if (frames > 400) fail("diagnostic did not finish (pilot edges " + std::to_string(matched) + ")");
    }
    require(bank(2, 0) == 0xa5, "diagnostic marker missing at $8000");
    if (spectrum_ram_bank(root, 7) != nullptr) {
        require(bank(7, 0) == 0x77, "bank 7 did not receive $77 through $7FFD");
        require(bank(0, 0) == 0x66, "bank 0 did not return to $C000");
        require(bank(2, 5) == 0x0f, "AY register 8 read back " + hex(bank(2, 5)));
    } else {
        require(bank(0, 0) == 0x66, "48K $C000 is not bank 0");
    }
    require((bank(2, 1) & 0xbf) == 0xbe, "keyboard A row read " + hex(bank(2, 1)));
    require(bank(2, 2) == 0x11, "Kempston read " + hex(bank(2, 2)));
    require(bank(2, 3) == 0xf5, "probe socket 1 id " + hex(bank(2, 3)));
    require(bank(2, 4) == 0x5a, "probe scratch " + hex(bank(2, 4)));
    require(matched >= 3, "tape pilot edges were not 2168 T-states apart");
    // The contended marker is a regression check on ULA timing, recorded from
    // the Fuse-matched simulation of this firmware.
    require(marker_t == SPECTRUM_MARKER_T,
            "timing marker at T " + std::to_string(marker_t) + " (frame " + std::to_string(marker_frame) +
            "), expected " + std::to_string(SPECTRUM_MARKER_T));
    // A chunk word is {bitmap, ink, paper}; a set bitmap bit shows ink.
    auto pixel = [&](int x, int y) {
        const uint32_t word = frame[y * 40 + x / 8];
        return (word >> (15 - x % 8)) & 1 ? (word >> 4) & 15 : word & 15;
    };
    require(pixel(0, 0) == 2, "border pixel is not red");
    require(pixel(32, 24) == 0 && pixel(39, 24) == 0, "first paper byte is not black ink");
    require(pixel(32, 25) == 7, "paper below the ink byte is not white (bitmap " + hex(bank(5, 0x100)) +
                                    ", attribute " + hex(bank(5, 0x1800)) + ")");
    std::cout << "fes.spectrum " SPECTRUM_MODEL_NAME " machine: ok (marker T " << marker_t << ")\n";
    return 0;
}
