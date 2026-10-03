// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vnative_video_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <deque>
#include <functional>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

// Public native-pixel request grammar, distinct from the timed RGB response.
static constexpr uint32_t VALID = 1u << 24, SOF = 1u << 25, EOL = 1u << 26;
static constexpr uint32_t EOF_BIT = 1u << 27, HOLD = 1u << 28, CONTROL = 1u << 29;
static constexpr uint32_t RESERVED = 1u << 30, DE = 1u << 24, HS = 1u << 25;
static constexpr uint32_t VS = 1u << 26, CE = 1u << 27;
static constexpr unsigned WIDTH = 256, HEIGHT = 192, PIXELS = WIDTH * HEIGHT;
static constexpr uint64_t RASTER = 1650 * 750;
static constexpr uint64_t SOURCE_HZ = 52224000, TOKEN_HZ = 4024320;
static constexpr std::array<uint32_t, 16> PALETTE = {
    0, 0, 0x21c842, 0x5edc78, 0x5455ed, 0x7d76fc, 0xd4524d, 0x42ebf5,
    0xfc5554, 0xff7978, 0xd4c154, 0xe6ce80, 0x21b03b, 0xc95bba, 0xcccccc, 0xffffff
};
using Frame = std::vector<uint8_t>;
static uint64_t checked_cycles = 0, checked_crossings = 0, checked_adapter_pixels = 0;

static void require(bool ok, const std::string &message) {
    if (!ok) throw std::runtime_error(message);
}

static Frame picture(unsigned tag) {
    Frame result(PIXELS);
    for (unsigned y = 0; y < HEIGHT; ++y)
        for (unsigned x = 0; x < WIDTH; ++x)
            result[y * WIDTH + x] = (x + 3 * y + (x >> 3) + (y >> 2) + tag) & 15;
    return result;
}

static uint32_t token(const Frame &frame, unsigned index) {
    return VALID | frame[index] | (index == 0 ? SOF : 0) |
        (index % WIDTH == WIDTH - 1 ? EOL : 0) | (index == PIXELS - 1 ? EOF_BIT : 0);
}

static uint32_t control(bool held) { return VALID | CONTROL | (held ? HOLD : 0); }

class Simulation {
public:
    Vnative_video_top dut;
    uint64_t pixel_edges = 0, source_edges = 0, crossings = 0;
    unsigned faults = 0, adapter_pixels = 0, adapter_controls = 0;
    bool exact_crossing = true, held = true;
    Frame front, pending, eof_expected;
    std::deque<uint32_t> transfers;
    std::function<void(uint32_t)> adapter_check;
    std::function<void(uint32_t)> crossed_check;

    Simulation(unsigned mode = 0) {
        dut.pixel_clock = 0;
        dut.source_clock = 0;
        dut.source_mode = mode;
        dut.native_request = 0;
        dut.source_request = 0;
        dut.source_hold = 1;
        dut.logical_blank = 1;
        dut.logical_x = 0;
        dut.logical_y = 0;
        dut.logical_pixel = 0;
        dut.eval();
    }

    void expect_pending(const Frame &frame) {
        require(pending.empty(), "test scheduled two completed back frames");
        pending = frame;
    }

    // Compare every output cycle against a software raster and full image;
    // no DUT counters, RAM address or capture state enter the oracle.
    void check_pixel(uint32_t consumed) {
        const unsigned x = pixel_edges % 1650;
        const unsigned y = (pixel_edges / 1650) % 750;
        if ((consumed & (VALID | CONTROL)) == (VALID | CONTROL) &&
            !(consumed & ~(VALID | CONTROL | HOLD))) {
            held = consumed & HOLD;
            if (held) pending.clear();
        }
        if (x == 0 && y == 0 && !pending.empty()) {
            front = pending;
            pending.clear();
        }
        uint32_t rgb = 0;
        if (!held && !front.empty() && x >= 384 && x < 896 && y >= 168 && y < 552)
            rgb = PALETTE[front[((y - 168) / 2) * WIDTH + (x - 384) / 2]];
        const uint32_t timing = CE | (x < 1280 && y < 720 ? DE : 0) |
            (x >= 1390 && x < 1430 ? HS : 0) | (y >= 725 && y < 730 ? VS : 0);
        const uint32_t dim = y & 1 ? (rgb & 0xfefefe) >> 1 : rgb;
        if (dut.direct_response != (timing | rgb) || dut.scanlines_response != (timing | dim)) {
            std::cerr << "HDMI mismatch cycle " << pixel_edges << " at " << x << ',' << y
                << " request=0x" << std::hex << consumed
                << " direct=0x" << dut.direct_response << " expected=0x" << (timing | rgb)
                << " scanlines=0x" << dut.scanlines_response << " expected=0x" << (timing | dim)
                << std::dec << '\n';
            throw std::runtime_error("full-raster response oracle failed");
        }
        ++pixel_edges;
        ++checked_cycles;
        // The CDC pulse is consumed at this edge; an EOF completing at HDMI
        // SOF may only become the next output frame, never the current one.
        if ((consumed & (VALID | EOF_BIT)) == (VALID | EOF_BIT) && !eof_expected.empty()) {
            expect_pending(eof_expected);
            eof_expected.clear();
        }
    }

    void step(uint32_t request = 0) {
        require(dut.source_mode == 0, "strict step used on asynchronous path");
        dut.native_request = request;
        dut.pixel_clock = 0;
        dut.eval();
        dut.pixel_clock = 1;
        dut.eval();
        check_pixel(request);
        dut.pixel_clock = 0;
        dut.eval();
    }

    void idle(uint64_t cycles) { for (uint64_t i = 0; i < cycles; ++i) step(); }
    void next_boundary() { idle((RASTER - pixel_edges % RASTER) % RASTER); }
    void show_frame() { next_boundary(); idle(RASTER); }

    void send(const Frame &frame, bool gaps = false) {
        for (unsigned i = 0; i < PIXELS; ++i) {
            // VALID=0 markers/reserved/payload must neither write nor abort.
            if (gaps) {
                step(SOF | EOL | EOF_BIT | RESERVED | 0xffffff);
                if (i % 3 == 0) step();
            }
            step(token(frame, i));
        }
    }

    // Called by the rational two-clock scheduler, including coincident edges.
    void edge(bool source_rise, bool pixel_rise, bool source_toggle, bool pixel_toggle) {
        const uint32_t consumed = dut.source_mode == 0 ? dut.native_request : dut.crossed_request;
        if (source_rise) {
            const uint32_t sent = dut.source_mode == 2 ? dut.adapter_request : dut.source_request;
            if (exact_crossing && (sent & VALID)) transfers.push_back(sent);
            ++source_edges;
        }
        if (source_toggle) dut.source_clock = !dut.source_clock;
        if (pixel_toggle) dut.pixel_clock = !dut.pixel_clock;
        dut.eval();
        if (source_rise && dut.source_mode == 2 && (dut.adapter_request & VALID)) {
            if (dut.adapter_request & CONTROL) ++adapter_controls;
            else { ++adapter_pixels; ++checked_adapter_pixels; }
            if (adapter_check) adapter_check(dut.adapter_request);
        }
        if (pixel_rise) {
            check_pixel(consumed);
            if (dut.crossed_request & VALID) {
                ++crossings;
                if (dut.crossed_request & RESERVED) ++faults;
                if (exact_crossing) {
                    require(!transfers.empty(), "CDC produced an unrequested token");
                    require(dut.crossed_request == transfers.front(), "CDC lost, reordered or changed a token");
                    transfers.pop_front();
                    ++checked_crossings;
                }
                if (crossed_check) crossed_check(dut.crossed_request);
            }
        }
    }
};

// Reduced exact 52.224/74.25 MHz half-periods. Edges are scheduled independently;
// source phases cover coincident and near-opposite sampling relationships.
class Clocks {
    Simulation &sim;
    uint64_t source_time, pixel_time;
public:
    explicit Clocks(Simulation &s, unsigned phase) : sim(s), source_time(phase), pixel_time(0) {}
    void event(const std::function<void(uint64_t)> &source_input = {}) {
        const auto now = source_time < pixel_time ? source_time : pixel_time;
        const bool st = source_time == now, pt = pixel_time == now;
        const bool sr = st && !sim.dut.source_clock, pr = pt && !sim.dut.pixel_clock;
        if (sr && source_input) source_input(sim.source_edges);
        sim.edge(sr, pr, st, pt);
        if (st) source_time += 12375;
        if (pt) pixel_time += 8704;
    }
    void until_pixel(uint64_t target, const std::function<void(uint64_t)> &source_input = {}) {
        while (sim.pixel_edges < target) event(source_input);
    }
};

static void strict_capture_tests() {
    Simulation sim;
    sim.idle(RASTER); // Power-up hold and blank picture, with uninterrupted timing.
    sim.step(control(false));
    const auto first = picture(1), second = picture(5), dropped = picture(9);
    sim.send(first, true);
    sim.expect_pending(first);
    sim.show_frame();

    sim.send(second);
    sim.expect_pending(second);
    sim.send(dropped); // A completed back bank rejects an entire following frame.
    sim.show_frame();
    sim.idle(RASTER); // Rejected frame must not surface one output frame later.

    const auto boundary = picture(4), collision = picture(11);
    sim.send(boundary);
    sim.expect_pending(boundary);
    sim.next_boundary();
    sim.send(collision); // Its SOF collides with the old pending bank's swap.
    sim.show_frame(); // Dropping that SOF rejects the entire collision frame.
    sim.idle(RASTER);

    // Completing the source exactly at HDMI SOF does not publish it early.
    for (unsigned i = 0; i < PIXELS - 1; ++i) sim.step(token(first, i));
    sim.next_boundary();
    sim.step(token(first, PIXELS - 1));
    sim.expect_pending(first);
    sim.show_frame();

    const auto paused = picture(12);
    for (unsigned i = 0; i < PIXELS / 2; ++i) sim.step(token(paused, i));
    sim.idle(2 * RASTER); // A paused incomplete source repeats the whole front.
    for (unsigned i = PIXELS / 2; i < PIXELS; ++i) sim.step(token(paused, i));
    sim.expect_pending(paused);
    sim.show_frame();

    const auto malformed = picture(2);
    const std::array<std::function<uint32_t(uint32_t, unsigned)>, 12> damage = {
        [](uint32_t w, unsigned i) { return i == 0 ? w & ~SOF : w; },
        [](uint32_t w, unsigned i) { return i == 101 ? w | SOF : w; },
        [](uint32_t w, unsigned i) { return i == 99 ? w | EOL : w; },
        [](uint32_t w, unsigned i) { return i == 255 ? w & ~EOL : w; },
        [](uint32_t w, unsigned i) { return i == 120 ? w | EOF_BIT : w; },
        [](uint32_t w, unsigned i) { return i == PIXELS - 1 ? w & ~EOF_BIT : w; },
        [](uint32_t w, unsigned i) { return i == PIXELS - 1 ? w & ~EOL : w; },
        [](uint32_t w, unsigned i) { return i == 513 ? w | RESERVED : w; },
        [](uint32_t w, unsigned i) { return i == 514 ? w | (1u << 31) : w; },
        [](uint32_t w, unsigned i) { return i == 515 ? w | (1u << 4) : w; },
        [](uint32_t w, unsigned i) { return i == 516 ? w | HOLD : w; },
        [](uint32_t w, unsigned i) { return i == 517 ? w | CONTROL : w; }
    };
    for (const auto &mutate : damage) {
        for (unsigned i = 0; i < PIXELS; ++i) sim.step(mutate(token(malformed, i), i));
        sim.show_frame(); // No damaged frame, or portion of one, becomes visible.
    }
    // Missing/duplicated source pixels also put line/frame markers off geometry.
    for (unsigned i = 0; i < PIXELS; ++i) if (i != 78) sim.step(token(malformed, i));
    sim.show_frame();
    for (unsigned i = 0; i < PIXELS; ++i) {
        sim.step(token(malformed, i));
        if (i == 83) sim.step(token(malformed, i));
    }
    sim.show_frame();

    // HOLD clears a pending completed bank as well as a partial capture.
    sim.send(second);
    sim.expect_pending(second);
    sim.step(control(true));
    sim.send(dropped);
    sim.show_frame();
    sim.step(control(false));
    sim.show_frame(); // The previously displayed paused frame remains intact.
    sim.send(second);
    sim.expect_pending(second);
    sim.next_boundary();
    sim.step(control(true)); // HOLD has priority over a simultaneous pending swap.
    sim.show_frame();
    sim.step(control(false));
    sim.show_frame(); // The pending second frame never became the front bank.
    for (unsigned i = 0; i < 1234; ++i) sim.step(token(first, i));
    sim.step(control(true));
    sim.idle(RASTER);
    sim.step(control(false));
    for (unsigned i = 1234; i < PIXELS; ++i) sim.step(token(first, i));
    sim.show_frame(); // RELEASE never resumes an aborted partial capture.
    sim.send(first);
    sim.expect_pending(first);
    sim.show_frame();
    std::cout << "strict native capture: complete/pause/pending/drop/malformed/HOLD passed\n";
}

static void raw_crossing_test(unsigned phase) {
    Simulation sim(1);
    Clocks clocks(sim, phase);
    const auto frame = picture(phase + 3);
    uint64_t accumulator = 0, last = 0;
    unsigned produced = 0, gaps12 = 0, gaps13 = 0;
    auto input = [&](uint64_t edge) {
        sim.dut.source_request = edge == 0 ? control(false) : 0;
        if (edge < 32 || produced == PIXELS) return;
        accumulator += TOKEN_HZ;
        if (accumulator < SOURCE_HZ) return;
        accumulator -= SOURCE_HZ;
        sim.dut.source_request = token(frame, produced);
        if (produced) {
            const auto gap = edge - last;
            require(gap == 12 || gap == 13, "fractional source cadence departed from 12/13 cycles");
            if (gap == 12) ++gaps12; else ++gaps13;
        }
        last = edge;
        ++produced;
    };
    bool completed = false;
    sim.crossed_check = [&](uint32_t request) {
        if (request & EOF_BIT) { sim.eof_expected = frame; completed = true; }
    };
    while (!completed) {
        require(sim.source_edges < PIXELS * 14 + 256, "sparse CDC frame did not deliver EOF");
        clocks.event(input);
    }
    const auto frame_end = ((sim.pixel_edges + RASTER - 1) / RASTER + 1) * RASTER;
    clocks.until_pixel(frame_end, input);
    require(sim.crossings == PIXELS + 1 && sim.transfers.empty() && !sim.faults,
            "sparse source did not cross exactly once per source token");
    require(gaps12 && gaps13, "fractional rate failed to exercise both source spacings");
    std::cout << "CDC phase " << phase << ": " << PIXELS << " pixels at 4.02432 MHz, "
              << gaps12 << " twelve-cycle and " << gaps13 << " thirteen-cycle gaps passed\n";
}

static void adapter_crossing_test(unsigned phase) {
    Simulation sim(2);
    Clocks clocks(sim, phase);
    const auto frame = picture(phase + 7);
    uint64_t accumulator = 0;
    unsigned coordinate = 0, age = 0, emitted = 0, controls = 0;
    bool active = false, finished = false, completed = false;
    sim.adapter_check = [&](uint32_t request) {
        if (request & CONTROL) {
            require(request == control(controls == 0), "adapter changed or duplicated initial HOLD/release");
            ++controls;
        } else {
            require(emitted < PIXELS, "adapter emitted beyond native frame");
            require(request == token(frame, emitted), "adapter sampled unsettled color or wrong geometry");
            ++emitted;
        }
    };
    sim.crossed_check = [&](uint32_t request) {
        if (request & EOF_BIT) { sim.eof_expected = frame; completed = true; }
    };
    auto input = [&](uint64_t edge) {
        sim.dut.source_hold = edge < 16;
        sim.dut.logical_blank = !active;
        if (edge < 32 || finished) { sim.dut.logical_blank = 1; return; }
        if (!active) { active = true; age = 0; }
        sim.dut.logical_blank = 0;
        sim.dut.logical_x = coordinate % WIDTH;
        sim.dut.logical_y = coordinate / WIDTH;
        // Coordinate and RAM/color stages deliberately disagree for two edges.
        sim.dut.logical_pixel = age < 2 ? (frame[coordinate] + age + 5) & 15 : frame[coordinate];
        ++age;
        accumulator += TOKEN_HZ;
        if (accumulator >= SOURCE_HZ) {
            accumulator -= SOURCE_HZ;
            if (++coordinate == PIXELS) { finished = true; active = false; }
            age = 0;
        }
    };
    while (!completed) {
        require(sim.source_edges < PIXELS * 15, "adapter did not complete the native frame");
        clocks.event(input);
    }
    const auto frame_end = ((sim.pixel_edges + RASTER - 1) / RASTER + 1) * RASTER;
    clocks.until_pixel(frame_end, input);
    require(emitted == PIXELS && controls == 2 && sim.adapter_controls == 2,
            "adapter emitted more or fewer tokens than the source frame");
    require(sim.crossings == PIXELS + 2 && sim.transfers.empty() && !sim.faults,
            "adapter/CDC changed the source frame or control tokens");
    std::cout << "Coleco adapter phase " << phase << ": initial/transition HOLD, settling and full frame passed\n";
}

static void overrun_test() {
    Simulation sim(1);
    Clocks clocks(sim, 8703);
    const auto good = picture(3), bad = picture(8);
    unsigned stage = 0, position = 0;
    uint64_t accumulator = 0, start = 0;
    bool completed = false, control_arrived = false, proper_hold_sent = false;
    auto input = [&](uint64_t edge) {
        sim.dut.source_request = 0;
        if (edge == 0) sim.dut.source_request = control(false);
        if (edge < 32) return;
        if (stage == 0) {
            accumulator += TOKEN_HZ;
            if (accumulator >= SOURCE_HZ) {
                accumulator -= SOURCE_HZ;
                sim.dut.source_request = token(good, position++);
                if (position == PIXELS) { stage = 1; position = 0; start = edge; }
            }
        } else if (stage == 1 && edge - start > 2 * SOURCE_HZ / 60) {
            stage = 2;
            sim.exact_crossing = false;
            sim.dut.source_request = token(bad, position++);
        } else if (stage == 2) {
            // An unsupported continuous source overruns the one-held-token CDC.
            sim.dut.source_request = token(bad, position++);
            if (position == 504) sim.dut.source_request = control(true) | RESERVED;
            if (position == 508) sim.dut.source_request = control(true) | SOF;
            if (position == 512) sim.dut.source_request = control(true) | 15;
            if (position == PIXELS) { stage = 3; start = edge; }
        } else if (stage == 3) {
            const auto delay = edge - start;
            // Drain malformed controls before sending a proper one: a later
            // desired level must not hide accidental CONTROL canonicalization.
            if (delay == 1) sim.dut.source_request = control(true) | RESERVED;
            if (delay == 2) sim.dut.source_request = control(true) | 15;
            if (delay == 3) sim.dut.source_request = control(true) | SOF;
            if (delay == 48) require(!sim.held, "malformed CONTROL changed HOLD after drain");
            if (delay == 49) sim.dut.source_request = control(false);
            if (delay == 50) {
                sim.dut.source_request = control(true);
                proper_hold_sent = true;
            }
            // The final HOLD collides with an outstanding release and must
            // arrive even as continuous overspeed VALID traffic keeps coming.
            if (delay > 50 && delay <= 600) sim.dut.source_request = VALID | bad[delay % PIXELS];
        }
    };
    sim.crossed_check = [&](uint32_t request) {
        if (stage <= 1 && (request & EOF_BIT)) { sim.eof_expected = good; completed = true; }
        if (request == control(true)) {
            require(proper_hold_sent, "CDC canonicalized a malformed CONTROL into HOLD");
            control_arrived = true;
        }
    };
    while (!completed) {
        require(sim.source_edges < PIXELS * 14 + 256, "initial overrun-test frame did not deliver EOF");
        clocks.event(input);
    }
    while (stage < 3 || sim.source_edges - start < 178) {
        require(sim.source_edges < PIXELS * 64, "overspeed test did not reach final control");
        clocks.event(input);
    }
    require(sim.faults > 0, "CDC overrun silently lost pixels without an invalid token");
    require(control_arrived && sim.held, "CDC lost the final HOLD control during overrun");
    clocks.until_pixel(sim.pixel_edges + RASTER, input);
    // Release and send a fresh well-paced frame. Invalid partial data must not
    // have changed the retained front, and subsequent capture must recover.
    const auto release_edge = sim.source_edges;
    auto release = [&](uint64_t edge) { sim.dut.source_request = edge == release_edge ? control(false) : 0; };
    clocks.until_pixel(sim.pixel_edges + RASTER, release);
    require(!sim.held && sim.front == good, "overrun/HOLD corrupted the retained complete front");
    sim.transfers.clear();
    sim.exact_crossing = true;
    position = 0;
    accumulator = 0;
    completed = false;
    sim.crossed_check = [&](uint32_t request) {
        if (request & EOF_BIT) { sim.eof_expected = bad; completed = true; }
    };
    auto recover = [&](uint64_t) {
        sim.dut.source_request = 0;
        if (position == PIXELS) return;
        accumulator += TOKEN_HZ;
        if (accumulator >= SOURCE_HZ) {
            accumulator -= SOURCE_HZ;
            sim.dut.source_request = token(bad, position++);
        }
    };
    const auto recover_start = sim.source_edges;
    while (!completed) {
        require(sim.source_edges - recover_start < PIXELS * 14 + 256,
                "CDC did not recover a complete frame after overrun/HOLD");
        clocks.event(recover);
    }
    clocks.until_pixel(((sim.pixel_edges + RASTER - 1) / RASTER + 1) * RASTER, recover);
    require(sim.transfers.empty(), "recovered frame still has outstanding source tokens");
    std::cout << "CDC overrun: explicit malformed token, final HOLD delivery and fresh-frame recovery passed\n";
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    try {
        strict_capture_tests();
        for (unsigned phase : {0u, 1u, 8703u, 12374u}) raw_crossing_test(phase);
        for (unsigned phase : {0u, 8703u}) adapter_crossing_test(phase);
        overrun_test();
        std::cout << "native video: " << checked_cycles << " exact HDMI cycles, "
                  << checked_crossings << " exact asynchronous tokens and "
                  << checked_adapter_pixels << " settled adapter pixels passed\n";
    } catch (const std::exception &error) {
        std::cerr << "native video: FAIL: " << error.what() << '\n';
        return 1;
    }
    return 0;
}
