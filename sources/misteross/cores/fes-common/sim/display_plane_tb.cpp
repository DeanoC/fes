// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vdisplay_plane_top.h"
#include "Vdisplay_plane_top___024root.h"
#include <verilated.h>
#include <cstdint>
#include <cstdlib>
#include <deque>
#include <iostream>
#include <string>

static void check(bool condition, const std::string &message) {
    if (!condition) { std::cerr << "display plane: " << message << '\n'; std::exit(1); }
}

struct Word { uint32_t value; unsigned h, v; };

// Premultiplied "over" exactly as the plane computes it.
static uint32_t over(uint32_t machine, uint32_t plane, unsigned alpha) {
    const unsigned weight = 256 - alpha - (alpha >> 7);
    uint32_t out = 0;
    for (unsigned shift = 0; shift < 24; shift += 8) {
        const unsigned m = (machine >> shift) & 0xff, p = (plane >> shift) & 0xff;
        unsigned sum = p + ((m * weight) >> 8);
        out |= (sum > 255 ? 255 : sum) << shift;
    }
    return out;
}

// The DDR model's pixel at a raster position: R=index[7:0], G=index[15:8], B=slot colour.
static uint32_t plane_rgb(unsigned h, unsigned v, unsigned slot) {
    const unsigned index = v * 1280 + h;
    return (index & 0xff) << 16 | ((index >> 8) & 0xff) << 8 | (slot ? 0x77 : 0x22);
}

struct Bench {
    Vdisplay_plane_top t;
    uint64_t sys_phase = 17000000;
    std::deque<Word> history;
    enum Mode { UNCHECKED, MACHINE, BLENDED } mode = UNCHECKED;
    unsigned slot = 0, alpha = 0;
    uint64_t checked = 0, blended = 0;
    Bench() {
        t.clk_sys = 0; t.pixel_clk = 0; t.reset_hold = 1; t.exec_reset = 0; t.request = 0;
        t.eval();
        for (unsigned i = 0; i < 100; ++i) tick();
        t.reset_hold = 0;
    }
    uint8_t &model_alpha() {
        return t.rootp->display_plane_top__DOT__memory__DOT__f2sdram__DOT__alpha;
    }
    void tick() {
        // Unrelated 52.224/74.25 MHz clocks.
        sys_phase += 52224000;
        if (sys_phase >= 74250000) {
            sys_phase -= 74250000;
            t.clk_sys = 0; t.eval(); t.clk_sys = 1; t.eval();
        }
        history.push_back({t.machine, t.machine_h, t.machine_v});
        if (history.size() > 3) history.pop_front();
        t.pixel_clk = 0; t.eval(); t.pixel_clk = 1; t.eval();
        check(!t.rootp->display_plane_top__DOT__memory__DOT__f2sdram__DOT__violation,
              "DDR bounds, writes or unused-port violation");
        if (history.size() < 3) return;
        // The word captured two edges ago is on the output now: three clocks.
        const Word &in = history.front();
        check((t.video >> 24) == (in.value >> 24), "sync/CE did not keep the fixed latency");
        if (mode == MACHINE) {
            check(t.video == in.value, "closed or faulted plane altered machine pixels");
            ++checked;
        } else if (mode == BLENDED) {
            const bool de = in.value >> 24 & 1;
            const uint32_t want = de ? over(in.value & 0xffffff, plane_rgb(in.h, in.v, slot), alpha)
                                     : (in.value & 0xffffff);
            if ((t.video & 0xffffff) != want) {
                std::cerr << "at " << in.h << "," << in.v << " machine=" << std::hex << (in.value & 0xffffff)
                          << " got=" << (t.video & 0xffffff) << " want=" << want << std::dec << '\n';
                check(false, "blended pixel differs from premultiplied over");
            }
            ++checked; blended += de;
        }
    }
    void cycles(unsigned n) { for (unsigned i = 0; i < n; ++i) tick(); }
    void to_frame_start() {
        for (unsigned i = 0; i < 1300000 && (t.machine_h != 0 || t.machine_v != 0); ++i) tick();
    }
    void frame(Mode checking) {
        to_frame_start();
        mode = checking;
        cycles(1650 * 750);
        mode = UNCHECKED;
    }
    unsigned command(unsigned op, unsigned index = 0, unsigned argument = 0) {
        t.request = 1; t.opcode = op; t.index = index; t.argument = argument;
        for (unsigned i = 0; i < 4000000 && !t.response_valid; ++i) tick();
        check(t.response_valid, "GP/CDC response timeout");
        const unsigned result = unsigned(t.response_error) << 16 | t.response_data;
        t.request = 0;
        for (unsigned i = 0; i < 1000 && t.response_valid; ++i) tick();
        check(!t.response_valid, "GP/CDC response did not clear");
        return result;
    }
    void submit(unsigned sequence, unsigned target) {
        check(command(21, 0, sequence & 0xffff) == 0, "sequence low rejected");
        check(command(21, 1, sequence >> 16) == 0, "sequence high rejected");
        check(command(21, 2, target) == 0, "slot commit rejected");
        for (unsigned i = 0; i < 4000000 && t.displayed_sequence != sequence; ++i) tick();
        check(t.displayed_sequence == sequence, "full frame did not complete");
        for (unsigned i = 0; i < 1300000 && !t.ui_active; ++i) tick();
        check(t.ui_active, "completed frame did not become visible");
        slot = target;
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Bench b;
    // Two frames establish the raster coordinate; the closed plane is exact.
    for (unsigned i = 0; i < 3 && !b.t.locked; ++i) b.frame(Bench::UNCHECKED);
    check(b.t.locked, "plane did not lock to the 720p raster");
    b.frame(Bench::MACHINE);

    check(b.command(18, 0) == 1280 && b.command(18, 1) == 720, "plane geometry mismatch");
    check(b.command(18, 7) == 2, "plane did not report premultiplied RGBA");
    check(b.command(18, 14) == 100 && b.command(18, 15) == 60 &&
          b.command(18, 16) == 1080 && b.command(18, 17) == 600, "picture rectangle mismatch");
    check(b.command(18, 18) == 0x10002, "info index past the picture rectangle accepted");
    check(b.command(19, 0, 1) == 0, "layout configuration rejected");
    check(b.command(20, 0, 1) == 0, "enable rejected");
    check(!b.t.ui_active, "enable exposed an uncompleted frame");

    // Opaque plane: alpha 255 is exactly the plane picture.
    b.model_alpha() = 0xff; b.alpha = 0xff;
    b.submit(1, 0);
    b.frame(Bench::BLENDED);
    // Translucent plane over the machine.
    b.model_alpha() = 0x80; b.alpha = 0x80;
    b.submit(2, 1);
    b.frame(Bench::BLENDED);
    b.model_alpha() = 0x40; b.alpha = 0x40;
    b.submit(3, 0);
    b.frame(Bench::BLENDED);
    // Alpha 0 adds the plane's colour: out-of-range premultiplied data saturates.
    b.model_alpha() = 0x00; b.alpha = 0x00;
    b.submit(4, 1);
    b.frame(Bench::BLENDED);
    check(b.t.underflows == 0 && !b.t.faulted, "visible scanout underflowed");
    b.alpha = 0xff; b.model_alpha() = 0xff;
    b.submit(5, 0);

    // Moving the raster breaks the coordinate: the plane faults and returns
    // machine pixels, the runtime closes it, and the raster locks again.
    while (b.t.machine_v != 300 || b.t.machine_h != 400) b.tick();
    // Sync still agrees until this line's DE falls 7 pixels early.
    b.t.rootp->display_plane_top__DOT__h = 407;
    b.cycles(1650);
    check(!b.t.locked && b.t.faulted, "raster mismatch did not unlock and fault the plane");
    b.mode = Bench::MACHINE;
    b.cycles(1650 * 750 * 2);
    b.mode = Bench::UNCHECKED;
    check(!b.t.ui_active, "faulted plane stayed active past the frame boundary");
    check(b.command(20, 0, 0) == 0, "faulted plane could not be closed");
    check(b.command(18, 9) == 25, "closed faulted plane did not report configured/quiesced/faulted");
    for (unsigned i = 0; i < 3 && !b.t.locked; ++i) b.frame(Bench::UNCHECKED);
    check(b.t.locked, "plane did not relock after the raster moved");
    b.frame(Bench::MACHINE);
    check(b.blended >= 4ull * 921600, "too few blended pixels were checked");
    std::cout << "PASS display plane: fixed three-clock latency, raster lock/relock, exact closed plane, "
                 "premultiplied over at alpha 255/128/64/0 with saturation, picture rectangle, fault return ("
              << b.checked << " words checked)\n";
}
