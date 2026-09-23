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

int main() {
    Vbench top;
    top.FPGA_CLK1_50 = 0;
    top.eval();
    bool toggle = false;
    require(command(top, toggle, 1, 0, 0) == 0x4546, "fes.application magic mismatch");
    require(command(top, toggle, 1, 7, 0) == 0x0002, "video capability missing");
    require(command(top, toggle, 2, 0, 1) == 0, "execution release failed");
    bool both = false;
    bool green = false;
    for (int i = 0; i < 1500000 && !green; ++i) {
        tick(top);
        both = top.bench->dut->sdram_pass && top.bench->dut->hps_pass
            && !top.bench->dut->sdram_fail && !top.bench->dut->hps_fail;
        const uint32_t pixel = top.HDMI_TX_D;
        if (both && top.HDMI_TX_DE && ((pixel >> 8) & 0xff) == 0xc0 && ((pixel >> 16) & 0xff) == 0x20)
            green = true;
    }
    require(both, "memory march did not pass");
    require(green, "HDMI did not show a passing bar");
    std::cout << "PASS: fes.application release, SDRAM and HPS bars\n";
    return 0;
}
