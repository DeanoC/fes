// SPDX-License-Identifier: GPL-2.0-or-later
// Host simulation of one Spectrum model with the open diagnostic firmware:
// paging, AY read-back, keyboard matrix, Kempston, probe card, the turbo tape
// that the firmware's own loader starts (through the deck's loader detection)
// and decodes, its 1200 T pilot, the captured frame and the T state of a
// contended timing marker.
#include "Vspectrum_sim_top.h"
#include "Vspectrum_sim_top___024root.h"
#include "spectrum_ram_banks.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <iterator>
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
    // --turbo runs every T state in turbo; the results must not change.
    for (int i = 1; i < argc; ++i)
        if (std::string(argv[i]) == "--turbo") dut.turbo_always = 1;
    dut.matrix = 0xffffffffffULL & ~(1ULL << 5);  // A held
    dut.kempston = 0x11;                          // right + fire
    // The diagnostic tape, streamed as the DDR store presents it: one byte
    // after each take, restarting at byte 0 whenever the deck drops enable.
    std::ifstream tape_file("build/diagnostics/fes-spectrum/tape.pzx", std::ios::binary);
    const std::vector<uint8_t> tape((std::istreambuf_iterator<char>(tape_file)), {});
    require(tape.size() > 10, "no diagnostic tape");
    size_t tape_pos = 0;
    dut.tape_ready = 1;
    dut.tape_size = tape.size();
    dut.eval();
    std::vector<uint32_t> frame(240 * 40);
    bool tape_played = false;
    auto clock = [&]() {
        if (!dut.tape_stream_enable) tape_pos = 0;
        dut.tape_stream_valid = dut.tape_stream_enable && tape_pos < tape.size();
        dut.tape_stream_data = tape_pos < tape.size() ? tape[tape_pos] : 0;
        dut.eval();
        const bool took = dut.tape_stream_valid && dut.tape_stream_take;
        dut.clk_sys = 1; dut.eval();
        dut.clk_sys = 0; dut.eval();
        if (took) ++tape_pos;
        tape_played |= dut.tape_playing;
        if (dut.pixel_we) frame[dut.pixel_row * 40 + dut.pixel_chunk] = dut.pixel_data;
    };
    for (int i = 0; i < 32; ++i) clock();
    dut.reset = 0;

    int frames = 0, matched = 0, last_ear = 0;
    uint32_t since = 0;
    bool seen_edge = false;
    long marker_t = -1;
    unsigned marker_frame = 0;
    const uint8_t loaded[4] = {0xa5, 0x3c, 0x81, 0x7e};
    auto tape_loaded = [&]() {
        for (unsigned i = 0; i < 4; ++i)
            if (bank(2, 0x100 + i) != loaded[i]) return false;
        return true;
    };
    bool pilot_seen = false;
    while (frames < 4 || !pilot_seen || !tape_loaded()) {
        // The marker's T state is its write cycle's T2, at that T state's
        // ce_n; when RAM changes depends on the clocks per T state.
        if (marker_t < 0 && root.MACHINE(cpu_ce_n) && root.MACHINE(cycle_present) &&
            root.MACHINE(cycle_kind) == 2 && root.MACHINE(cycle_t) == 2 &&
            root.MACHINE(cpu_a) == 0x8006 && root.MACHINE(cpu_do) == 0xc3) {
            marker_t = root.MACHINE(ula__DOT__t);
            marker_frame = frames;
        }
        clock();
        if (dut.tick && seen_edge) ++since;
        if (dut.ear != last_ear) {
            matched = seen_edge && since == 1200 ? matched + 1 : seen_edge ? 0 : matched;
            pilot_seen |= matched >= 3;
            seen_edge = true;
            since = 0;
            last_ear = dut.ear;
        }
        if (dut.frame_done) ++frames;
        if (dut.cpu_illegal) fail("CPU entered an illegal state");
        if (frames > 400)
            fail("diagnostic did not finish (pilot edges " + std::to_string(matched) + ", bytes " +
                 hex(bank(2, 0x100)) + " " + hex(bank(2, 0x101)) + " " + hex(bank(2, 0x102)) + " " +
                 hex(bank(2, 0x103)) + ")");
    }
    // After the tape the firmware runs its main loop; give it a whole frame.
    for (const int until = frames + 1; frames < until;) {
        clock();
        if (dut.frame_done) ++frames;
    }
    require(bank(2, 0) == 0xa5, "diagnostic marker missing at $8000");
    require(bank(2, 6) == 0xc3, "timing marker missing at $8006");
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
    require(tape_played, "the firmware's loader did not start the tape");
    require(pilot_seen, "tape pilot edges were not 1200 T-states apart");
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
    // FLASH swaps ink and paper every 16 frames, as the ULA and Fuse do. The
    // last paper cell of the shown screen becomes a solid byte with FLASH,
    // blue paper and yellow ink; its chunk is row 215, chunk 35.
    const bool shadow = spectrum_ram_bank(root, 7) != nullptr && (root.MACHINE(port_7ffd) & 0x08);
    uint8_t* screen = spectrum_ram_bank(root, shadow ? 7 : 5);
    screen[0x17ff] = 0xff;
    screen[0x1aff] = 0x80 | (1 << 3) | 6;
    std::vector<int> swaps;
    unsigned last_ink = 0;
    for (int seen = 0; seen < 36;) {
        clock();
        if (!dut.frame_done) continue;
        const unsigned ink = (frame[215 * 40 + 35] >> 4) & 15;
        require(ink == 1 || ink == 6, "FLASH cell shows ink " + std::to_string(ink));
        if (seen > 0 && ink != last_ink) swaps.push_back(seen);
        last_ink = ink;
        ++seen;
    }
    require(swaps.size() >= 2 && swaps[1] - swaps[0] == 16,
            "FLASH did not swap every 16 frames (" + std::to_string(swaps.size()) + " swaps)");
    std::cout << "fes.spectrum " SPECTRUM_MODEL_NAME " machine: ok (marker T " << marker_t << ")\n";
    return 0;
}
