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

uint32_t command(bool toggle, uint8_t opcode, uint16_t argument, uint8_t index = 0) {
    return (toggle ? 0x80000000u : 0u) | (uint32_t(opcode) << 24) | (uint32_t(index) << 16) | argument;
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
                  const std::string &name, uint8_t index = 0) {
        set_gpo(command(toggle, opcode, argument, index));
        pixel_tick();
        const uint32_t held = gpi();
        toggle = !toggle;
        set_gpo(command(toggle, opcode, argument, index));
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
    board.set_gpo(command(true, 1, 0, 4));
    for(unsigned i=0;i<8;++i) board.reference_tick();
    require(board.gpi()==0xf5000000u,"reference clock advanced mailbox");
    toggle=true; board.wait_ack(toggle,"identity");
    require((board.gpi()&0x40ffff)==3,"application identity");
    if(MEDIA) {
        board.exchange(toggle,2,1,"premature release");
        require((board.gpi()&0x40ffff)==0x400004,"release before asset");
        board.exchange(toggle,4,3,"begin palette");
        board.exchange(toggle,5,0x00ff,"red green");
        board.exchange(toggle,3,8,"interleaved right");
        board.exchange(toggle,5,0,"blue",1);
        board.exchange(toggle,6,0,"commit palette");
    }
    board.exchange(toggle,2,1,"release");
    require(!board.root.top__DOT__mailbox_reset,"application stayed reset");
    unsigned lit=0;
    for(unsigned i=0;i<1650*750;++i) {
        board.pixel_tick();
        if(board.dut.HDMI_TX_D) ++lit;
        if(MEDIA) require((board.dut.HDMI_TX_D&0xffff)==0,"palette asset not applied");
    }
    require(lit>0,"demo image is black");
    if(MEDIA) require(board.root.top__DOT__core__DOT__phase==4,"button did not change animation speed");
    board.exchange(toggle,2,0,"hold");
    board.pixel_tick();
    require(board.root.top__DOT__mailbox_reset && !board.root.top__DOT__mailbox_buttons,"HOLD vector");
    require(board.dut.HDMI_TX_D==0,"reset output not black");
    std::cout<<"application board passed media="<<MEDIA<<" clock isolation, I2C and image path\n";
}
