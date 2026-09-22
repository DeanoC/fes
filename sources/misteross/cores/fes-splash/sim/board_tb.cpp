// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "fes-splash board: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

struct Board {
    Vtop dut;
    Vtop___024root &root;
    uint64_t pixel_cycles = 0;

    Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        dut.eval();
    }

    void reference_tick() {
        dut.FPGA_CLK1_50 = 1;
        dut.eval();
        dut.FPGA_CLK1_50 = 0;
        dut.eval();
    }

    void pixel_tick() {
        root.top__DOT__video_clock__DOT__outclk_0 = 1;
        dut.eval();
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        dut.eval();
        ++pixel_cycles;
        if (pixel_cycles % 3 == 1) reference_tick();
    }
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Board board;
    for (unsigned hps_low = 0; hps_low != 4; ++hps_low) {
        for (unsigned external_low = 0; external_low != 4; ++external_low) {
            board.root.top__DOT__hdmi_i2c__DOT__out_clk = hps_low & 1;
            board.root.top__DOT__hdmi_i2c__DOT__out_data = (hps_low >> 1) & 1;
            board.root.top__DOT__hdmi_scl_pad__DOT__external_low = external_low & 1;
            board.root.top__DOT__hdmi_sda_pad__DOT__external_low = (external_low >> 1) & 1;
            board.dut.eval();
            require(!board.root.top__DOT__hdmi_scl_pad__DOT__drive_high &&
                        !board.root.top__DOT__hdmi_sda_pad__DOT__drive_high,
                    "I2C pad actively drives high");
            require(board.root.top__DOT__hdmi_scl_pad__DOT__drive_low == bool(hps_low & 1) &&
                        board.root.top__DOT__hdmi_sda_pad__DOT__drive_low == bool(hps_low & 2),
                    "I2C low enable is inverted or crossed");
            require(board.root.top__DOT__hdmi_i2c__DOT__observed_scl ==
                        !(hps_low & 1 || external_low & 1) &&
                    board.root.top__DOT__hdmi_i2c__DOT__observed_sda ==
                        !(hps_low & 2 || external_low & 2),
                    "I2C pad feedback does not reflect wired-AND bus");
        }
    }

    require(board.root.top__DOT__pll_locked, "sim PLL locked");

    unsigned lit = 0;
    unsigned letters = 0;
    for (unsigned i = 0; i < 1650 * 750; ++i) {
        board.pixel_tick();
        if (board.dut.HDMI_TX_D) ++lit;
        if (board.dut.HDMI_TX_D == 0xECF4F8u) ++letters;
        if (board.pixel_cycles % 3 == 1) {
            require(board.root.top__DOT__video_clock__DOT__outclk_0 == 0,
                    "pixel model left the PLL output high");
        }
    }
    require(lit > 0, "splash HDMI image is black");
    require(letters > 1000, "board path lost the FES mark");
    std::cout << "splash board passed I2C low-or-release, clock isolation and image path\n";
}
