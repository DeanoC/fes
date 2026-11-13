// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsg1000_application_gp.h"
#include "verilated.h"

#include <cstdlib>
#include <iostream>

static void require(bool ok, const char *message) {
    if (!ok) {
        std::cerr << "SG-1000 application media: " << message << '\n';
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
    auto tick = [&]() { dut.clk = 1; dut.eval(); dut.clk = 0; dut.eval(); };
    bool toggle = false;
    auto exchange = [&](unsigned opcode, unsigned index, unsigned argument) {
        toggle = !toggle;
        dut.gpo = (unsigned(toggle) << 31) | (opcode << 24) | (index << 16) | argument;
        for (unsigned i = 0; i < 16; ++i) {
            tick();
            if (bool(uint32_t(dut.gpi) & 0x00800000u) == toggle)
                return uint32_t(dut.gpi);
        }
        require(false, "missing mailbox response");
        return uint32_t(0);
    };
    auto ok = [&](unsigned opcode, unsigned index, unsigned argument) {
        uint32_t response = exchange(opcode, index, argument);
        require((response & 0x00400000u) == 0, "unexpected rejection");
        return response & 0xffffu;
    };
    require(ok(1, 7, 0) == 0x36, "diagnostic media capability");
    require((exchange(4, 0, 16385) & 0x00400000u) != 0, "oversized upload rejected");
    require((exchange(2, 0, 1) & 0x00400000u) != 0 && dut.exec_reset,
            "release rejected before upload");
    auto expected = [](unsigned i) { return uint8_t(0x40 ^ i ^ (i >> 8)); };
    for (unsigned count : {1u, 3u, 256u, 16384u}) {
        ok(2, 0, 0);
        ok(4, 0, count);
        for (unsigned i = 0; i + 1 < count; i += 2)
            ok(5, 0, unsigned(expected(i)) | (unsigned(expected(i + 1)) << 8));
        if (count & 1) ok(5, 1, expected(count - 1));
        ok(6, 0, 0);
        require(dut.media_ready && dut.media_size == count, "committed length");
        for (unsigned i = 0; i < count; ++i) {
            dut.media_addr = i;
            tick();
            require(dut.media_q == expected(i), "staged cartridge byte");
        }
        ok(2, 0, 1);
        require(!dut.exec_reset, "release after commit");
    }
    std::cout << "SG-1000 diagnostic application media staging checks passed\n";
    return EXIT_SUCCESS;
}
