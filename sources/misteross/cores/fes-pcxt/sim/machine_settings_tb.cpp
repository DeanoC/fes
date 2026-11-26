// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vmachine_settings_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

void require(bool ok, const char* what) {
    if (!ok) {
        std::cerr << "FAIL machine_settings_tb: " << what << '\n';
        std::exit(1);
    }
}

struct Bench {
    Vmachine_settings_top dut;
    uint32_t toggle = 0;

    Bench() {
        dut.clk = 0;
        dut.gpo = 0;
        for (int i = 0; i < 4; ++i) dut.build_id[i] = 0;
        dut.eval();
        for (int i = 0; i < 6; ++i) tick();
    }
    void tick() {
        dut.eval();
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    }
    uint32_t request(uint8_t op, uint8_t index, uint16_t arg) {
        const uint32_t command = (uint32_t(op) << 24) | (uint32_t(index) << 16) | arg;
        dut.gpo = (toggle << 31) | command;
        for (int i = 0; i < 3; ++i) tick();
        toggle ^= 1;
        dut.gpo = (toggle << 31) | command;
        for (int i = 0; i < 40; ++i) {
            tick();
            if (((dut.gpi >> 23) & 1u) == toggle) return dut.gpi;
        }
        require(false, "mailbox timeout");
        return 0;
    }
    uint16_t ok(uint8_t op, uint8_t index, uint16_t arg = 0) {
        const uint32_t v = request(op, index, arg);
        require((v & 0xff000000u) == 0xf5000000u && !(v & (1u << 22)), "unexpected mailbox error");
        return static_cast<uint16_t>(v);
    }
    void err(uint8_t op, uint8_t index, uint16_t arg, uint16_t code) {
        const uint32_t v = request(op, index, arg);
        require((v & (1u << 22)) && static_cast<uint16_t>(v) == code, "unexpected error code");
    }
};

} // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Bench b;
    require(b.dut.exec_reset == 1, "starts held");
    require(b.dut.video_card == 0 && b.dut.mono_tint == 0, "defaults to cga/white");
    // Identity word 16: floppy (bit 20) | floppy-write (bit 21) | persistence (bit 17) | pcxt.machine (bit 23) = 0xb2.
    require(b.ok(1, 6) == 1 && b.ok(1, 7) == 0x000b && b.ok(1, 16) == 0x00b2, "capability bits mismatch");
    // DataInfo: 4 words, tag 4, v1.0.
    require(b.ok(27, 0) == 4 && b.ok(27, 1) == 4 && b.ok(27, 2) == 1 && b.ok(27, 3) == 0, "data info mismatch");
    b.err(27, 4, 0, 2);
    b.err(27, 0, 1, 3);
    // Reject read/freeze/commit in invalid states.
    b.err(25, 0, 0, 4);
    b.err(26, 0, 1, 4);
    b.err(24, 0, 0, 4);
    b.ok(24, 0, 1); // Begin succeeds.
    // Partial commit rejected.
    b.ok(26, 0, 2);
    b.ok(26, 1, 2);
    b.ok(26, 2, 0);
    b.err(24, 0, 2, 4);
    // Out-of-range args rejected at Commit without losing staging.
    b.ok(26, 3, 1); // reserved1 != 0
    b.err(24, 0, 2, 3);
    require(b.dut.video_card == 0 && b.dut.mono_tint == 0, "failed commit modified outputs");
    b.ok(26, 3, 0);
    b.ok(26, 0, 3); // video_card > 2
    b.err(24, 0, 2, 3);
    b.ok(26, 0, 2);
    b.ok(26, 1, 3); // mono_tint > 2
    b.err(24, 0, 2, 3);
    // Valid commit: Hercules (2), Amber (2).
    b.ok(26, 1, 2);
    b.ok(24, 0, 2);
    require(b.dut.video_card == 2 && b.dut.mono_tint == 2, "commit did not update video_card/mono_tint");
    b.err(26, 0, 0, 4); // write after commit closes staging
    b.err(24, 0, 2, 4); // second commit without begin
    // Release execution -> freeze & read back.
    b.ok(2, 0, 1);
    require(b.dut.exec_reset == 0, "execution did not release");
    b.err(24, 0, 1, 4); // begin while running rejected
    b.ok(24, 0, 0);     // freeze
    b.ok(24, 0, 0);     // idempotent freeze
    require(b.ok(25, 0) == 2 && b.ok(25, 1) == 2 && b.ok(25, 2) == 0 && b.ok(25, 3) == 0, "snapshot readback");
    b.err(25, 4, 0, 2);
    b.ok(24, 0, 3); // resume drops snapshot
    b.err(25, 0, 0, 4);
    b.ok(24, 0, 0); // freeze again before hold
    b.ok(2, 0, 0);  // hold drops snapshot on hold
    require(b.dut.exec_reset == 1, "execution did not hold");
    b.err(25, 0, 0, 4);
    std::cout << "PASS pcxt_machine_settings mailbox\n";
    return 0;
}
