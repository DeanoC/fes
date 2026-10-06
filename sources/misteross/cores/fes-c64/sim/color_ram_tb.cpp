// SPDX-License-Identifier: MIT
#include "Vc64_color_ram.h"
#include "verilated.h"

#include <cstdio>

int main() {
    Vc64_color_ram top;
    top.clk_a = 0;
    top.clk_b = 0;
    top.addr_a = 0;
    top.addr_b = 0;
    top.wdata_a = 0;
    top.we_a = 0;
    top.eval();

    auto tick_a = [&]() {
        top.clk_a = 1;
        top.eval();
        top.clk_a = 0;
        top.eval();
    };
    auto tick_b = [&]() {
        top.clk_b = 1;
        top.eval();
        top.clk_b = 0;
        top.eval();
    };
    auto fail = [&](const char *message) {
        std::printf("c64 color RAM failed: %s (q_a=%x q_b=%x)\n",
                    message, top.q_a, top.q_b);
        return 1;
    };

    top.addr_a = 0;
    top.wdata_a = 5;
    top.we_a = 1;
    tick_a();
    top.we_a = 0;
    tick_a();
    if (top.q_a != 5) return fail("system-port write/read");

    top.addr_a = 999;
    top.wdata_a = 10;
    top.we_a = 1;
    tick_a();
    top.we_a = 0;
    tick_a();
    if (top.q_a != 10) return fail("last valid address");

    top.addr_a = 0;
    top.eval();
    if (top.q_a != 10) return fail("read changed before clock");
    tick_a();
    if (top.q_a != 5) return fail("registered system-port read");

    top.addr_b = 999;
    tick_b();
    if (top.q_b != 10) return fail("independent video-port read");

    top.addr_a = 1000;
    top.wdata_a = 15;
    top.we_a = 1;
    tick_a();
    top.we_a = 0;
    tick_a();
    if (top.q_a != 0) return fail("invalid system address did not read zero");

    top.addr_b = 1000;
    tick_b();
    if (top.q_b != 0) return fail("invalid video address did not read zero");

    top.addr_a = 0;
    tick_a();
    if (top.q_a != 5) return fail("invalid write aliased valid storage");

    std::puts("c64 color RAM passed");
    return 0;
}
