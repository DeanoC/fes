// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_zx81_board.h"
#include "Vfes_de25_zx81_board___024root.h"
#include "verilated.h"

#include <cstdio>
#include <cstdlib>

static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL: %s\n", message); std::exit(1); }
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_de25_zx81_board t;
    auto lock = [&](bool value) {
        t.rootp->fes_de25_zx81_board__DOT__video_clock__DOT__locked = value;
        t.eval();
    };
    auto tick = [&]() {
        t.CLOCK0_50 = 0;
        t.rootp->fes_de25_zx81_board__DOT__video_clock__DOT__outclk_0 = 0;
        t.eval();
        t.CLOCK0_50 = 1;
        t.rootp->fes_de25_zx81_board__DOT__video_clock__DOT__outclk_0 = 1;
        t.eval();
    };
    auto black = [&]() {
        check(!t.HDMI_TX_DE && !t.HDMI_TX_D, "pixel data was not blanked");
    };

    t.CLOCK0_50 = 0; t.ninit_done = 1; t.HDMI_TX_INT = 1;
    t.video_rgb = 0x123456; t.video_de = 1; t.video_hs = 0; t.video_vs = 0;
    t.eval(); lock(false);
    for (int i = 0; i < 4; ++i) { tick(); black(); }

    // Release on an arbitrary pixel. Data stays blank until an entire HS pulse
    // has finished and the line gate reaches its porch boundary.
    t.ninit_done = 0; lock(true); t.eval();
    for (int i = 0; i < 6; ++i) { tick(); black(); }
    t.video_hs = 1;
    for (int i = 0; i < 4; ++i) { tick(); black(); }
    t.video_hs = 0; tick();
    check(t.HDMI_TX_DE && t.HDMI_TX_D == 0x123456,
          "initial video did not become visible after a complete sync pulse");

    // Lock loss blanks pixels immediately but leaves an in-flight HS pulse
    // intact. The active line is hidden only at the falling sync edge.
    t.video_hs = 1; tick();
    check(t.HDMI_TX_HS, "active HS pulse did not reach HDMI");
    lock(false);
    black();
    check(t.HDMI_TX_HS, "asynchronous PLL loss clipped HDMI HS");
    for (int i = 0; i < 4; ++i) {
        tick(); black();
        check(t.HDMI_TX_HS, "lock loss clipped an active HS pulse");
    }
    t.video_hs = 0; tick();
    check(!t.HDMI_TX_HS, "HS remained asserted after its input trailing edge");

    // Relock during active video must not expose a mid-line fragment.
    lock(true);
    for (int i = 0; i < 6; ++i) { tick(); black(); }
    t.video_hs = 1;
    for (int i = 0; i < 4; ++i) { tick(); black(); }
    t.video_hs = 0; tick();
    check(t.HDMI_TX_DE && t.HDMI_TX_D == 0x123456,
          "video did not resume at the next complete line boundary");

    // Configuration reset uses the same data/sync boundary behavior.
    t.video_hs = 1; tick();
    check(t.HDMI_TX_HS, "HS pulse setup before configuration reset");
    t.ninit_done = 1; t.eval();
    black();
    check(t.HDMI_TX_HS, "asynchronous configuration reset clipped HDMI HS");
    for (int i = 0; i < 3; ++i) {
        tick(); black();
        check(t.HDMI_TX_HS, "configuration reset clipped an active HS pulse");
    }
    t.video_hs = 0; tick();
    check(!t.HDMI_TX_HS, "configuration reset did not hide the next line");
    std::puts("PASS DE25 ZX81 board: reset/PLL data blanking and line-safe HDMI resume; digital PLL boundary only");
    return 0;
}
