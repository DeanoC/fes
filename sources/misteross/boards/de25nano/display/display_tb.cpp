// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vde25nano_display.h"
#include "Vde25nano_display___024root.h"
#include "verilated.h"
#include <cstdint>
#include <cstdio>
#include <cstdlib>

static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL: %s\n", message); std::exit(1); }
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vde25nano_display t;
    t.HDMI_TX_INT = 1;
    auto tick = [&]() {
        t.CLOCK0_50 = 0;
        t.rootp->de25nano_display__DOT__video_clock__DOT__outclk_0 = 0;
        t.eval();
        t.CLOCK0_50 = 1;
        t.rootp->de25nano_display__DOT__video_clock__DOT__outclk_0 = 1;
        t.eval();
    };
    auto blank = [&]() {
        check(!t.HDMI_TX_DE && !t.HDMI_TX_HS && !t.HDMI_TX_VS && !t.HDMI_TX_D,
              "video must be blank during configuration/unlock");
    };
    t.eval();
    for (int i = 0; i < 10; ++i) { tick(); blank(); }
    t.rootp->de25nano_display__DOT__configuration_release__DOT__ninit_done = 0;
    for (int i = 0; i < 10; ++i) { tick(); blank(); }
    t.rootp->de25nano_display__DOT__video_clock__DOT__locked = 1;
    t.eval(); blank();
    tick(); blank(); // First qualifying edge cannot release the output.
    tick();

    bool last_vs = false, green = false, magenta = false, returned = false;
    unsigned frames = 0, frame_active = 0, frame_hs = 0, frame_vs = 0;
    uint64_t last_frame = 0, frame_hash = 0, previous_hash = 0;
    bool moving = false, saw_letters = false, saw_ring = false;
    const uint64_t clocks = uint64_t(1650) * 750 * (MENU_PATTERN ? 123 : 4);
    for (uint64_t cycle = 0; cycle < clocks; ++cycle) {
        tick();
        check(t.rootp->de25nano_display__DOT__underflows == 0, "menu scanout underflow");
        check(!(t.HDMI_MCLK || t.HDMI_SCLK || t.HDMI_LRCLK || t.HDMI_I2S), "audio outputs must stay silent");
        if (t.HDMI_TX_VS && !last_vs) {
            if (frames > 0) {
                check(cycle - last_frame == uint64_t(1650) * 750, "720p frame period");
                check(frame_active == 1280 * 720 && frame_hs == 40 * 750 && frame_vs == 5 * 1650,
                      "720p active/sync dimensions");
                if (frames > 1 && frame_hash != previous_hash) moving = true;
            }
            previous_hash = frame_hash;
            frame_hash = 0;
            frame_active = frame_hs = frame_vs = 0;
            last_frame = cycle;
            ++frames;
        }
        last_vs = t.HDMI_TX_VS;
        frame_active += t.HDMI_TX_DE;
        frame_hs += t.HDMI_TX_HS;
        frame_vs += t.HDMI_TX_VS;
        frame_hash = frame_hash * 33 + t.HDMI_TX_D;
        if (!t.HDMI_TX_DE) check(t.HDMI_TX_D == 0, "blanking RGB must be black");
#if MENU_PATTERN
        if (t.HDMI_TX_DE && t.rootp->de25nano_display__DOT__menu__DOT__scanout__DOT__internal_h == 2 &&
            t.rootp->de25nano_display__DOT__menu__DOT__scanout__DOT__internal_v == 2) {
            if (t.HDMI_TX_DE && t.HDMI_TX_D == 0x00ff00) { green = true; if (magenta) returned = true; }
            if (t.HDMI_TX_DE && t.HDMI_TX_D == 0xff00ff) magenta = true;
        }
#else
        {
            saw_letters |= t.HDMI_TX_D == 0xecf4f8;
            saw_ring |= t.HDMI_TX_D == 0x40bad2;
        }
#endif
    }
    if (MENU_PATTERN) check(frames >= 120 && green && magenta && returned && moving, "60-frame pattern submissions");
    else check(frames >= 3 && saw_letters && saw_ring && moving, "FES idle logo and frame motion");
    // Assert without any new pixel/reference edge, then relock with a stopped
    // clock and verify that output cannot release until clock edges resume.
    t.rootp->de25nano_display__DOT__video_clock__DOT__locked = 0;
    t.eval(); blank();
    t.rootp->de25nano_display__DOT__video_clock__DOT__locked = 1;
    t.eval(); blank();
    tick(); blank();
    tick();
    t.rootp->de25nano_display__DOT__configuration_release__DOT__ninit_done = 1;
    t.eval(); blank();
    std::printf("PASS DE25 %s: 720p timing, pixels/motion, no underflow, configuration/PLL gating; digital boundary models only\n",
                MENU_PATTERN ? "menu-pattern" : "idle");
}
