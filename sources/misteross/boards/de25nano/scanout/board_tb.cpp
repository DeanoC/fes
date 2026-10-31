#include "Vfes_de25_scanout_board.h"
#include "Vfes_de25_scanout_board___024root.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL: %s\n", message); std::exit(1); }
}
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_de25_scanout_board t;
    auto lock = [&](bool value) {
        t.rootp->fes_de25_scanout_board__DOT__video_clock__DOT__locked = value;
        t.eval();
    };
    auto tick_system = [&]() {
        t.system_clk = 0; t.CLOCK0_50 = 0; t.eval();
        t.system_clk = 1; t.CLOCK0_50 = 1; t.eval();
    };
    auto tick_pixel = [&]() {
        t.rootp->fes_de25_scanout_board__DOT__video_clock__DOT__outclk_0 = 0;
        t.eval();
        t.rootp->fes_de25_scanout_board__DOT__video_clock__DOT__outclk_0 = 1;
        t.eval();
    };
    unsigned blank_checks = 0;
    auto blank = [&]() {
        ++blank_checks;
        if (t.HDMI_TX_D || t.HDMI_TX_DE || t.HDMI_TX_HS || t.HDMI_TX_VS) {
            std::fprintf(stderr, "blank check %u: D=%06x DE=%u HS=%u VS=%u\n", blank_checks,
                         t.HDMI_TX_D, t.HDMI_TX_DE, t.HDMI_TX_HS, t.HDMI_TX_VS);
            check(false, "pixel-domain boundary did not blank HDMI");
        }
    };

    t.ninit_done = 1; t.top_reset_n = 0; t.HDMI_TX_INT = 1;
    t.video_rgb = 0x123456; t.video_de = 0; t.video_hs = 1; t.video_vs = 0;
    t.eval(); lock(false);
    for (int i = 0; i < 4; ++i) { tick_system(); tick_pixel(); blank(); check(!t.pixel_reset_n && !t.system_reset_n, "cold domain reset"); }
    t.ninit_done = 0; t.top_reset_n = 1;
    for (int i = 0; i < 4; ++i) { tick_system(); tick_pixel(); blank(); check(!t.pixel_reset_n && !t.system_reset_n, "wait first lock"); }
    lock(true);
    tick_system(); check(!t.pixel_reset_n && !t.system_reset_n, "first qualifying edge cannot release resets");
    tick_system(); check(!t.pixel_reset_n && !t.system_reset_n, "first lock must be captured after qualification");
    tick_system(); check(!t.pixel_reset_n && !t.system_reset_n, "domain resets assert until their local synchronizers run");
    tick_system(); check(!t.pixel_reset_n && !t.system_reset_n, "system reset waits for its second local edge");
    tick_system(); check(t.system_reset_n && !t.pixel_reset_n && !t.clock_fault,
                         "100 MHz reset releases only after two system-clock edges");
    tick_pixel(); check(!t.pixel_reset_n, "pixel reset waits for its second local edge");
    tick_pixel(); check(t.pixel_reset_n && t.system_reset_n,
                        "pixel reset releases only after two pixel-clock edges");

    // The request is synchronized first. It remains hidden through the active
    // HS pulse and becomes visible only in the following horizontal porch.
    for (int i = 0; i < 4; ++i) { tick_pixel(); blank(); }
    t.video_hs = 0; tick_pixel();
    check(t.HDMI_TX_D == 0x123456 && !t.HDMI_TX_DE && !t.HDMI_TX_HS && !t.HDMI_TX_VS,
          "visible request was not applied after the sync pulse");
    t.video_de = 1; t.eval();
    check(t.HDMI_TX_DE && t.HDMI_TX_D == 0x123456, "visible active video did not pass");

    // A short asynchronous fault request between pixel edges must not feed a
    // multi-domain visibility expression directly to the HDMI data/DE pins.
    t.rootp->fes_de25_scanout_board__DOT__fault = 1;
    t.eval();
    check(t.HDMI_TX_DE && t.HDMI_TX_D == 0x123456,
          "an unsampled visibility request changed HDMI data/DE between pixels");
    t.rootp->fes_de25_scanout_board__DOT__fault = 0;
    t.eval();

    // A mid-sync visibility loss waits through the complete HS and VS pulse,
    // then takes effect at an HS trailing edge with VS low.
    t.video_de = 1; t.video_hs = 1; t.video_vs = 1; tick_pixel();
    check(t.HDMI_TX_HS && t.HDMI_TX_VS && t.HDMI_TX_DE,
          "visible HS/VS/data setup did not start");
    t.top_reset_n = 0; t.eval();
    check(!t.HDMI_TX_DE && !t.HDMI_TX_D && t.HDMI_TX_HS && t.HDMI_TX_VS,
          "asynchronous reset did not blank data while preserving sync");
    for (int i = 0; i < 4; ++i) {
        tick_pixel();
        check(!t.HDMI_TX_DE && !t.HDMI_TX_D && t.HDMI_TX_HS && t.HDMI_TX_VS,
              "asynchronous control clipped sync or exposed reset pixel data");
    }
    t.video_de = 0;
    t.video_hs = 0; tick_pixel();
    check(!t.HDMI_TX_HS && t.HDMI_TX_VS, "HS pulse was not passed through to its trailing edge");
    t.video_hs = 1; tick_pixel();
    check(t.HDMI_TX_HS && t.HDMI_TX_VS, "VS pulse ended before its complete line");
    t.video_hs = 0; t.video_vs = 0; tick_pixel();
    blank();

    // Initial lock loss still latches in the system domain and never resets
    // the live bridge epoch. Its HDMI mask follows the same safe boundary.
    t.top_reset_n = 1; t.video_de = 1; t.video_hs = 1; t.video_vs = 1; t.eval();
    lock(true);
    for (int i = 0; i < 4; ++i) tick_system();
    for (int i = 0; i < 4; ++i) { tick_pixel(); blank(); }
    tick_system();
    check(t.pixel_reset_n && t.system_reset_n, "both domain resets release after cold restart");
    t.video_hs = 0; t.video_vs = 0; tick_pixel();
    check(t.HDMI_TX_D == 0x123456, "visibility did not recover after cold reset");
    t.video_de = 1; t.video_hs = 1; t.video_vs = 1; tick_pixel();
    check(t.HDMI_TX_HS && t.HDMI_TX_VS && t.HDMI_TX_DE,
          "sync/data pulse setup before lock fault");
    t.system_clk = 0; t.eval(); lock(false);
    check(!t.HDMI_TX_DE && !t.HDMI_TX_D && t.HDMI_TX_HS && t.HDMI_TX_VS,
          "PLL loss did not blank data while preserving sync");
    check(t.pixel_reset_n && t.system_reset_n, "PLL loss must not erase live CDC/accounting");
    lock(true);
    check(t.pixel_reset_n && t.system_reset_n, "relock must not reset bridges");
    tick_system(); check(t.clock_fault, "brief loss must latch on running system clock");
    for (int i = 0; i < 8; ++i) {
        tick_system();
        check(t.pixel_reset_n && t.system_reset_n && t.clock_fault, "sticky loss must not reset live bridges");
    }
    for (int i = 0; i < 4; ++i) {
        tick_pixel();
        check(!t.HDMI_TX_DE && !t.HDMI_TX_D && t.HDMI_TX_HS && t.HDMI_TX_VS,
              "fault clipped sync or exposed pixel data in the middle of sync");
    }
    t.video_hs = 0; tick_pixel();
    check(!t.HDMI_TX_HS && t.HDMI_TX_VS, "HS did not finish before fault blanking");
    t.video_hs = 1; tick_pixel();
    t.video_hs = 0; t.video_vs = 0; tick_pixel();
    blank();

    t.top_reset_n = 0; t.eval();
    check(!t.pixel_reset_n && !t.system_reset_n && !t.clock_fault, "cold reset alone clears monitor epoch");
    std::puts("PASS board clock/lock simulation: pixel-synchronized HDMI gating changes only in horizontal porch; complete sync pulses preserved; modeled PLL boundary only");
}
