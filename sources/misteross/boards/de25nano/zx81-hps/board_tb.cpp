// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_zx81_board.h"
#include "Vfes_de25_zx81_board___024root.h"
#include "verilated.h"

#include <cstdio>
#include <cstdlib>
#include <algorithm>
#include <cstdint>

static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL: %s\n", message); std::exit(1); }
}

static void reset_and_sync_boundaries() {
    Vfes_de25_zx81_board t;
    auto lock = [&](bool value) {
        t.rootp->fes_de25_zx81_board__DOT__video_clock__DOT__locked = value;
        t.eval();
    };
    auto configured = [&](bool value) {
        t.rootp->fes_de25_zx81_board__DOT__hdmi_config__DOT__configured = value;
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
        check(!t.video_ready, "blanked/reset video was advertised ready");
    };

    t.CLOCK0_50 = 0; t.ninit_done = 1; t.HDMI_TX_INT = 1;
    t.video_rgb = 0x123456; t.video_de = 1; t.video_hs = 0; t.video_vs = 0;
    t.eval(); lock(false); configured(false);
    for (int i = 0; i < 4; ++i) { tick(); black(); }

    // Release on an arbitrary pixel. Data stays blank until an entire HS pulse
    // has finished and the line gate reaches its porch boundary.
    t.ninit_done = 0; lock(true); t.eval();
    for (int i = 0; i < 6; ++i) { tick(); black(); }
    t.video_hs = 1;
    for (int i = 0; i < 4; ++i) { tick(); black(); }
    t.video_hs = 0; tick();
    tick(); // Output pins add one aligned pixel-clock stage.
    check(t.HDMI_TX_DE && t.HDMI_TX_D == 0x123456,
          "initial video did not become visible after a complete sync pulse");

    check(!t.video_ready, "video became ready before HDMI configuration");
    configured(true);
    tick(); check(!t.video_ready, "HDMI completion bypassed its synchronizer");
    tick(); check(!t.video_ready, "HDMI completion bypassed its registered source");
    tick(); check(t.video_ready, "configured full-line video was not ready");
    configured(false);
    tick(); tick(); tick(); check(!t.video_ready, "HDMI completion loss stayed ready");
    configured(true);
    tick(); tick(); tick(); check(t.video_ready, "HDMI completion did not recover");

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
    tick();
    check(t.HDMI_TX_DE && t.HDMI_TX_D == 0x123456,
          "video did not resume at the next complete line boundary");
    check(!t.video_ready, "video readiness bypassed the source register at the complete line");
    tick(); check(!t.video_ready, "pixel activity bypassed the return synchronizer");
    tick(); check(!t.video_ready, "pixel activity bypassed the registered reference source");
    tick(); check(t.video_ready, "video readiness did not recover after the complete line");

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
    std::puts("PASS DE25 ZX81 board: reset/PLL blanking, synchronized HDMI completion and line-safe readiness; digital boundaries only");
}

// External clock/fault stimulus only: DUT state is never rewritten.
struct DigitalClock {
    uint64_t frequency, next, remainder = 0;
    bool level = false, running = true;
    void advance() {
        const uint64_t half_second_fs = 500000000000000ull;
        next += half_second_fs / frequency;
        remainder += half_second_fs % frequency;
        if (remainder >= frequency) { ++next; remainder -= frequency; }
    }
};

struct Rig {
    Vfes_de25_zx81_board t;
    DigitalClock ref, pix;
    uint64_t ref_edges = 0, pix_edges = 0;
    bool previous_pixel_hs = false, is_locked = false;
    unsigned visible_rises = 0;
    Rig(uint64_t phase): ref{50000000, 10000000}, pix{74250000, phase + 1} {
        t.CLOCK0_50 = 0; t.ninit_done = 1; t.HDMI_TX_INT = 1;
        t.video_rgb = 0x123456; t.video_de = 1; t.video_hs = 0; t.video_vs = 0;
        t.eval(); lock(false); configured(false);
        events(60);
        t.ninit_done = 0; t.eval(); lock(true); configured(true);
        train();
    }
    void configured(bool value) {
        t.rootp->fes_de25_zx81_board__DOT__hdmi_config__DOT__configured = value;
        t.eval();
    }
    void black() {
        check(!t.HDMI_TX_DE && !t.HDMI_TX_D && !t.video_ready,
              "fault or stopped rearm exposed pixels/readiness");
    }
    void lock(bool value) {
        is_locked = value;
        bool hs = t.HDMI_TX_HS, vs = t.HDMI_TX_VS;
        t.rootp->fes_de25_zx81_board__DOT__video_clock__DOT__locked = value;
        t.eval();
        if (!value) {
            black();
            check(bool(t.HDMI_TX_HS) == hs && bool(t.HDMI_TX_VS) == vs,
                  "asynchronous lock loss clipped a sync pulse");
        }
    }
    void step() {
        uint64_t now = std::min(ref.next, pix.next);
        bool old_visible = t.HDMI_TX_DE, old_hs = t.HDMI_TX_HS, old_vs = t.HDMI_TX_VS;
        bool old_input_hs = t.video_hs, old_input_vs = t.video_vs;
        bool pixel_rise = false;
        if (ref.next == now) {
            if (ref.running) {
                ref.level = !ref.level; t.CLOCK0_50 = ref.level;
                if (ref.level) ++ref_edges;
            }
            ref.advance();
        }
        if (pix.next == now) {
            if (pix.running) {
                pix.level = !pix.level;
                t.rootp->fes_de25_zx81_board__DOT__video_clock__DOT__outclk_0 = pix.level;
                pixel_rise = pix.level;
                if (pixel_rise) ++pix_edges;
                else {
                    unsigned position = pix_edges % 16;
                    t.video_hs = position >= 4 && position < 8;
                    t.video_vs = (pix_edges / 16) % 16 >= 12;
                }
            }
            pix.advance();
        }
        bool boundary = pixel_rise && previous_pixel_hs && !t.video_hs && !t.video_vs;
        t.eval();
        if (!old_visible && t.HDMI_TX_DE) {
            check(boundary, "pixels re-enabled outside the complete HS trailing edge");
            ++visible_rises;
        }
        if (old_hs && !t.HDMI_TX_HS)
            check(old_input_hs && !t.video_hs, "an output HS pulse was shortened");
        if (!old_hs && t.HDMI_TX_HS)
            check(!old_input_hs && t.video_hs, "an output HS pulse started mid-pulse");
        if (old_vs && !t.HDMI_TX_VS)
            check(old_input_vs && !t.video_vs, "an output VS pulse was shortened");
        if (!old_vs && t.HDMI_TX_VS)
            check(!old_input_vs && t.video_vs, "an output VS pulse started mid-pulse");
        check(!t.video_ready || t.HDMI_TX_DE, "ready preceded visible output (DE input is held high)");
        if (pixel_rise) previous_pixel_hs = t.video_hs;
        if (!is_locked) black();
    }
    void events(unsigned count) { while (count--) step(); }
    void refs(unsigned count) {
        check(ref.running, "reference clock is stopped");
        uint64_t end = ref_edges + count;
        for (unsigned n = 0; ref_edges < end; ++n) { check(n < 20000, "reference progress bound"); step(); }
    }
    void pixels(unsigned count) {
        check(pix.running, "pixel clock is stopped");
        uint64_t end = pix_edges + count;
        for (unsigned n = 0; pix_edges < end; ++n) { check(n < 20000, "pixel progress bound"); step(); }
    }
    void train() {
        ref.running = pix.running = true;
        for (unsigned n = 0; !t.video_ready || !t.HDMI_TX_DE; ++n) {
            check(n < 10000, "visibility/readiness did not recover with both clocks running"); step();
        }
    }
    void sync_high(bool vertical) {
        for (unsigned n = 0; !t.HDMI_TX_HS || (vertical && !t.HDMI_TX_VS); ++n) {
            check(n < 10000, "sync pulse setup bound"); step();
        }
    }
    void stopped_rearm(bool stop_reference_first, bool vertical) {
        train(); sync_high(vertical);
        ref.running = !stop_reference_first; pix.running = false;
        lock(false); events(40); black();
        lock(true); events(40); black();
        // A short loss can occur with both clocks stopped. A new reference-
        // domain qualification must not reuse the old visible-pixel reply.
        ref.running = pix.running = false;
        lock(false); lock(true); events(20); black();
        ref.running = true;
        for (unsigned fault = 0; fault < 8; ++fault) {
            lock(false); refs(3); black();
            lock(true); refs(8); black();
        }
        // Let only the pixel side run, then pause it again. Recovery is allowed
        // here only if the unchanged whole-HS/VS and readiness invariants hold.
        ref.running = false; pix.running = true;
        pixels(48);
        lock(false); pixels(8); black(); lock(true); pixels(48);
        train();
        std::printf("PASS digital stopped rearm: reference-first-stop=%u active-VS=%u; visibility-rises=%u\n",
                    unsigned(stop_reference_first), unsigned(vertical), visible_rises);
    }
};

static uint32_t next_random(uint32_t &state) {
    state ^= state << 13; state ^= state >> 17; state ^= state << 5;
    return state;
}
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    reset_and_sync_boundaries();
    for (uint64_t phase : {0ull, 77ull, 999999ull}) {
        Rig rig(phase);
        rig.stopped_rearm(false, false);
        rig.stopped_rearm(true, false);
        rig.stopped_rearm(true, true);
    }
    for (unsigned seed = 1; seed <= 96; ++seed) {
        Rig rig(seed * 77);
        uint32_t random = 0x9e3779b9u * seed;
        for (unsigned i = 0; i < 4096; ++i) {
            uint32_t value = next_random(random);
            if ((value & 31) == 0) rig.lock(false);
            if ((value & 31) == 1) rig.lock(true);
            rig.ref.running = (value & 32) != 0;
            rig.pix.running = (value & 64) != 0;
            rig.step();
        }
        rig.lock(true); rig.train();
    }
    std::puts("PASS DE25 board: 50/74.25MHz, nine stopped-clock cases, 96 fault schedules, whole sync pulses and eventual recovery; digital only");
    return 0;
}
