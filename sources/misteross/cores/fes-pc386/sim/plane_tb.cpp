// SPDX-License-Identifier: MIT
// pc386 DDR port 2 bench: the shared display plane reading through
// pc386_plane_bridge and pc386_port2 beside the presenter's reads and capture
// write bursts (sim/plane_top.sv), on one pc386_ddr_model.h port.
//
//   - The closed plane passes the presenter's 720p picture unchanged; the
//     plane reports the picture rectangle (160, 0, 960, 720).
//   - Opened on launcher frames in DDR slots 0 and 1 (alpha 128, then 255),
//     every pixel is the premultiplied "over" of the launcher pixel on the
//     presenter's, as fes_display_plane computes it.
//   - Closed again, the picture returns unchanged.
//   - A machine reset in mid-frame leaves the 720p syncs running (every line
//     1650 pixel clocks, every frame 1650 x 750) and the plane locked; the
//     picture returns unchanged after the next capture.
// All the while 16-beat capture bursts go to capture buffer 2. Neither the
// presenter nor the plane may underflow, and the plane must not fault.
//
// Prints PASS/FAIL lines; exit 0 when everything passes.
#include "Vplane_top.h"
#include "pc386_ddr_model.h"
#include "pc386_presenter_model.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

namespace {

constexpr uint64_t SYS_PERIOD_PS = 10000;
constexpr uint64_t PIXEL_HALF_PS = 6734;

uint32_t over(uint32_t machine, uint32_t plane, unsigned alpha) {
    unsigned weight = 256 - alpha - (alpha >> 7);
    uint32_t out = 0;
    for (unsigned shift = 0; shift < 24; shift += 8) {
        unsigned m = (machine >> shift) & 0xff, p = (plane >> shift) & 0xff;
        unsigned sum = p + ((m * weight) >> 8);
        out |= (sum > 255 ? 255 : sum) << shift;
    }
    return out;
}

// Launcher pixel at a raster position: premultiplied, a function of the slot.
uint32_t launcher(unsigned x, unsigned y, unsigned slot, unsigned alpha) {
    unsigned r = (x * 3 + y) & 0xff, g = (y * 5) & 0xff, b = slot ? 0x77 : 0x22;
    auto scale = [&](unsigned v) { return v * alpha / 255; };
    return alpha << 24 | scale(r) << 16 | scale(g) << 8 | scale(b);
}

class Bench {
public:
    Bench() {
        top.reset = 1;
        top.pixel_locked = 0;
        for (int n = 0; n < 16; ++n) step();
        top.reset = 0;
        top.pixel_locked = 1;
    }
    Vplane_top top;
    pc386::Window window;
    pc386::Port ddr{window, 0x5eed0002, "port 2"};
    pc386::Random random{7};
    uint64_t now = 0, pixel_edge = 0, cycles = 0, capture_beats = 0;
    unsigned capture_left = 0, capture_index = 0;
    pc386::Frame raster{1280, 720, std::vector<uint32_t>(1280 * 720)};
    pc386::Frame last;
    unsigned x = 0, y = 0, complete = 0;
    bool started = false, last_de = false, last_vs = false, last_hs = false;
    uint64_t pixels = 0, hs_at = 0, vs_at = 0, sync_breaks = 0;

    void step() {
        // Capture bursts: 16 beats to buffer 2, started at random.
        if (!top.capture_write && !top.reset && (random.next() & 63) == 0) {
            top.capture_write = 1;
            top.capture_burstcount = 16;
            capture_left = 16;
            top.capture_address = (pc386::WINDOW_BASE + pc386::CAPTURE_OFFSET + 2 * pc386::CAPTURE_BYTES) / 8 +
                                  (capture_index++ * 16) % 0x3ff00;
            top.capture_writedata = (uint64_t(random.next()) << 32) | random.next();
        }
        pc386::PortSignals port{&top.p2_address, &top.p2_burstcount, &top.p2_read, &top.p2_write,
                                &top.p2_writedata, &top.p2_byteenable, &top.p2_waitrequest, &top.p2_readdata,
                                &top.p2_readdatavalid};
        // The capture inputs changed since the last eval; the port sees this clock's command.
        top.eval();
        ddr.drive(port, cycles);
        top.eval();
        bool beat_taken = top.capture_write && !top.capture_waitrequest;
        now += SYS_PERIOD_PS;
        while (pixel_edge <= now) {
            top.pixel_clk = !top.pixel_clk;
            top.eval();
            if (top.pixel_clk) observe();
            pixel_edge += PIXEL_HALF_PS;
        }
        top.clk = 1;
        top.eval();
        top.clk = 0;
        top.eval();
        ++cycles;
        if (beat_taken) {
            ++capture_beats;
            if (--capture_left == 0) top.capture_write = 0;
            top.capture_writedata = (uint64_t(random.next()) << 32) | random.next();
        }
    }
    void observe() {
        uint32_t word = top.video;
        bool de = word & (1u << 24), hs = word & (1u << 25), vs = word & (1u << 26);
        ++pixels;
        if (hs && !last_hs) {
            sync_breaks += hs_at && pixels - hs_at != 1650;
            hs_at = pixels;
        }
        if (vs && !last_vs) {
            sync_breaks += vs_at && pixels - vs_at != 1650 * 750;
            vs_at = pixels;
        }
        last_hs = hs;
        if (vs && !last_vs) {
            if (started && y == 720) { last = raster; ++complete; }
            started = true;
            y = 0;
        }
        if (de && !last_de) x = 0;
        if (de && started && y < 720 && x < 1280) raster.pixels[y * 1280 + x] = word & 0xffffff;
        if (de) ++x;
        if (!de && last_de) ++y;
        last_de = de;
        last_vs = vs;
    }
    bool frames(unsigned count) {
        unsigned target = complete + count;
        for (uint64_t limit = cycles + uint64_t(count + 2) * 1700000; cycles < limit;) {
            step();
            if (complete >= target) return true;
        }
        return false;
    }
    unsigned command(unsigned opcode, unsigned index = 0, unsigned argument = 0) {
        top.request = 1; top.opcode = opcode; top.index = index; top.argument = argument;
        for (unsigned n = 0; n < 4000000 && !top.response_valid; ++n) step();
        unsigned result = top.response_valid ? (unsigned(top.response_error) << 16 | top.response_data) : 0xdead;
        top.request = 0;
        for (unsigned n = 0; n < 1000 && top.response_valid; ++n) step();
        return result;
    }
    void fill_slot(unsigned slot, unsigned alpha) {
        for (unsigned row = 0; row < 720; ++row)
            for (unsigned column = 0; column < 1280; ++column) {
                uint32_t value = launcher(column, row, slot, alpha);
                std::memcpy(window.at(slot * 0x400000 + (row * 1280 + column) * 4), &value, 4);
            }
    }
    bool submit(unsigned sequence, unsigned slot) {
        if (command(21, 0, sequence & 0xffff) || command(21, 1, sequence >> 16) || command(21, 2, slot)) return false;
        for (unsigned n = 0; n < 8000000 && top.displayed_sequence != sequence; ++n) step();
        for (unsigned n = 0; n < 4000000 && !top.ui_active; ++n) step();
        return top.displayed_sequence == sequence && top.ui_active;
    }
};

bool report(bool ok, const std::string &what) {
    std::printf("%s %s\n", ok ? "PASS" : "FAIL", what.c_str());
    return ok;
}

unsigned compare(const pc386::Frame &got, const pc386::Frame &want) {
    unsigned differing = 0;
    for (size_t n = 0; n < want.pixels.size(); ++n) differing += got.pixels[n] != want.pixels[n];
    return differing;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    Bench bench;
    bool ok = true;
    // A 320 x 400 capture in buffer 0.
    pc386::Frame source{320, 400, std::vector<uint32_t>(320 * 400)};
    for (unsigned row = 0; row < 400; ++row)
        for (unsigned column = 0; column < 320; ++column) {
            unsigned v = (column + 2 * row) & 63, w = (v << 2) | (v >> 4);
            source.pixels[row * 320 + column] = w << 16 | ((63 - v) << 2 | (63 - v) >> 4) << 8 | 0x82;
        }
    for (unsigned row = 0; row < 400; ++row)
        std::memcpy(bench.window.at(pc386::CAPTURE_OFFSET + row * pc386::CAPTURE_STRIDE), &source.pixels[row * 320],
                    320 * 4);
    bench.top.frame_buffer = 0;
    bench.top.frame_width = 320;
    bench.top.frame_height = 400;
    bench.step();
    bench.top.frame_sequence = 1;
    pc386::Frame picture = pc386::present_720p(source);

    for (unsigned n = 0; n < 4 && !bench.top.locked; ++n) bench.frames(1);
    ok &= report(bench.top.locked, "plane locks to the presenter's 720p raster");
    bench.frames(1);
    ok &= report(compare(bench.last, picture) == 0, "closed plane passes the presenter's picture unchanged");
    ok &= report(bench.command(18, 14) == 160 && bench.command(18, 15) == 0 && bench.command(18, 16) == 960 &&
                 bench.command(18, 17) == 720, "plane reports the picture rectangle (160, 0, 960, 720)");
    ok &= report(bench.command(19, 0, 1) == 0 && bench.command(20, 0, 1) == 0, "plane configured and enabled");

    const unsigned alphas[] = {0x80, 0xff};
    for (unsigned slot = 0; slot < 2; ++slot) {
        bench.fill_slot(slot, alphas[slot]);
        bool shown = bench.submit(slot + 1, slot);
        ok &= report(shown, "launcher frame " + std::to_string(slot + 1) + " shown from slot " + std::to_string(slot));
        bench.frames(1);
        pc386::Frame want{1280, 720, std::vector<uint32_t>(1280 * 720)};
        for (unsigned row = 0; row < 720; ++row)
            for (unsigned column = 0; column < 1280; ++column) {
                uint32_t plane = launcher(column, row, slot, alphas[slot]);
                want.pixels[row * 1280 + column] = over(picture.at(column, row), plane & 0xffffff, plane >> 24);
            }
        unsigned differing = compare(bench.last, want);
        ok &= report(differing == 0, "alpha " + std::to_string(alphas[slot]) + " over the picture: " +
                                         std::to_string(differing) + " pixels differ");
    }
    ok &= report(bench.command(20, 0, 0) == 0, "plane closed");
    for (unsigned n = 0; n < 4000000 && bench.top.ui_active; ++n) bench.step();
    bench.frames(2);
    ok &= report(compare(bench.last, picture) == 0, "closed plane returns the picture unchanged");
    ok &= report(bench.top.present_underflows == 0 && bench.top.plane_underflows == 0 && !bench.top.faulted,
                 "no presenter or plane underflow, no plane fault (capture beats written: " +
                     std::to_string(bench.capture_beats) + ")");

    // A machine reset of 1,000 system clocks a third of the way into a frame.
    uint64_t breaks = bench.sync_breaks;
    for (unsigned n = 0; n < 1700000 / 3; ++n) bench.step();
    bench.top.reset = 1;
    for (unsigned n = 0; n < 1000; ++n) bench.step();
    bench.top.reset = 0;
    bench.frames(1);
    bench.top.frame_sequence = 2;
    bench.frames(2);
    ok &= report(bench.sync_breaks == breaks && bench.hs_at && bench.vs_at,
                 "720p syncs run through a machine reset (" + std::to_string(bench.sync_breaks - breaks) +
                     " broken periods)");
    ok &= report(bench.top.locked && !bench.top.faulted, "plane stays locked through a machine reset");
    ok &= report(compare(bench.last, picture) == 0 && bench.top.present_underflows == 0,
                 "picture returns unchanged after the reset, no presenter underflow");
    return ok ? 0 : 1;
}
