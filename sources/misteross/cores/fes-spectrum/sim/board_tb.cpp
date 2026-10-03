// SPDX-License-Identifier: GPL-2.0-or-later
// Board simulation through the fes.computer mailbox: identity advertises the
// Spectrum tape unit, a .tap image can be committed while the machine runs,
// and a USB HID 'A' becomes the Spectrum matrix after the settle window.
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"

#include <cstdint>
#include <iostream>
#include <string>

namespace {

#if SPECTRUM_FAST_CPU
constexpr uint64_t kSysHalfPs = 8929;  // nominal 56 MHz, rounded to ps
#else
constexpr uint64_t kSysHalfPs = 9574;  // nominal 52.224 MHz, rounded to ps
#endif

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
    bool toggle = false;
    explicit Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__system_clock__DOT__outclk_0 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        root.top__DOT__hps_gp__DOT__gp_out = 0;
        dut.eval();
    }
    void step() {
        now = next_sys;
        next_sys += kSysHalfPs;
        const uint8_t level = !root.top__DOT__system_clock__DOT__outclk_0;
        root.top__DOT__system_clock__DOT__outclk_0 = level;
        dut.eval();
    }
    void run_clocks(int clocks) {
        for (int i = 0; i < clocks * 2; ++i) step();
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
    // video, keyboard, gamepad, audio, spectrum tape: bits 0, 1, 2, 3 and 5.
    require(board.ok(1, 7, 0, "capabilities") == 0x002f, "capability word");
    require(board.ok(5, 0, 0, "min lo") == 1, "tape minimum");
    require(board.ok(5, 1, 0, "min hi") == 0, "tape minimum high");
    require(board.ok(5, 2, 0, "max lo") == 0, "tape maximum low");
    require(board.ok(5, 3, 0, "max hi") == 1, "tape maximum high");
    require(board.ok(5, 5, 0, "state") == 1, "tape starts empty");
    board.ok(2, 0, 1, "release");

    const uint8_t tap[3] = {0x01, 0x00, 0xff};
    const uint32_t crc = crc32(tap, 3);
    board.ok(6, 0, 3, "begin total");
    board.ok(6, 1, 0, "begin total high");
    board.ok(6, 2, crc & 0xffff, "crc lo");
    board.ok(6, 3, crc >> 16, "crc hi");
    board.ok(7, 0, 0, "chunk offset");
    board.ok(7, 1, 0, "chunk offset high");
    board.ok(7, 2, 3, "chunk length");
    board.ok(8, 0, tap[0] | (tap[1] << 8), "data 0");
    board.ok(8, 1, tap[2], "data 1");
    board.ok(9, 0, 0, "commit");
    require(board.ok(5, 5, 0, "ready") == 3, "tape did not become ready");

    // HID usage 0x04 ('A') is bit 4 of keyboard row 0.
    board.ok(3, 0, 1u << 4, "key A");
    board.run_clocks(60000);
    const uint64_t matrix = board.root.top__DOT__matrix;
    require((matrix & 0xffffffffffULL) == (0xffffffffffULL & ~(1ULL << 5)),
            "HID A did not land on the Spectrum A key");
    std::cout << "fes.spectrum board: ok\n";
    return 0;
}
