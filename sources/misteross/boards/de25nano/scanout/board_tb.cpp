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
    auto tick = [&]() {
        t.system_clk = 0; t.CLOCK0_50 = 0; t.eval();
        t.system_clk = 1; t.CLOCK0_50 = 1; t.eval();
    };
    auto blank = [&]() {
        check(!t.HDMI_TX_D && !t.HDMI_TX_DE && !t.HDMI_TX_HS && !t.HDMI_TX_VS,
              "fault/configuration must blank HDMI immediately");
    };
    t.ninit_done = 1; t.top_reset_n = 0; t.HDMI_TX_INT = 1;
    t.video_rgb = 0x123456; t.video_de = t.video_hs = t.video_vs = 1;
    t.eval(); lock(false);
    for (int i = 0; i < 4; ++i) { tick(); blank(); check(!t.scanout_reset_n, "cold bridge reset"); }
    t.ninit_done = 0; t.top_reset_n = 1;
    for (int i = 0; i < 4; ++i) { tick(); blank(); check(!t.scanout_reset_n, "wait first lock"); }
    lock(true);
    tick(); check(!t.scanout_reset_n, "first qualifying edge cannot release bridges");
    tick(); check(!t.scanout_reset_n, "first lock must be captured after qualification");
    tick(); check(t.scanout_reset_n && !t.clock_fault, "initial qualified bridge release");
    check(t.HDMI_TX_D == 0x123456 && t.HDMI_TX_DE, "initial visible video");
    // No system or pixel edge during this short lock-loss/relock pulse. The
    // qualifier stretches it; bridge reset must remain deasserted throughout.
    t.system_clk = 0; t.eval(); lock(false); blank();
    check(t.scanout_reset_n, "PLL loss must not erase live CDC/accounting");
    lock(true); blank();
    check(t.scanout_reset_n, "relock must not reset bridges");
    tick(); blank(); check(t.clock_fault, "brief loss must latch on running system clock");
    for (int i = 0; i < 8; ++i) { tick(); blank(); check(t.scanout_reset_n && t.clock_fault, "sticky loss without bridge reset"); }
    t.top_reset_n = 0; t.eval(); blank();
    check(!t.scanout_reset_n && !t.clock_fault, "cold reset alone clears monitor epoch");
    std::puts("PASS actual board startup/brief PLL-loss/relock blanking and sticky fault without resetting live bridges; modeled PLL boundary only");
}
