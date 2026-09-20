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

    // Restore through the production mailbox; game reset never owns the record.
    auto ok = [&](uint8_t opcode, uint16_t argument, uint8_t index=0) {
        board.exchange(toggle, opcode, argument, "persistence board exchange", index);
        require(!(board.gpi() & 0x00400000u), "unexpected persistence error");
        return uint16_t(board.gpi());
    };
    ok(4,1); ok(6,2); ok(6,17,1); ok(4,2);
    require(board.root.top__DOT__mailbox_reset && board.root.top__DOT__paddle_speed==2,
            "restore must apply speed while reset stays held");
    ok(2,1); board.pixel_tick();
    // Directed positions accelerate collision coverage; every record change is
    // still caused by the actual shared game collision logic and event wiring.
    auto return_frame = [&]() {
        board.root.top__DOT__core__DOT__game__DOT__playing=1;
        board.root.top__DOT__core__DOT__game__DOT__ball_x=19;
        board.root.top__DOT__core__DOT__game__DOT__ball_y=112;
        board.root.top__DOT__core__DOT__game__DOT__player_y=104;
        board.root.top__DOT__core__DOT__game__DOT__rightward=0;
        board.root.top__DOT__core__DOT__game__DOT__downward=1;
        board.root.top__DOT__core__DOT__game__DOT__vertical_speed=1;
        board.root.top__DOT__core__DOT__video__DOT__horizontal=1649;
        board.root.top__DOT__core__DOT__video__DOT__vertical=749;
        board.dut.eval(); board.pixel_tick();
        require(board.root.top__DOT__player_return, "player collision must emit return pulse");
        board.pixel_tick();
        require(!board.root.top__DOT__player_return, "return event must last one clock");
    };
    for(unsigned i=0;i<18;++i) return_frame();
    require(board.root.top__DOT__gp_mailbox__DOT__current_rally==18 &&
            board.root.top__DOT__gp_mailbox__DOT__best_rally==18,
            "unfinished rally updates restored record immediately");

    // Arrange the 19th return on the exact edge accepting freeze. Its pulse
    // reaches the mailbox one edge later and must be drained before ACK.
    board.set_gpo(command(toggle,4,0)); board.pixel_tick(); toggle=!toggle;
    board.set_gpo(command(toggle,4,0)); board.pixel_tick(); board.pixel_tick();
    return_frame(); board.wait_ack(toggle,"collision-edge freeze");
    require(board.root.top__DOT__game_frozen && !board.root.top__DOT__mailbox_reset,
            "freeze must hold game without reset");
    require(ok(5,0)==2 && ok(5,0,1)==19, "freeze lost final running-edge return");
    auto bx=board.root.top__DOT__core__DOT__game__DOT__ball_x;
    auto by=board.root.top__DOT__core__DOT__game__DOT__ball_y;
    auto py=board.root.top__DOT__core__DOT__game__DOT__player_y;
    auto tone_left=board.root.top__DOT__core__DOT__game__DOT__tone_left;
    ok(3,2);
    for(unsigned i=0;i<200;++i) board.pixel_tick();
    ok(4,0);
    require(ok(5,0,1)==19 && board.root.top__DOT__core__DOT__game__DOT__ball_x==bx &&
            board.root.top__DOT__core__DOT__game__DOT__ball_y==by &&
            board.root.top__DOT__core__DOT__game__DOT__player_y==py &&
            board.root.top__DOT__core__DOT__game__DOT__tone_left==tone_left,
            "repeated freeze must retain snapshot and full gameplay");
    ok(4,3);
    require(!board.root.top__DOT__game_frozen && board.root.top__DOT__core__DOT__game__DOT__playing &&
            board.root.top__DOT__core__DOT__game__DOT__ball_x==bx,
            "resume must retain the running game's position");
    ok(3,0);
    for(unsigned i=19;i<65540;++i) return_frame();
    require(board.root.top__DOT__gp_mailbox__DOT__current_rally==65535 &&
            board.root.top__DOT__gp_mailbox__DOT__best_rally==65535,
            "current and best rally must saturate at u16 max");

    // Both scoring sides terminate a rally independently of 0-9 score wrap.
    for(unsigned side=0;side<2;++side) {
        for(unsigned score=0;score<10;++score) {
            board.root.top__DOT__core__DOT__game__DOT__playing=1;
            board.root.top__DOT__core__DOT__game__DOT__ball_x=side ? 316 : 0;
            board.root.top__DOT__core__DOT__game__DOT__rightward=side;
            board.root.top__DOT__core__DOT__video__DOT__horizontal=1649;
            board.root.top__DOT__core__DOT__video__DOT__vertical=749;
            board.dut.eval(); board.pixel_tick();
            require(board.root.top__DOT__point,"point event missing");
            board.pixel_tick();
            require(!board.root.top__DOT__point && board.root.top__DOT__gp_mailbox__DOT__current_rally==0,
                    "point must end current rally");
        }
    }
    require(board.root.top__DOT__core__DOT__game__DOT__player_score==0 &&
            board.root.top__DOT__core__DOT__game__DOT__ai_score==0,
            "display scores must retain wrap after nine");
    ok(2,0); board.pixel_tick();
    require(board.root.top__DOT__gp_mailbox__DOT__best_rally==65535 &&
            board.root.top__DOT__paddle_speed==2, "game reset must retain persistent registers");

    std::cout << "FES board: I2C/clock domains, restore, collision-edge freeze/resume, rally saturation and score wrap passed\n";
    return EXIT_SUCCESS;
}
