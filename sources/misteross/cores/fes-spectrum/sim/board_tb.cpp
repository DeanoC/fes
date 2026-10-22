// SPDX-License-Identifier: GPL-2.0-or-later
// Board simulation through the fes.computer mailbox: identity advertises the
// Spectrum tape unit and the session display, a PZX image committed in two
// chunks while the machine runs lands in HPS DDR (a byte pair straddling a
// DDR word), the firmware's turbo loader plays it back from DDR (its 1200 T
// pilot edges and the four bytes it decodes), a USB HID 'A' becomes the Spectrum matrix after the settle window,
// and controller port 1 is Sinclair joystick 1.
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"

#include <cstdint>
#include <fstream>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

namespace {

constexpr uint64_t kSysHalfPs = 9574;    // nominal 52.224 MHz, rounded to ps
constexpr uint64_t kPixelHalfPs = 6734;  // nominal 74.25 MHz, rounded to ps

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "fes.spectrum board: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

uint32_t crc32(const uint8_t* bytes, int length) {
    uint32_t c = 0xffffffffu;
    for (int i = 0; i < length; ++i) {
        c ^= bytes[i];
        for (int bit = 0; bit < 8; ++bit) c = (c & 1u) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

struct Board {
    Vtop dut;
    Vtop___024root& root;
    uint64_t now = 0;
    uint64_t next_sys = 0;
    uint64_t next_pixel = 0;
    bool toggle = false;
    explicit Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__system_clock__DOT__outclk_0 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        root.top__DOT__hps_gp__DOT__gp_out = 0;
        dut.eval();
    }
    // Advance to the next edge of either unrelated clock.
    bool step() {
        if (next_pixel < next_sys) {
            now = next_pixel;
            next_pixel += kPixelHalfPs;
            root.top__DOT__video_clock__DOT__outclk_0 = !root.top__DOT__video_clock__DOT__outclk_0;
            dut.eval();
            return false;
        }
        now = next_sys;
        next_sys += kSysHalfPs;
        root.top__DOT__system_clock__DOT__outclk_0 = !root.top__DOT__system_clock__DOT__outclk_0;
        dut.eval();
        return true;
    }
    void run_clocks(int clocks) {
        for (int i = 0; i < clocks * 2;) i += step();
    }
    uint32_t gpi() const { return root.top__DOT__hps_gp__DOT__observed_gpi; }
    uint32_t request(uint32_t opcode, uint32_t index, uint32_t argument) {
        const uint32_t fields = (opcode << 24) | (index << 16) | argument;
        root.top__DOT__hps_gp__DOT__gp_out = fields | (toggle ? 0x80000000u : 0);
        run_clocks(4);
        toggle = !toggle;
        root.top__DOT__hps_gp__DOT__gp_out = fields | (toggle ? 0x80000000u : 0);
        for (int i = 0; i < 64; ++i) {
            run_clocks(1);
            const uint32_t word = gpi();
            if ((word >> 24) == 0xf5 && ((word >> 23) & 1u) == static_cast<uint32_t>(toggle))
                return word;
        }
        fail("mailbox did not acknowledge");
    }
    uint16_t ok(uint32_t opcode, uint32_t index, uint32_t argument, const char* what) {
        const uint32_t word = request(opcode, index, argument);
        if (word & 0x400000u)
            fail(std::string(what) + " rejected with error " + std::to_string(word & 0xffff));
        return static_cast<uint16_t>(word);
    }
};

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Board board;
    board.run_clocks(8);
    require(board.ok(1, 0, 0, "magic") == 0x4546, "identity magic");
    require(board.ok(1, 4, 0, "tag") == 4, "ABI tag");
    // video, keyboard, gamepad, audio, spectrum tape (bits 0-3 and 5), HPS DDR
    // and the session display (bits 12 and 13).
    require(board.ok(1, 7, 0, "capabilities") == 0x302f, "capability word");
    // fes.media.spectrum-tape 2.0: a PZX image of 10 bytes to 16 MiB.
    require(board.ok(5, 0, 0, "min lo") == 10, "tape minimum");
    require(board.ok(5, 1, 0, "min hi") == 0, "tape minimum high");
    require(board.ok(5, 2, 0, "max lo") == 0, "tape maximum low");
    require(board.ok(5, 3, 0, "max hi") == 0x100, "tape maximum high");
    require(board.ok(5, 5, 0, "state") == 1, "tape starts empty");
    board.ok(2, 0, 1, "release");

    std::ifstream file("build/diagnostics/fes-spectrum/tape.pzx", std::ios::binary);
    const std::vector<uint8_t> tape((std::istreambuf_iterator<char>(file)), {});
    require(tape.size() > 16 && tape.size() <= 512, "no diagnostic tape");
    const uint32_t crc = crc32(tape.data(), static_cast<int>(tape.size()));
    board.ok(6, 0, tape.size(), "begin total");
    board.ok(6, 1, 0, "begin total high");
    board.ok(6, 2, crc & 0xffff, "crc lo");
    board.ok(6, 3, crc >> 16, "crc hi");
    // A 7-byte chunk leaves the next one at an odd offset, so its pair at
    // bytes 7 and 8 straddles two DDR words.
    for (const auto [offset, length] : {std::pair<size_t, size_t>{0, 7}, {7, tape.size() - 7}}) {
        board.ok(7, 0, offset, "chunk offset");
        board.ok(7, 1, 0, "chunk offset high");
        board.ok(7, 2, length, "chunk length");
        for (size_t i = 0; i < length; i += 2) {
            const unsigned low = tape[offset + i];
            const unsigned high = i + 1 < length ? tape[offset + i + 1] : 0;
            board.ok(8, i / 2, low | (high << 8), "data");
        }
    }
    board.ok(9, 0, 0, "commit");
    require(board.ok(5, 5, 0, "ready") == 3, "tape did not become ready");
    // The image is at window offset 16 MiB, the model's media base.
    for (size_t i = 0; i < tape.size(); ++i) {
        const uint64_t word = board.root.top__DOT__memory__DOT__f2sdram__DOT__media[i / 8];
        require(((word >> (8 * (i % 8))) & 0xff) == tape[i], "DDR byte " + std::to_string(i) + " differs");
    }

    // The diagnostic's loader starts the deck, which streams the image back
    // from DDR: three successive pilot edges 1200 T apart, then the four bytes
    // the loader decodes into $8100.
    int matched = 0;
    bool seen_edge = false, last_ear = false, last_tick = false;
    uint32_t since = 0;
    for (uint64_t clocks = 0; matched < 3; ++clocks) {
        board.run_clocks(1);
        const bool tick = board.root.top__DOT__tick;
        if (tick && !last_tick && seen_edge) ++since;
        last_tick = tick;
        const bool ear = board.root.top__DOT__ear;
        if (ear != last_ear) {
            matched = seen_edge && since == 1200 ? matched + 1 : seen_edge ? 0 : matched;
            seen_edge = true;
            since = 0;
            last_ear = ear;
        }
        require(clocks < 8000000, "no tape pilot from DDR (" + std::to_string(matched) + " edges)");
    }
    // The 48K top keeps Spectrum bank 2 ($8000) as its second stored bank.
    const uint8_t loaded[4] = {0xa5, 0x3c, 0x81, 0x7e};
    auto ram = [&](unsigned offset) { return board.root.top__DOT__machine__DOT__ram__DOT__bank__BRA__1__KET____DOT__mem[offset]; };
    for (uint64_t clocks = 0;; ++clocks) {
        bool done = true;
        for (unsigned i = 0; i < 4; ++i) done &= ram(0x100 + i) == loaded[i];
        if (done) break;
        require(clocks < 20000000, "the loader did not decode the tape from DDR");
        board.run_clocks(1);
    }
    require(!board.root.top__DOT__memory__DOT__f2sdram__DOT__violation_media &&
            !board.root.top__DOT__memory__DOT__f2sdram__DOT__violation_plane,
            "DDR model saw a protocol or window violation");

    // HID usage 0x04 ('A') is bit 4 of keyboard row 0.
    board.ok(3, 0, 1u << 4, "key A");
    board.run_clocks(60000);
    const uint64_t matrix = board.root.top__DOT__matrix;
    require((matrix & 0xffffffffffULL) == (0xffffffffffULL & ~(1ULL << 5)),
            "HID A did not land on the Spectrum A key");
    board.ok(3, 0, 0, "keys up");
    // Port 1 Up/Fire: Sinclair joystick 1 is keys 9 and 0.
    board.ok(4, 1, 0x11, "pad 1");
    board.run_clocks(60000);
    const uint64_t joystick = board.root.top__DOT__matrix;
    require((joystick & 0xffffffffffULL) == (0xffffffffffULL & ~(1ULL << 21) & ~(1ULL << 20)),
            "Sinclair joystick 1 did not press 9 and 0");
    std::cout << "fes.spectrum board: ok\n";
    return 0;
}
