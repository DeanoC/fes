// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsg1000_application_gp.h"
#include "verilated.h"

#include <cstdlib>
#include <iostream>

static void require(bool condition, const char *message) {
    if (!condition) {
        std::cerr << "SG-1000 application mailbox: " << message << '\n';
        std::exit(EXIT_FAILURE);
    }
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vsg1000_application_gp dut;
    dut.clk = 0;
    dut.gpo = 0;
    dut.media_addr = 0;
    dut.eval();
    require(dut.exec_reset && !dut.controller_buttons, "initial reset and neutral controllers");
    bool toggle = false;
    auto exchange = [&](unsigned opcode, unsigned index, unsigned argument) {
        toggle = !toggle;
        dut.gpo = (unsigned(toggle) << 31) | (opcode << 24) | (index << 16) | argument;
        for (unsigned i = 0; i < 16; ++i) {
            dut.clk = 1; dut.eval();
            dut.clk = 0; dut.eval();
            if (bool(uint32_t(dut.gpi) & 0x00800000u) == toggle)
                return uint32_t(dut.gpi);
        }
        require(false, "missing mailbox response");
        return uint32_t(0);
    };
    auto ok = [&](unsigned op, unsigned index, unsigned argument) {
        uint32_t response = exchange(op, index, argument);
        require((response & 0x00400000u) == 0, "unexpected rejection");
        return response & 0xffffu;
    };
    require(ok(1, 4, 0) == 3, "application ABI tag");
    require(ok(1, 7, 0) == 0x32, "ROM-linked video/audio/controller capabilities");
    require(ok(13, 0, 0x11) == 0 && ok(13, 1, 0x18) == 0, "controller commands");
    require(dut.controller_buttons == 0x1811, "independent controller state");
    require((exchange(13, 2, 1) & 0x00400000u) != 0, "invalid controller port rejected");
    require((exchange(14, 0, 1) & 0x00400000u) != 0, "keypad absent");
    require((exchange(4, 0, 1) & 0x00400000u) != 0, "ROM-linked media absent");
    require(dut.controller_buttons == 0x1811, "rejections preserve controllers");
    ok(2, 0, 1);
    require(!dut.exec_reset, "ROM-linked release without upload");
    ok(2, 0, 0);
    require(dut.exec_reset && !dut.controller_buttons, "hold clears controllers");
    std::cout << "SG-1000 ROM-linked application mailbox checks passed\n";
    return EXIT_SUCCESS;
}
