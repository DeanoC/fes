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
    std::cerr << "FES Coleco board: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

uint32_t command(bool toggle, uint8_t opcode, uint8_t index, uint16_t argument) {
    return (toggle ? 0x80000000u : 0u) | (uint32_t(opcode) << 24) |
           (uint32_t(index) << 16) | argument;
}

uint32_t response(bool toggle, bool error, uint16_t data) {
    return 0xf5000000u | (toggle ? 0x00800000u : 0u) |
           (error ? 0x00400000u : 0u) | data;
}

struct Board {
    Vtop dut;
    Vtop___024root &root;

    Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__system_clock__DOT__outclk_0 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        root.top__DOT__hps_gp__DOT__gp_out = 0;
        dut.eval();
    }

    void sys_tick() {
        root.top__DOT__system_clock__DOT__outclk_0 = 1;
        dut.eval();
        root.top__DOT__system_clock__DOT__outclk_0 = 0;
        dut.eval();
    }

    void pixel_tick() {
        root.top__DOT__video_clock__DOT__outclk_0 = 1;
        dut.eval();
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        dut.eval();
    }

    void set_gpo(uint32_t value) {
        root.top__DOT__hps_gp__DOT__gp_out = value;
        dut.eval();
    }

    uint32_t gpi() const { return root.top__DOT__hps_gp__DOT__observed_gpi; }

    void exchange(bool &toggle, uint8_t opcode, uint8_t index, uint16_t argument,
                  uint32_t expected, const std::string &name) {
        set_gpo(command(toggle, opcode, index, argument));
        sys_tick();
        toggle = !toggle;
        set_gpo(command(toggle, opcode, index, argument));
        for (unsigned i = 0; i != 8; ++i) {
            if (((gpi() >> 23) & 1u) == unsigned(toggle)) {
                require(gpi() == expected, name + ": unexpected mailbox response");
                return;
            }
            sys_tick();
        }
        fail(name + ": mailbox ACK timeout");
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

    bool toggle = false;
    require(board.gpi() == 0xf5000000u, "initial mailbox signature");
    board.exchange(toggle, 1, 4, 0, response(!toggle, false, 2), "identity tag");
    board.exchange(toggle, 3, 0, 0x0011, response(!toggle, false, 0), "keyboard row");
    board.exchange(toggle, 4, 0, 3, response(!toggle, false, 0), "media begin");
    board.exchange(toggle, 5, 0, 0x0201, response(!toggle, false, 0), "media pair");
    board.exchange(toggle, 5, 1, 3, response(!toggle, false, 0), "media tail");
    board.exchange(toggle, 6, 0, 0, response(!toggle, false, 0), "media commit");
    board.exchange(toggle, 2, 0, 1, response(!toggle, false, 0), "execution release");

    unsigned hs_rises = 0;
    unsigned vs_rises = 0;
    bool old_hs = false;
    bool old_vs = false;
    const unsigned cycles = 1650 * 751;
    for (unsigned i = 0; i != cycles; ++i) {
        board.pixel_tick();
        if ((board.dut.HDMI_TX_HS != 0) && !old_hs) ++hs_rises;
        if ((board.dut.HDMI_TX_VS != 0) && !old_vs) ++vs_rises;
        old_hs = board.dut.HDMI_TX_HS;
        old_vs = board.dut.HDMI_TX_VS;
        if ((i % 3) == 0) board.sys_tick();
    }
    require(hs_rises >= 750, "top shell did not produce 720p horizontal sync");
    require(vs_rises >= 1, "top shell did not produce vertical sync");
    require(board.dut.HDMI_TX_CLK == 0, "pixel clock boundary did not settle");
    std::cout << "FES Coleco board shell I2C/mailbox/720p path passed\n";
    return EXIT_SUCCESS;
}
