// SPDX-License-Identifier: GPL-2.0-or-later
// fes.riscv shell through the fes.application mailbox: identity, release,
// gamepad input reaching the firmware, HDMI pixels and execution hold.
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"
#include "frame_capture.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "fes.riscv board: " << message << '\n';
    std::exit(EXIT_FAILURE);
}
void require(bool ok, const std::string &message) { if (!ok) fail(message); }

struct Board {
    Vtop dut;
    Vtop___024root &root;
    FrameCapture capture;
    bool toggle = false;
    uint64_t cycles = 0;

    Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        root.top__DOT__hps_gp__DOT__gp_out = 0;
        dut.eval();
    }
    void tick() {
        root.top__DOT__video_clock__DOT__outclk_0 = 1; dut.eval();
        root.top__DOT__video_clock__DOT__outclk_0 = 0; dut.eval();
        ++cycles;
        capture.sample(dut.HDMI_TX_DE, dut.HDMI_TX_VS, dut.HDMI_TX_D);
    }
    unsigned command(unsigned opcode, unsigned argument, unsigned index = 0) {
        toggle = !toggle;
        root.top__DOT__hps_gp__DOT__gp_out = (toggle ? 0x80000000u : 0) | (opcode << 24) | (index << 16) | argument;
        for (unsigned i = 0; i < 8; ++i) tick();
        unsigned reply = root.top__DOT__hps_gp__DOT__observed_gpi;
        require(bool(reply & 0x800000) == toggle && !(reply & 0x400000), "mailbox command rejected");
        return reply & 0xffff;
    }
    void frames(unsigned count) {
        unsigned target = capture.frames + count;
        uint64_t limit = cycles + uint64_t(count + 2) * 1650 * 750;
        while (capture.frames < target) {
            require(cycles < limit, "raster did not complete the requested frames");
            tick();
        }
    }
    int box_column(int row, uint8_t colour) { return capture.find_run(row, FrameCapture::expand(colour), 12); }
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Board b;
    for (unsigned i = 0; i < 2000; ++i) b.tick();
    require(b.command(1, 0, 7) == 0x3, "capabilities advertise fixed video and the gamepad");
    require(b.command(1, 0, 4) == 3, "fes.application ABI tag");
    // Held in reset: the raster runs, the framebuffer stays unwritten by the CPU.
    b.frames(2);
    require(b.capture.at(0, 0) != FrameCapture::expand(0xff) || b.capture.at(80, 60) != FrameCapture::expand(0xe0),
            "held firmware must not draw the scene");
    require(b.command(2, 1, 0) == 0, "release ack");
    b.frames(3);
    require(b.capture.at(0, 0) == FrameCapture::expand(0xff), "border reaches the HDMI pins after release");
    require(b.box_column(60, 0xe0) == 74, "centred red box after release");
    require(b.command(3, 8, 0) == 0, "Right ack");
    b.frames(6);
    require(b.command(3, 0, 0) == 0, "neutral ack");
    b.frames(2);
    int moved = b.box_column(60, 0xe0);
    require(moved >= 78 && moved <= 82, "generic gamepad Right moved the box: column " + std::to_string(moved));
    require(b.command(2, 0, 0) == 0, "hold ack");
    b.frames(1);
    require(b.command(2, 1, 0) == 0, "second release ack");
    b.frames(3);
    require(b.box_column(60, 0xe0) == 74, "hold restarted the firmware with a centred box");
    require(b.capture.bar_violations == 0, "side bars stay black");
    std::cout << "fes.riscv board mailbox, release, gamepad, HDMI output and hold passed (" << b.cycles
              << " pixel cycles)\n";
    return 0;
}
