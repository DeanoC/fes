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
    std::cerr << "FES board: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

uint32_t command(bool toggle, uint8_t opcode, uint16_t argument) {
    return (toggle ? 0x80000000u : 0u) | (uint32_t(opcode) << 24) | argument;
}

struct Board {
    Vtop dut;
    Vtop___024root &root;
    uint64_t pixel_cycles = 0;

    Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        root.top__DOT__hps_gp__DOT__gp_out = 0;
        dut.eval();
    }

    void set_gpo(uint32_t value) {
        root.top__DOT__hps_gp__DOT__gp_out = value;
        dut.eval();
    }

    uint32_t gpi() const { return root.top__DOT__hps_gp__DOT__observed_gpi; }

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
        // The 50 MHz reference has an independent phase in this digital model.
        if (pixel_cycles % 3 == 1) reference_tick();
    }

    void wait_ack(bool toggle, const std::string &name) {
        for (unsigned cycle = 0; cycle != 8; ++cycle) {
            if (((gpi() >> 23) & 1u) == unsigned(toggle)) return;
            pixel_tick();
        }
        fail(name + ": pixel-domain ACK timeout");
    }

    void exchange(bool &toggle, uint8_t opcode, uint16_t argument,
                  const std::string &name) {
        set_gpo(command(toggle, opcode, argument));
        pixel_tick();
        const uint32_t held = gpi();
        toggle = !toggle;
        set_gpo(command(toggle, opcode, argument));
        wait_ack(toggle, name);
        require(gpi() != held || (((held >> 23) & 1u) == unsigned(toggle)),
                name + ": response did not commit with ACK");
    }
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Board board;
    // The HPS I2C path is combinational and independent of the pixel clock.
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
            require(board.root.top__DOT__hdmi_i2c__DOT__observed_scl == !(hps_low & 1 || external_low & 1) &&
                    board.root.top__DOT__hdmi_i2c__DOT__observed_sda == !(hps_low & 2 || external_low & 2),
                    "I2C pad feedback does not reflect wired-AND bus");
        }
    }
    bool toggle = false;
    require(board.gpi() == 0xf5000000u, "initial mailbox state");

    // A request may arrive while the PLL/pixel clock is stopped. Identity and
    // controls must remain at their initialized state until pixel clocks run.
    board.set_gpo(command(false, 2, 1));
    board.reference_tick();
    board.reference_tick();
    board.set_gpo(command(true, 2, 1));
    for (unsigned cycle = 0; cycle != 5; ++cycle) board.reference_tick();
    require(board.gpi() == 0xf5000000u,
            "mailbox advanced on reference clock while pixel clock was stopped");

    toggle = true;
    board.wait_ack(toggle, "release gameplay");
    require(!board.root.top__DOT__mailbox_reset,
            "gameplay release was not committed in pixel domain");

    // Establish a deliberately multi-bit controller state before arranging a
    // reset commit on the last pixel of the frame.
    board.exchange(toggle, 3, 0x00a5, "multi-bit buttons");
    require(board.root.top__DOT__mailbox_buttons == 0xa5,
            "multi-bit buttons were not accepted atomically");

    while (!(board.root.top__DOT__core__DOT__video__DOT__horizontal == 1647 &&
             board.root.top__DOT__core__DOT__video__DOT__vertical == 749)) {
        board.pixel_tick();
    }
    board.set_gpo(command(toggle, 2, 0));
    board.reference_tick();
    toggle = !toggle;
    board.set_gpo(command(toggle, 2, 0));
    board.pixel_tick();
    board.pixel_tick();
    require(board.root.top__DOT__core__DOT__video__DOT__horizontal == 1649 &&
                board.root.top__DOT__core__DOT__video__DOT__vertical == 749,
            "reset request was not phased immediately before frame tick");
    board.pixel_tick();
    require(((board.gpi() >> 23) & 1u) == unsigned(toggle),
            "reset ACK did not commit on the frame-tick pixel edge");
    require(board.root.top__DOT__mailbox_reset &&
                board.root.top__DOT__mailbox_buttons == 0,
            "reset and button clear did not commit as one vector");
    board.pixel_tick();
    require(board.root.top__DOT__core__DOT__game__DOT__player_y == 104 &&
                !board.root.top__DOT__core__DOT__game__DOT__playing,
            "game did not observe the coherent reset vector after frame tick");

    std::cout << "FES board: I2C low/release/feedback, independent reference/pixel phases and coherent frame-edge controls passed\n";
    return EXIT_SUCCESS;
}
