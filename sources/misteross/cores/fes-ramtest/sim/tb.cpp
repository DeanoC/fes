// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vbench.h"
#include "Vbench_bench.h"
#include "Vbench_top.h"
#include "Vbench_cyclonev_hps_interface_mpu_general_purpose.h"
#include "verilated.h"
#include <cstdint>
#include <iostream>

static void require(bool ok, const char* message) {
    if (!ok) {
        std::cerr << message << '\n';
        std::exit(1);
    }
}

static void tick(Vbench& top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
    require(!top.ddr_violation, "HPS DDR model saw a protocol or address violation");
}

static uint32_t command(Vbench& top, bool& toggle, unsigned opcode, unsigned index, unsigned argument) {
    const uint32_t fields = (opcode << 24) | (index << 16) | argument;
    auto* mailbox = top.bench->dut->hps_gp;
    mailbox->gpo = fields | (static_cast<uint32_t>(toggle) << 31);
    top.eval();
    tick(top);
    toggle = !toggle;
    mailbox->gpo = fields | (static_cast<uint32_t>(toggle) << 31);
    top.eval();
    for (int i = 0; i < 16 && ((mailbox->observed & 0x00800000u) != 0u) != toggle; ++i)
        tick(top);
    require(((mailbox->observed & 0x00800000u) != 0u) == toggle, "fes.application ACK timeout");
    require((mailbox->observed & 0x00400000u) == 0u, "fes.application rejected the command");
    return mailbox->observed & 0x0000ffffu;
}

static void report(Vbench& top) {
    auto* dut = top.bench->dut;
    std::cerr << "sdram pass=" << int(dut->sdram_pass) << " fail=" << int(dut->sdram_fail)
              << " errors=" << dut->sdram_errors << '\n';
    const int done[3] = {dut->ddr0_done, dut->ddr1_done, dut->ddr2_done};
    const int nack[3] = {dut->ddr0_nack, dut->ddr1_nack, dut->ddr2_nack};
    const uint32_t errors[3] = {dut->ddr0_errors, dut->ddr1_errors, dut->ddr2_errors};
    for (int port = 0; port < 3; ++port)
        std::cerr << "ddr" << port << " done=" << done[port] << " nack=" << nack[port]
                  << " errors=" << errors[port] << '\n';
}

static bool ddr_done(Vbench& top) {
    auto* dut = top.bench->dut;
    return dut->ddr0_done && dut->ddr1_done && dut->ddr2_done;
}

static bool ddr_clean(Vbench& top) {
    auto* dut = top.bench->dut;
    return !dut->ddr0_nack && !dut->ddr1_nack && !dut->ddr2_nack
        && dut->ddr0_errors == 0 && dut->ddr1_errors == 0 && dut->ddr2_errors == 0;
}

// Each 64-bit lane at byte address a ends the scan holding {~a, a}.
static void require_signature(Vbench& top, uint32_t address) {
    top.peek_slot = ((address >> 27) & 7u) << 9 | ((address >> 3) & 0x1ffu);
    top.eval();
    const uint64_t expected = (static_cast<uint64_t>(~address) << 32) | address;
    require(top.peek_data == expected, "HPS DDR lane does not hold the ADDR signature");
}

int main() {
    Vbench top;
    top.FPGA_CLK1_50 = 0;
    top.peek_slot = 0;
    top.eval();
    bool toggle = false;
    require(command(top, toggle, 1, 0, 0) == 0x4546, "fes.application magic mismatch");
    require(command(top, toggle, 1, 7, 0) == 0x0103, "video, gamepad and HPS DDR capabilities missing");
    require(command(top, toggle, 2, 0, 1) == 0, "execution release failed");
    bool both = false;
    bool green = false;
    for (int i = 0; i < 30000000 && !green; ++i) {
        tick(top);
        both = top.bench->dut->sdram_pass && !top.bench->dut->sdram_fail
            && top.bench->dut->sdram_errors == 0 && ddr_done(top) && ddr_clean(top);
        const uint32_t pixel = top.HDMI_TX_D;
        if (both && top.HDMI_TX_DE && ((pixel >> 8) & 0xff) == 0xc0 && ((pixel >> 16) & 0xff) == 0x20)
            green = true;
    }
    if (!both)
        report(top);
    require(both, "memory scan did not pass");
    require(green, "HDMI did not show a passing status");
    require(!top.ddr_open, "an HPS DDR write burst was left open");
    for (uint32_t base : {0x20000000u, 0x30000000u, 0x38000000u})
        for (uint32_t offset : {0x0u, 0x8u, 0xff8u})
            require_signature(top, base + offset);

    // Hold execution mid-scan at varied points until a guard has finished a
    // write burst and drained reads issued before a hold. The restarted scan
    // must still pass with every burst complete.
    bool finished = false;
    bool drained = false;
    for (int round = 0; round < 40 && !(finished && drained); ++round) {
        require(command(top, toggle, 2, 0, 0) == 0, "execution hold failed");
        for (int i = 0; i < 8; ++i) {
            tick(top);
            finished = finished || top.ddr_finishing;
            drained = drained || top.ddr_draining_reads;
        }
        require(command(top, toggle, 2, 0, 1) == 0, "execution release failed");
        for (int i = 0; i < 97 + round * 131; ++i)
            tick(top);
    }
    require(finished, "no hold landed inside a write burst");
    require(drained, "no hold landed while reads were in flight");
    bool again = false;
    for (int i = 0; i < 2000000 && !again; ++i) {
        tick(top);
        again = ddr_done(top);
    }
    if (!again || !ddr_clean(top))
        report(top);
    require(again && ddr_clean(top), "HPS DDR scan did not pass after mid-scan holds");
    require(!top.ddr_open, "an HPS DDR write burst was left open after a hold");

    require(command(top, toggle, 3, 0, 0x10) == 0, "gamepad button was rejected");
    bool stopped = false;
    for (int i = 0; i < 64 && !stopped; ++i) {
        tick(top);
        auto* dut = top.bench->dut;
        stopped = dut->sdram_stopped && dut->ddr0_stopped && dut->ddr1_stopped && dut->ddr2_stopped;
    }
    require(stopped, "button did not stop the scan");
    std::cout << "PASS: SDRAM and three HPS DDR port scans, signatures, holds, status text, button stop\n";
    return 0;
}
