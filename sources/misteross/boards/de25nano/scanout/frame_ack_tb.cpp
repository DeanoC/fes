#include "Vfes_menu_video.h"
#include "Vfes_menu_video___024root.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>

static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL: %s\n", message); std::exit(1); }
}
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_menu_video t;
    t.clk = 0; t.rst = 1; t.enable = 0; t.quiesce = 0;
    t.window_base = 0; t.window_base_valid = 0;
    t.raster_h = 0; t.raster_v = 0; t.waitrequest = 0;
    t.readdatavalid = 0; t.response = 0; t.submit_valid = 0;
    for (int i = 0; i < 4; ++i) { t.clk = 0; t.eval(); t.clk = 1; t.eval(); }
    t.rst = 0; t.enable = 1;
    bool initial_accepted = false, late_accepted = false;
    unsigned since_late = 0;
    for (unsigned cycle = 0; cycle < 6000000; ++cycle) {
        t.clk = 0; t.submit_valid = 0; t.eval();
        if (!initial_accepted) {
            t.submit_valid = 1; t.submit_slot = 1; t.submit_sequence = 1;
        } else if (!late_accepted && t.displayed_sequence == 1 &&
                   t.rootp->fes_menu_video__DOT__internal_h == 1279 &&
                   t.rootp->fes_menu_video__DOT__internal_v == 719) {
            // The old frame has no pending submission. Its retained
            // pending_sequence still equals fetch_sequence on this edge.
            t.submit_valid = 1; t.submit_slot = 0; t.submit_sequence = 2;
            check(t.submit_ready, "last-visible-pixel submission must be accepted");
        }
        t.eval();
        const bool accepted = t.submit_valid && t.submit_ready;
        if (accepted) {
            if (!initial_accepted) initial_accepted = true;
            else { late_accepted = true; since_late = 0; }
        }
        t.clk = 1; t.eval();
        check(t.underflows == 0, "normal complete-frame test must not underflow");
        if (late_accepted) ++since_late;
        if (t.displayed_sequence == 2) {
            check(late_accepted && since_late >= 1650U * 720U,
                  "completion must wait for a complete new frame");
            check(t.current_sequence == 2 && t.current_slot == 0,
                  "current and completed identities must agree after full frame");
            std::puts("PASS exact last-visible-pixel submit survives prior full-frame ACK and completes next frame");
            return 0;
        }
    }
    check(false, "accepted last-visible-pixel submission was lost or never completed");
}
