#include "Vfes_menu_video.h"
#include "Vfes_menu_video___024root.h"
#include "generated/fes_de25_scanout.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>

static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL: %s\n", message); std::exit(1); }
}
static void first_underflow_is_retained() {
    Vfes_menu_video t;
    t.clk = 0; t.rst = 1; t.enable = 0; t.quiesce = 0;
    t.window_base = 0; t.window_base_valid = 0;
    t.raster_h = 0; t.raster_v = 0; t.waitrequest = 0;
    t.readdatavalid = 0; t.response = 0; t.submit_valid = 0;
    const auto edge = [&]() { t.clk = 1; t.eval(); t.clk = 0; t.eval(); };
    t.eval(); edge(); t.rst = 0; t.enable = 1; t.eval();
    bool reached = false;
    for (unsigned cycle = 0; cycle < 100000; ++cycle) {
        if (t.rootp->fes_menu_video__DOT__state == 3 &&
            t.rootp->fes_menu_video__DOT__internal_h == 17 &&
            t.rootp->fes_menu_video__DOT__internal_v == 0) { reached = true; break; }
        edge();
    }
    check(reached && t.underflows == 0, "first-only test needs a healthy active frame");
    // Inject an index discontinuity in the actual ordered reader. Unlike the
    // allocated endpoint, this generic video keeps running after an underflow,
    // allowing a second, different fault predicate to exercise first-only.
    t.rootp->fes_menu_video__DOT__reader__DOT__index = 18;
    t.eval();
    const auto count = t.rootp->fes_menu_video__DOT__reader__DOT__count;
    const auto outstanding = t.rootp->fes_menu_video__DOT__reader__DOT__outstanding;
    const auto lane = t.rootp->fes_menu_video__DOT__reader__DOT__lane;
    edge();
    check(t.underflows == 1, "future-index pixel did not underflow");
    const auto state = t.first_fault_state, occupancy = t.first_fault_occupancy, gaps = t.first_fault_gaps;
    check(state & FES_DE25_SCANOUT_FIRST_FAULT_VALID_MASK, "first-only record is absent");
    check(((state >> FES_DE25_SCANOUT_FIRST_FAULT_HORIZONTAL_SHIFT) & FES_DE25_SCANOUT_FIRST_FAULT_HORIZONTAL_MASK) == 17,
          "first-only record lost initial horizontal coordinate");
    check((state & FES_DE25_SCANOUT_FIRST_FAULT_PIXEL_VALID_MASK) &&
          ((state >> FES_DE25_SCANOUT_FIRST_FAULT_INDEX_RELATION_SHIFT) & FES_DE25_SCANOUT_FIRST_FAULT_INDEX_RELATION_MASK) ==
          FES_DE25_SCANOUT_FIRST_FAULT_INDEX_RELATION_GREATER, "future-index relation was not captured");
    check(count > 0 && lane == 1 && occupancy == (count | (outstanding << FES_DE25_SCANOUT_FIRST_FAULT_OUTSTANDING_SHIFT) |
          (lane << FES_DE25_SCANOUT_FIRST_FAULT_LANE_SHIFT)), "buffered first-fault occupancy/lane is not pre-edge state");
    t.rootp->fes_menu_video__DOT__reader__DOT__index = 17;
    t.eval(); edge();
    check(t.underflows == 2, "second distinct missing pixel did not underflow");
    check(t.first_fault_state == state && t.first_fault_occupancy == occupancy && t.first_fault_gaps == gaps,
          "later underflow overwrote the first record");
    t.enable = 0; t.quiesce = 1; t.eval();
    for (unsigned cycle = 0; cycle < 2000; ++cycle) edge();
    check(t.quiesced, "first-only test did not drain");
    check(t.first_fault_state == state && t.first_fault_occupancy == occupancy && t.first_fault_gaps == gaps,
          "quiesce/drain erased the first record");
    t.rst = 1; t.eval(); edge();
    check(!t.first_fault_state && !t.first_fault_occupancy && !t.first_fault_gaps, "reset retained first-only record");
    t.final();
    std::puts("PASS first underflow retains pre-edge buffered state across later distinct faults and drain");
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
            t.final();
            first_underflow_is_retained();
            return 0;
        }
    }
    check(false, "accepted last-visible-pixel submission was lost or never completed");
}
