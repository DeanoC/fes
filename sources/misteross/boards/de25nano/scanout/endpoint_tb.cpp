// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_scanout.h"
#include "Vfes_de25_scanout___024root.h"
#include "generated/fes_de25_scanout.h"
#include "verilated.h"

#include <array>
#include <algorithm>
#include <cstdint>
#include <deque>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

static void require(bool condition, const std::string& message) {
    if (!condition) throw std::runtime_error(message);
}
static void equal(std::uint64_t actual, std::uint64_t expected, const std::string& message) {
    if (actual == expected) return;
    std::ostringstream detail;
    detail << message << ": got 0x" << std::hex << actual << ", expected 0x" << expected;
    throw std::runtime_error(detail.str());
}
struct Descriptor {
    std::uint64_t base = 0, generation = 0xfedcba9876543210ull;
    std::uint32_t span = FES_DE25_SCANOUT_ALLOCATION_BYTES, sequence = 1, slot = 1;
};
struct Snapshot {
    std::array<std::uint32_t, 13> words{};
    std::uint32_t status() const { return words[0]; }
    std::uint64_t generation() const { return std::uint64_t(words[2]) << 32 | words[1]; }
    std::uint32_t accepted() const { return words[3]; }
    std::uint32_t displayed() const { return words[4]; }
    std::uint32_t current() const { return words[5]; }
    std::uint32_t slots() const { return words[6]; }
    std::uint32_t errors() const { return words[7]; }
    std::uint32_t outstanding() const { return words[9]; }
};
struct Burst { std::uint32_t address, beats; };

struct FirstFault {
    std::array<std::uint32_t, 3> words{};
    bool valid() const { return words[0] & FES_DE25_SCANOUT_FIRST_FAULT_VALID_MASK; }
    unsigned h() const { return (words[0] >> FES_DE25_SCANOUT_FIRST_FAULT_HORIZONTAL_SHIFT) & FES_DE25_SCANOUT_FIRST_FAULT_HORIZONTAL_MASK; }
    unsigned v() const { return (words[0] >> FES_DE25_SCANOUT_FIRST_FAULT_VERTICAL_SHIFT) & FES_DE25_SCANOUT_FIRST_FAULT_VERTICAL_MASK; }
    unsigned state() const { return (words[0] >> FES_DE25_SCANOUT_FIRST_FAULT_VIDEO_STATE_SHIFT) & FES_DE25_SCANOUT_FIRST_FAULT_VIDEO_STATE_MASK; }
    unsigned fifo() const { return words[1] & FES_DE25_SCANOUT_FIRST_FAULT_FIFO_COUNT_MASK; }
    unsigned outstanding() const { return (words[1] >> FES_DE25_SCANOUT_FIRST_FAULT_OUTSTANDING_SHIFT) & FES_DE25_SCANOUT_FIRST_FAULT_OUTSTANDING_MASK; }
    unsigned response_gap() const { return words[2] & FES_DE25_SCANOUT_FIRST_FAULT_RESPONSE_GAP_MASK; }
    unsigned command_age() const { return (words[2] >> FES_DE25_SCANOUT_FIRST_FAULT_HELD_COMMAND_AGE_SHIFT) & FES_DE25_SCANOUT_FIRST_FAULT_HELD_COMMAND_AGE_MASK; }
};

// The response pattern distinguishes every pixel lane and both slots. BGRX's
// low24 bits pass unchanged to the endpoint's RGB output.
static std::uint32_t pixel(unsigned slot, std::uint32_t index) {
    return 0xff000000u | (slot ? 0x00600000u : 0x00300000u) | index;
}
class Endpoint {
public:
    Vfes_de25_scanout dut;
    bool stall = false, responses = true, zero_latency = false, unsolicited = false;
    unsigned response_code = 0;
    bool check_pixels = false;
    unsigned visible_slot = 1;
    std::uint64_t active_pixels = 0, returned = 0;
    std::vector<Burst> bursts;
    std::deque<std::uint32_t> pending;
    std::deque<std::uint64_t> response_ready;
    std::deque<unsigned> pending_commands;
    unsigned max_burst = FES_DE25_SCANOUT_MAX_BURST_BEATS, response_limit = 511;
    unsigned response_latency = 0, max_response_credit = 0, max_commands = 0;
    std::uint64_t cycles = 0;
    // Observe the real reader/video immediately before the edge that faults,
    // independently of the endpoint's later cancellation and CSR reads.
    bool observe_first_fault = false, hold_blank_for_age_test = false;
    FirstFault expected_fault;

    Endpoint() {
        dut.clk = 0; dut.reset_n = 0; dut.clock_fault = 0; dut.stream_ready = 0; dut.display_ready = 0;
        dut.address = 0; dut.read = 0; dut.write = 0; dut.byteenable = 0; dut.writedata = 0;
        dut.avm_waitrequest = 0; dut.avm_readdatavalid = 0; dut.avm_response = 0;
        for (unsigned lane = 0; lane < 4; ++lane) dut.avm_readdata[lane] = 0;
        dut.eval(); tick(); tick(); dut.reset_n = 1; idle(4);
        equal(read(FES_DE25_SCANOUT_ID_OFFSET), FES_DE25_SCANOUT_IDENTITY, "identity");
        equal(read(FES_DE25_SCANOUT_VERSION_OFFSET), FES_DE25_SCANOUT_VERSION, "version");
        equal(read(FES_DE25_SCANOUT_BUILD_ID0_OFFSET), 0x76543210u, "build ID word0");
        equal(read(FES_DE25_SCANOUT_BUILD_ID1_OFFSET), 0xfedcba98u, "build ID word1");
        equal(read(FES_DE25_SCANOUT_BUILD_ID2_OFFSET), 0x89abcdefu, "build ID word2");
        equal(read(FES_DE25_SCANOUT_BUILD_ID3_OFFSET), 0x01234567u, "build ID word3");
        write(FES_DE25_SCANOUT_BUILD_ID0_OFFSET, 0xffffffffu);
        equal(read(FES_DE25_SCANOUT_BUILD_ID0_OFFSET), 0x76543210u, "build identity is read-only");
        equal(read(FES_DE25_SCANOUT_STATUS_OFFSET), FES_DE25_SCANOUT_STATUS_DRAINED, "cold state");
        dut.stream_ready = 1;
        dut.display_ready = 1; idle(2);
        equal(read(FES_DE25_SCANOUT_STATUS_OFFSET), FES_DE25_SCANOUT_STATUS_DRAINED | FES_DE25_SCANOUT_STATUS_DISPLAY_READY,
              "readiness is observed without enabling DMA");
        dut.stream_ready = 0;
        dut.display_ready = 0; idle(2);
        equal(read(FES_DE25_SCANOUT_STATUS_OFFSET), FES_DE25_SCANOUT_STATUS_DRAINED, "readiness loss is observed");
        dut.stream_ready = 1;
        dut.display_ready = 1; idle(2);
        require(bursts.empty() && !dut.avm_read, "cold endpoint fetched memory");
    }
    ~Endpoint() { dut.final(); }

    void tick() {
        if (hold_blank_for_age_test) {
            // Saturation alone needs a longer blank than the normal raster.
            // This test-only stimulus never changes memory/credit accounting.
            dut.rootp->fes_de25_scanout__DOT__video__DOT__internal_h = 0;
            dut.rootp->fes_de25_scanout__DOT__video__DOT__internal_v = 720;
        }
        dut.clk = 0; dut.avm_waitrequest = stall;
        dut.avm_readdatavalid = 0; dut.avm_response = 0; dut.eval();
        const bool accepts = dut.reset_n && dut.avm_read && !stall;
        const bool old_response = !pending.empty();
        if (dut.reset_n) {
            const unsigned held = dut.avm_read ? dut.avm_burstcount : 0;
            const auto* reader = dut.rootp;
            equal(reader->fes_de25_scanout__DOT__video__DOT__reader__DOT__outstanding,
                  pending.size(), "accepted credit differs from independent response queue");
            require(pending.size() + held <= response_limit, "accepted plus held response credit exceeded limit");
            require(reader->fes_de25_scanout__DOT__video__DOT__reader__DOT__count + pending.size() + held <= 2048,
                    "FIFO plus accepted/held response reservations exceed local capacity");
            require(pending_commands.size() + (dut.avm_read ? 1u : 0u) <= 16,
                    "accepted plus held commands exceed HPS transaction capacity");
            max_response_credit = std::max(max_response_credit, unsigned(pending.size() + held));
            max_commands = std::max(max_commands, unsigned(pending_commands.size() + (dut.avm_read ? 1u : 0u)));
            if (held_) {
                require(dut.avm_read, "stalled command withdrawn");
                equal(dut.avm_address, held_address_, "stalled address changed");
                equal(dut.avm_burstcount, held_beats_, "stalled burst changed");
                equal(dut.avm_byteenable, 0xffff, "stalled byte enable changed");
            }
            if (dut.avm_read) {
                require(bounded_, "memory access without an accepted prospective grant");
                require(dut.avm_burstcount && dut.avm_burstcount <= max_burst,
                        "invalid memory burst length");
                equal(dut.avm_address & 15u, 0, "memory alignment");
                equal(dut.avm_byteenable, 0xffff, "memory byte enables");
                require((dut.avm_address & 4095u) + 16u * dut.avm_burstcount <= 4096,
                        "memory burst crosses4KiB");
                const std::uint64_t address = dut.avm_address;
                require(address >= base_ && address + 16u * dut.avm_burstcount <= base_ + FES_DE25_SCANOUT_ALLOCATION_BYTES,
                        "memory burst exceeds allocation");
                const auto offset = address - base_;
                const unsigned slot = offset / FES_DE25_SCANOUT_SLOT_BYTES;
                require(slot < 2 && offset % FES_DE25_SCANOUT_SLOT_BYTES + 16u * dut.avm_burstcount <= FES_DE25_SCANOUT_FRAME_BYTES,
                        "memory burst reaches slot padding");
                require(allowed_slots_ & (1u << slot), "fetch of a slot before submission");
            }
            if (accepts) {
                require(pending.size() + dut.avm_burstcount <= response_limit,
                        "accepted responses exceed the crossing response-credit limit");
                const auto address = std::uint64_t(dut.avm_address);
                const auto slot = unsigned((address - base_) / FES_DE25_SCANOUT_SLOT_BYTES);
                const auto start = base_ + std::uint64_t(slot) * FES_DE25_SCANOUT_SLOT_BYTES;
                if (!have_frame_ || address == start) {
                    if (have_frame_)
                        equal(next_address_, frame_start_ + FES_DE25_SCANOUT_FRAME_BYTES, "new frame before prior complete fetch");
                    frame_start_ = start; next_address_ = start; have_frame_ = true;
                }
                equal(address, next_address_, "frame fetch skips or repeats bytes");
                bursts.push_back({dut.avm_address, dut.avm_burstcount});
                pending_commands.push_back(dut.avm_burstcount);
                for (unsigned beat = 0; beat < dut.avm_burstcount; ++beat) {
                    pending.push_back(dut.avm_address + beat * 16u);
                    response_ready.push_back(cycles + response_latency);
                }
                next_address_ += 16u * dut.avm_burstcount;
            }
            if (responses && (old_response || (zero_latency && accepts)) && response_ready.front() <= cycles) {
                const auto address = pending.front(); pending.pop_front();
                response_ready.pop_front();
                if (!--pending_commands.front()) pending_commands.pop_front();
                const auto offset = std::uint64_t(address) - base_;
                const unsigned slot = offset / FES_DE25_SCANOUT_SLOT_BYTES;
                const auto index = std::uint32_t(offset % FES_DE25_SCANOUT_SLOT_BYTES / 4u);
                for (unsigned lane = 0; lane < 4; ++lane) dut.avm_readdata[lane] = pixel(slot, index + lane);
                dut.avm_readdatavalid = 1; dut.avm_response = response_code; ++returned;
            } else if (unsolicited) {
                require(pending.empty() && !accepts, "unexpected-response injection was solicited");
                dut.avm_readdatavalid = 1;
            }
            dut.eval();
            if (check_pixels && dut.video_de) {
                const auto index = std::uint32_t(active_pixels % (FES_DE25_SCANOUT_FRAME_BYTES / 4u));
                equal(dut.video_rgb, pixel(visible_slot, index) & 0xffffffu, "display pixel/lane/order");
                ++active_pixels;
            }
            held_ = dut.avm_read && stall; held_address_ = dut.avm_address; held_beats_ = dut.avm_burstcount;
        }
        if (observe_first_fault) sample_first_fault();
        dut.clk = 1; dut.eval(); dut.clk = 0; dut.eval();
        ++cycles;
        if (observe_first_fault) {
            equal(dut.rootp->fes_de25_scanout__DOT__video__DOT__first_fault_state,
                  expected_fault.words[0], "first-fault state on capture/reset/hold edge");
            equal(dut.rootp->fes_de25_scanout__DOT__video__DOT__first_fault_occupancy,
                  expected_fault.words[1], "first-fault occupancy on capture/reset/hold edge");
            equal(dut.rootp->fes_de25_scanout__DOT__video__DOT__first_fault_gaps,
                  expected_fault.words[2], "first-fault gaps on capture/reset/hold edge");
        }
    }
    void idle(unsigned clocks = 1) {
        dut.read = 0; dut.write = 0;
        while (clocks--) tick();
    }
    void write(std::uint32_t address, std::uint32_t value, unsigned enables = 15) {
        dut.address = address; dut.write = 1; dut.read = 0; dut.byteenable = enables; dut.writedata = value;
        tick(); require(dut.writeresponsevalid && !dut.waitrequest && !dut.response, "write acknowledgement"); idle();
    }
    std::uint32_t read(std::uint32_t address) {
        dut.address = address; dut.read = 1; dut.write = 0; tick();
        require(dut.readdatavalid && !dut.waitrequest && !dut.response, "registered read acknowledgement");
        const auto result = dut.readdata; idle(); return result;
    }
    void command(std::uint32_t value) { write(FES_DE25_SCANOUT_COMMAND_OFFSET, value); }
    void stage(const Descriptor& d) {
        write(FES_DE25_SCANOUT_GRANT_LO_OFFSET, std::uint32_t(d.base));
        write(FES_DE25_SCANOUT_GRANT_HI_OFFSET, std::uint32_t(d.base >> 32));
        write(FES_DE25_SCANOUT_GRANT_SPAN_OFFSET, d.span);
        write(FES_DE25_SCANOUT_GENERATION_LO_OFFSET, std::uint32_t(d.generation));
        write(FES_DE25_SCANOUT_GENERATION_HI_OFFSET, std::uint32_t(d.generation >> 32));
        write(FES_DE25_SCANOUT_SEQUENCE_OFFSET, d.sequence);
        write(FES_DE25_SCANOUT_SLOT_OFFSET, d.slot);
    }
    void submit(const Descriptor& d) {
        stage(d); base_ = d.base; bounded_ = true; allowed_slots_ |= 1u << d.slot;
        command(FES_DE25_SCANOUT_COMMAND_SUBMIT);
    }
    Snapshot snapshot() {
        const auto serial = read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET);
        command(FES_DE25_SCANOUT_COMMAND_SNAPSHOT);
        equal(read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET), std::uint32_t(serial + 1u), "snapshot serial");
        Snapshot result;
        for (unsigned i = 0; i < result.words.size(); ++i)
            result.words[i] = read(FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET + 4u * i);
        equal(read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET), std::uint32_t(serial + 1u), "snapshot changed while read");
        return result;
    }
    FirstFault first_fault() {
        return {{read(FES_DE25_SCANOUT_FIRST_FAULT_STATE_OFFSET),
                 read(FES_DE25_SCANOUT_FIRST_FAULT_OCCUPANCY_OFFSET),
                 read(FES_DE25_SCANOUT_FIRST_FAULT_GAPS_OFFSET)}};
    }
    Snapshot wait_status(std::uint32_t bits, unsigned limit = 10000) {
        for (unsigned i = 0; i < limit; i += 64) {
            auto state = snapshot(); if ((state.status() & bits) == bits) return state; idle(64);
        }
        throw std::runtime_error("status wait exceeded bounded test budget");
    }
    Snapshot wait_displayed(std::uint32_t sequence) {
        for (unsigned i = 0; i < 3000000; i += 256) {
            auto state = snapshot(); require(!state.errors(), "frame fault before display completion");
            if (state.displayed() == sequence) return state; idle(256);
        }
        throw std::runtime_error("full-frame completion absent");
    }
    void wait_request(unsigned limit = 100000) {
        for (unsigned i = 0; i < limit && !dut.avm_read; ++i) idle();
        require(dut.avm_read, "no memory command");
    }
    void stop_and_drain() {
        command(FES_DE25_SCANOUT_COMMAND_STOP); stall = false; responses = true;
        const auto state = wait_status(FES_DE25_SCANOUT_STATUS_DRAINED);
        equal(state.outstanding(), 0, "drained outstanding"); require(pending.empty() && !dut.avm_read, "drain hid traffic");
    }
    void reset_epoch() {
        dut.reset_n = 0; dut.eval();
        // The scanout reset also resets both mm_ccb sides, dropping this
        // accepted burst and its unread responses.
        pending.clear(); response_ready.clear(); pending_commands.clear();
        held_ = false; have_frame_ = false; bounded_ = false;
        allowed_slots_ = 0; frame_start_ = 0; next_address_ = 0;
        idle(2);
        dut.reset_n = 1; dut.eval(); idle(4);
    }
    void new_grant_epoch() {
        // Reset only test memory-order bookkeeping after legal local CLEAR.
        // The DUT must rearm without asserting its configuration reset.
        require(pending.empty() && !dut.avm_read, "new test epoch before traffic drained");
        held_ = false; have_frame_ = false; bounded_ = false; allowed_slots_ = 0;
        frame_start_ = 0; next_address_ = 0;
    }
private:
    unsigned response_gap_ = 0, command_age_ = 0, response_max_ = 0, command_max_ = 0;
    void sample_first_fault() {
        const auto* r = dut.rootp;
        const bool reset = !dut.reset_n || r->fes_de25_scanout__DOT__local_clear;
        const bool fetch_start = r->fes_de25_scanout__DOT__video__DOT__state == 1 &&
            r->fes_de25_scanout__DOT__video__DOT__running && r->fes_de25_scanout__DOT__video__DOT__reader_idle;
        response_gap_ = r->fes_de25_scanout__DOT__video__DOT__reader__DOT__outstanding && !dut.avm_readdatavalid ?
            std::min(65535u, response_gap_ + 1) : 0;
        command_age_ = dut.avm_read && dut.avm_waitrequest ? std::min(65535u, command_age_ + 1) : 0;
        response_max_ = std::max(response_max_, response_gap_);
        command_max_ = std::max(command_max_, command_age_);
        if (reset) expected_fault = {};
        else if (!expected_fault.valid() && r->fes_de25_scanout__DOT__video__DOT__armed &&
                 r->fes_de25_scanout__DOT__video__DOT__running && r->fes_de25_scanout__DOT__video__DOT__de &&
                 !r->fes_de25_scanout__DOT__video__DOT__pixel_matches) {
            const auto index = r->fes_de25_scanout__DOT__video__DOT__pixel_index;
            const auto raster = r->fes_de25_scanout__DOT__video__DOT__raster_index;
            const unsigned relation = index < raster ? 0 : (index == raster ? 1 : 2);
            expected_fault.words[0] = FES_DE25_SCANOUT_FIRST_FAULT_VALID_MASK |
                (r->fes_de25_scanout__DOT__video__DOT__internal_h << FES_DE25_SCANOUT_FIRST_FAULT_HORIZONTAL_SHIFT) |
                (r->fes_de25_scanout__DOT__video__DOT__internal_v << FES_DE25_SCANOUT_FIRST_FAULT_VERTICAL_SHIFT) |
                (r->fes_de25_scanout__DOT__video__DOT__state << FES_DE25_SCANOUT_FIRST_FAULT_VIDEO_STATE_SHIFT) |
                (r->fes_de25_scanout__DOT__video__DOT__pixel_valid ? FES_DE25_SCANOUT_FIRST_FAULT_PIXEL_VALID_MASK : 0) |
                (relation << FES_DE25_SCANOUT_FIRST_FAULT_INDEX_RELATION_SHIFT) |
                (dut.avm_read ? FES_DE25_SCANOUT_FIRST_FAULT_READ_MASK : 0) |
                (dut.avm_waitrequest ? FES_DE25_SCANOUT_FIRST_FAULT_WAITREQUEST_MASK : 0) |
                (dut.avm_readdatavalid ? FES_DE25_SCANOUT_FIRST_FAULT_READDATAVALID_MASK : 0) | dut.avm_response;
            expected_fault.words[1] = r->fes_de25_scanout__DOT__video__DOT__reader__DOT__count |
                (r->fes_de25_scanout__DOT__video__DOT__reader__DOT__outstanding << FES_DE25_SCANOUT_FIRST_FAULT_OUTSTANDING_SHIFT) |
                (r->fes_de25_scanout__DOT__video__DOT__reader__DOT__lane << FES_DE25_SCANOUT_FIRST_FAULT_LANE_SHIFT);
            expected_fault.words[2] = response_max_ | (command_max_ << FES_DE25_SCANOUT_FIRST_FAULT_HELD_COMMAND_AGE_SHIFT);
        }
        if (reset || fetch_start) response_gap_ = command_age_ = response_max_ = command_max_ = 0;
    }
    bool held_ = false, bounded_ = false, have_frame_ = false;
    std::uint32_t held_address_ = 0, held_beats_ = 0;
    std::uint64_t base_ = 0, frame_start_ = 0, next_address_ = 0;
    unsigned allowed_slots_ = 0;
};

static void disabled_and_snapshot() {
    Endpoint e; e.idle(500);
    require(e.bursts.empty() && !e.dut.video_de, "disabled endpoint active");
    e.write(FES_DE25_SCANOUT_GRANT_LO_OFFSET, 0xaabbccddu);
    e.write(FES_DE25_SCANOUT_GRANT_LO_OFFSET, 0x11223344u, 5);
    equal(e.read(FES_DE25_SCANOUT_GRANT_LO_OFFSET), 0xaa22cc44u, "staging byte enables");
    e.write(FES_DE25_SCANOUT_ID_OFFSET, 0);
    equal(e.read(FES_DE25_SCANOUT_ID_OFFSET), FES_DE25_SCANOUT_IDENTITY, "readonly identity");
    Descriptor d; e.stall = true; e.responses = false; e.submit(d);
    const auto frozen = e.snapshot(); equal(frozen.generation(), d.generation, "full64 generation");
    equal(frozen.accepted(), 1, "accepted sequence"); equal(frozen.displayed(), 0, "acceptance falsely displayed");
    const auto serial = e.read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET);
    e.command(FES_DE25_SCANOUT_COMMAND_STOP);
    equal(e.read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET), serial, "STOP changed frozen serial");
    for (unsigned i = 0; i < frozen.words.size(); ++i)
        equal(e.read(FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET + 4u * i), frozen.words[i], "live event changed frozen fields");
    e.stop_and_drain(); e.command(FES_DE25_SCANOUT_COMMAND_CLEAR);
    auto empty = e.snapshot(); equal(empty.generation(), 0, "CLEAR generation");
    equal(empty.status(), FES_DE25_SCANOUT_STATUS_DRAINED | FES_DE25_SCANOUT_STATUS_DISPLAY_READY, "CLEAR empty state");
}
static void raster_sync_runs_while_disabled_and_survives_reset() {
    Endpoint e;
    bool previous_hs = false, reset_during_hs = false;
    unsigned hsync_pixels = 0;
    for (unsigned cycle = 0; cycle < 2000; ++cycle) {
        e.idle();
        const bool hs = e.dut.video_hs;
        if (hs) {
            if (!previous_hs) hsync_pixels = 0;
            ++hsync_pixels;
            if (hsync_pixels == 5) {
                e.dut.reset_n = 0; e.dut.eval();
                require(e.dut.video_hs, "endpoint reset clipped an active HS pulse");
                reset_during_hs = true;
            }
        } else if (previous_hs) {
            break;
        }
        previous_hs = hs;
    }
    require(reset_during_hs, "test did not assert reset inside an HS pulse");
    equal(hsync_pixels, 40, "continuous disabled/reset HS width");
    require(!e.dut.video_de && !e.dut.video_rgb, "disabled/reset endpoint exposed pixel data");

    e.dut.reset_n = 1; e.dut.eval();
    bool previous_vs = false, reset_during_vs = false;
    unsigned vsync_pixels = 0;
    for (unsigned cycle = 0; cycle < 20000; ++cycle) {
        e.idle();
        const bool vs = e.dut.video_vs;
        if (vs) {
            if (!previous_vs) vsync_pixels = 0;
            ++vsync_pixels;
            if (vsync_pixels == 100) {
                e.dut.reset_n = 0; e.dut.eval();
                require(e.dut.video_vs, "endpoint reset clipped an active VS pulse");
                reset_during_vs = true;
            }
        } else if (previous_vs) {
            break;
        }
        previous_vs = vs;
    }
    require(reset_during_vs, "test did not assert reset inside a VS pulse");
    equal(vsync_pixels, 5u * 1650u, "continuous VS width across endpoint reset");
}
static void clock_fault_does_not_shorten_active_hsync() {
    Endpoint e; Descriptor d; e.submit(d);
    while (e.dut.video_hs) e.idle();
    for (unsigned cycle = 0; cycle < 2000 && !e.dut.video_hs; ++cycle) e.idle();
    require(e.dut.video_hs, "enabled endpoint never reached horizontal sync");

    unsigned hsync_pixels = 0;
    while (e.dut.video_hs) {
        ++hsync_pixels;
        if (hsync_pixels == 5) e.dut.clock_fault = 1;
        e.idle();
    }
    equal(hsync_pixels, 40, "clock fault shortened active HS pulse");
    require(e.dut.video_de == 0 && e.dut.video_rgb == 0,
            "clock fault left endpoint pixel data visible");
}
static void two_complete_frames(bool zero_latency) {
    Endpoint e; Descriptor d; e.zero_latency = zero_latency; e.check_pixels = true; e.submit(d);
    auto accepted = e.snapshot(); equal(accepted.accepted(), 1, "first acceptance"); equal(accepted.displayed(), 0, "early completion");
    bool saw_current_before_ack = false;
    for (unsigned i = 0; i < 200000; i += 128) {
        auto state = e.snapshot(); require(!state.errors(), "initial prefetch fault");
        if (state.current() == 1) {
            equal(state.displayed(), 0, "current/fetch identity falsely completed a frame");
            saw_current_before_ack = true; break;
        }
        e.idle(128);
    }
    require(saw_current_before_ack, "current identity never advanced");
    auto complete = e.wait_displayed(1);
    equal(complete.status() & FES_DE25_SCANOUT_STATUS_PENDING, 0, "pending after full frame");
    equal(e.active_pixels, FES_DE25_SCANOUT_FRAME_BYTES / 4u, "first full visible frame pixels");
    equal((complete.slots() >> FES_DE25_SCANOUT_DISPLAYED_SLOT_SHIFT) & 1u, 1, "first displayed slot");
    e.visible_slot = 0; d.slot = 0; d.sequence = 2; e.submit(d);
    auto pending = e.snapshot(); equal(pending.displayed(), 1, "second acceptance released old front");
    auto second = e.wait_displayed(2);
    equal(second.current(), 2, "second current"); equal((second.slots() >> 8) & 1u, 0, "second displayed slot");
    equal(e.active_pixels, 2u * (FES_DE25_SCANOUT_FRAME_BYTES / 4u), "two full visible frames");
    require(e.returned >= 2u * FES_DE25_SCANOUT_FRAME_BYTES / 16u, "displayed bytes not all fetched");
    e.check_pixels = false; e.stop_and_drain();
}
static void transient_ddr_contention_preserves_complete_frame() {
    Endpoint e; Descriptor d; e.check_pixels = true; e.submit(d);
    for (unsigned clocks = 0; e.active_pixels < 12000 && clocks < 200000; ++clocks) e.idle();
    require(e.active_pixels >= 12000, "contention test never reached active scanout");
    // Host writes and page-table maintenance can delay the shared DDR path.
    // Exercise a finite service gap; this is a modeled elasticity requirement,
    // not a claim that physical memory latency has been bounded.
    e.stall = true; e.responses = false; e.idle(6000);
    e.stall = false; e.responses = true;
    const auto complete = e.wait_displayed(d.sequence);
    equal(complete.errors(), 0, "finite DDR gap faulted scanout");
    equal(complete.words[8], 0, "finite DDR gap lost a pixel");
    equal(e.active_pixels, FES_DE25_SCANOUT_FRAME_BYTES / 4u, "contention frame was incomplete");
    e.stop_and_drain();
}
static void stop_keeps_stalled_command_and_accepted_responses() {
    Endpoint e; Descriptor d; e.stall = true; e.responses = false; e.submit(d); e.wait_request();
    const auto address = e.dut.avm_address; const auto beats = e.dut.avm_burstcount;
    auto held = e.snapshot(); require(held.outstanding() >= beats && !(held.status() & FES_DE25_SCANOUT_STATUS_DRAINED), "held request absent from drain accounting");
    e.command(FES_DE25_SCANOUT_COMMAND_STOP); e.idle(100);
    equal(e.dut.avm_address, address, "STOP stalled address"); equal(e.dut.avm_burstcount, beats, "STOP stalled length");
    e.stall = false; e.idle(); e.idle(100);
    auto pending = e.snapshot(); require(!(pending.status() & FES_DE25_SCANOUT_STATUS_DRAINED), "missing responses became drained");
    equal(pending.generation(), d.generation, "STOP lost ownership generation"); equal(pending.outstanding(), beats, "accepted response budget");
    equal(pending.displayed(), 0, "STOP acknowledged incomplete frame");
    e.stop_and_drain(); equal(e.returned, beats, "not all accepted responses retired");
}
static void read_ahead_reserves_capacity_and_stop_drains_every_accepted_beat() {
    Endpoint e; Descriptor d; e.responses = false; e.submit(d); e.wait_request();
    e.idle(1000);
    constexpr unsigned whole_bursts = 7u * 64u;
    equal(e.pending.size(), whole_bursts, "fully unreturned bursts must stop at448, not wrap512");
    require(!e.dut.avm_read, "reader overcommitted a command after reserving the full FIFO");
    auto full = e.snapshot(); equal(full.outstanding(), e.pending.size(), "overlapped response budget");
    e.command(FES_DE25_SCANOUT_COMMAND_STOP);
    auto stopping = e.snapshot();
    require(!(stopping.status() & FES_DE25_SCANOUT_STATUS_DRAINED), "STOP released unreturned responses");
    equal(stopping.outstanding(), whole_bursts, "STOP lost an overlapped response reservation");
    e.stop_and_drain(); equal(e.returned, whole_bursts, "STOP did not retire all overlapped responses");
}
static void partial_response_reserves_exact_511_through_held_stop() {
    Endpoint e; Descriptor d; e.responses = false; e.submit(d); e.idle(1000);
    equal(e.pending.size(), 448, "partial boundary needs seven unreturned64-beat bursts");
    e.responses = true; e.idle(); e.responses = false; e.idle();
    require(e.dut.avm_read, "one returned beat did not permit the next64-beat reservation");
    e.stall = true; e.idle(4);
    equal(e.pending.size(), 447, "partial boundary returned more than one beat");
    const auto held = e.snapshot();
    equal(held.outstanding(), 511, "accepted plus held must represent511 without wrapping");
    equal(e.max_response_credit, 511, "511 boundary was never reached");
    e.command(FES_DE25_SCANOUT_COMMAND_STOP); e.idle(32);
    equal(e.snapshot().outstanding(), 511, "STOP lost511 accepted/held reservations");
    e.stall = false; e.idle();
    equal(e.pending.size(), 511, "held command did not become exactly511 accepted beats");
    e.stop_and_drain(); equal(e.returned, 512, "partial plus511 accepted beats did not all retire");
}
static void reset_midburst_restores_idle_submit_and_clear() {
    Endpoint e; Descriptor d; e.responses = false; e.submit(d); e.wait_request();
    e.idle();
    auto active = e.snapshot();
    require(active.outstanding() != 0, "test did not leave an accepted burst in flight");
    require(!e.pending.empty(), "test did not retain the burst's unread responses");

    e.reset_epoch();
    auto reset = e.snapshot();
    require(reset.status() & FES_DE25_SCANOUT_STATUS_DRAINED, "reset did not return to drained idle");
    equal(reset.outstanding(), 0, "reset retained an old response budget");
    require(!e.dut.avm_read, "reset left an old memory command asserted");
    equal(e.dut.avm_address, 0, "reset retained an old memory address");
    equal(e.dut.avm_burstcount, 0, "reset retained an old burst length");

    d.generation += 1; d.sequence = 1;
    e.stall = true; e.responses = false; e.submit(d); e.wait_request(2000000);
    auto accepted = e.snapshot();
    equal(accepted.accepted(), d.sequence, "SUBMIT after reset was not accepted");
    require(!accepted.errors(), "SUBMIT after reset reported a fault");
    e.command(FES_DE25_SCANOUT_COMMAND_STOP);
    e.stall = false; e.responses = true; e.idle(64);
    e.stop_and_drain();
    e.command(FES_DE25_SCANOUT_COMMAND_CLEAR);
    auto cleared = e.snapshot();
    require(cleared.status() & FES_DE25_SCANOUT_STATUS_DRAINED, "CLEAR after reset was not drained");
    equal(cleared.generation(), 0, "CLEAR after reset retained the grant");
    equal(cleared.errors(), 0, "CLEAR after reset retained errors");
}
static void reset_cancels_a_stalled_command() {
    Endpoint e; Descriptor d; e.stall = true; e.responses = false; e.submit(d); e.wait_request();
    require(e.dut.avm_read, "test did not hold an unaccepted command");
    e.reset_epoch();
    const auto reset = e.snapshot();
    require(reset.status() & FES_DE25_SCANOUT_STATUS_DRAINED, "reset did not retire a stalled command");
    require(!e.dut.avm_read, "reset reissued a previously stalled command");
    equal(e.dut.avm_address, 0, "stalled reset retained address");
    equal(e.dut.avm_burstcount, 0, "stalled reset retained burst length");
}
static void boundaries_and_low_iova() {
    for (const std::uint64_t base : {0ull, 0x10000020ull, 0xff800000ull}) {
        Endpoint e; Descriptor d; d.base = base; e.submit(d);
        for (unsigned i = 0; i < 150000 && e.bursts.size() < 35; ++i) e.idle();
        require(e.bursts.size() >= 35, "insufficient burst/page coverage");
        bool page_end = false;
        for (const auto& burst : e.bursts)
            if ((burst.address & 4095u) + burst.beats * 16u == 4096u) page_end = true;
        require(page_end, "test did not exercise exact4KiB split"); e.stop_and_drain();
    }
}
static void invalid_descriptors_never_fetch() {
    for (unsigned variant = 0; variant < 9; ++variant) {
        Endpoint e; Descriptor d;
        switch (variant) {
        case 0: d.base = 1ull << 32; break;
        case 1: d.base = 16; break;
        case 2: d.base = 0xff800020ull; break;
        case 3: d.span -= 16; break;
        case 4: d.generation = 0; break;
        case 5: d.sequence = 0; break;
        case 6: d.slot = 2; break;
        case 7: d.slot = 0; break;
        case 8: d.span = 0; break;
        }
        e.stage(d); e.command(FES_DE25_SCANOUT_COMMAND_SUBMIT); auto state = e.snapshot(); e.idle(100);
        require(state.errors() && (state.status() & FES_DE25_SCANOUT_STATUS_FAULT), "invalid descriptor accepted");
        equal(state.accepted(), 0, "invalid acceptance"); equal(state.displayed(), 0, "invalid display acknowledgement");
        require(e.bursts.empty(), "invalid descriptor fetched memory");
    }
}
static void pending_and_sequence_errors() {
    Endpoint e; Descriptor d; e.stall = true; e.responses = false; e.submit(d); e.wait_request();
    // Staging writes alone never alter the immutable active grant.
    d.generation ^= 1ull << 32; d.sequence = 2; d.slot = 0; e.stage(d);
    auto unchanged = e.snapshot(); equal(unchanged.generation(), 0xfedcba9876543210ull, "stage changed active generation");
    e.command(FES_DE25_SCANOUT_COMMAND_SUBMIT); auto error = e.snapshot();
    require(error.errors() & FES_DE25_SCANOUT_ERROR_COMMAND, "pending resubmit lacked command fault");
    require(error.errors() & FES_DE25_SCANOUT_ERROR_DESCRIPTOR, "changed immutable grant lacked descriptor fault");
    equal(error.accepted(), 1, "rejected resubmit changed acceptance"); equal(error.displayed(), 0, "rejected resubmit completed frame");
    e.stop_and_drain(); e.command(FES_DE25_SCANOUT_COMMAND_CLEAR);
    Descriptor maximum; maximum.sequence = 0xffffffffu; e.submit(maximum); e.stop_and_drain();
    maximum.sequence = 1; maximum.slot = 1; e.stage(maximum); e.command(FES_DE25_SCANOUT_COMMAND_SUBMIT);
    auto exhausted = e.snapshot(); require(exhausted.errors() & FES_DE25_SCANOUT_ERROR_SEQUENCE, "sequence wrapped within retained generation");
    equal(exhausted.accepted(), 0xffffffffu, "sequence error replaced accepted identity");
}
static void response_error(unsigned code) {
    Endpoint e; Descriptor d; e.response_code = code; e.submit(d);
    auto state = e.wait_status(FES_DE25_SCANOUT_STATUS_FAULT);
    require(state.errors() & FES_DE25_SCANOUT_ERROR_RESPONSE, "memory response code lost");
    equal(state.displayed(), 0, "response error acknowledged frame");
    e.stop_and_drain(); e.command(FES_DE25_SCANOUT_COMMAND_CLEAR);
    auto cleared = e.snapshot(); equal(cleared.errors(), 0, "drained response error could not clear");
}
static void unexpected_response_is_terminal() {
    Endpoint e; Descriptor d; e.stall = true; e.responses = false; e.submit(d); e.wait_request();
    e.unsolicited = true; e.idle(); e.unsolicited = false;
    auto state = e.snapshot(); require(state.errors() & FES_DE25_SCANOUT_ERROR_UNEXPECTED_RESPONSE, "unexpected response ignored");
    e.command(FES_DE25_SCANOUT_COMMAND_STOP); e.stall = false; e.responses = true; e.idle(100);
    require(e.pending.empty(), "test did not retire actual accepted traffic");
    e.command(FES_DE25_SCANOUT_COMMAND_CLEAR); auto terminal = e.snapshot();
    require(!(terminal.status() & FES_DE25_SCANOUT_STATUS_DRAINED), "unexpected response manufactured drain proof");
    require(terminal.errors() & FES_DE25_SCANOUT_ERROR_UNEXPECTED_RESPONSE, "CLEAR removed terminal accounting fault");
    equal(terminal.displayed(), 0, "unexpected response acknowledged frame");
}
static void underflow_never_completes_frame() {
    Endpoint e; Descriptor d; e.stall = true; e.responses = false; e.submit(d);
    auto state = e.wait_status(FES_DE25_SCANOUT_STATUS_FAULT, 120000);
    require(state.errors() & FES_DE25_SCANOUT_ERROR_UNDERFLOW, "unavailable pixels not reported");
    equal(state.displayed(), 0, "underflow acknowledged incomplete frame"); e.stop_and_drain();
}
static void clock_fault_cannot_clear_into_new_lease() {
    Endpoint e; Descriptor d; e.stall = true; e.responses = false; e.submit(d); e.wait_request();
    e.dut.clock_fault = 1; e.idle(8); e.dut.clock_fault = 0; e.idle(8);
    auto state = e.snapshot(); require(state.errors() & FES_DE25_SCANOUT_ERROR_CLOCK, "clock fault lost");
    equal(state.displayed(), 0, "clock fault acknowledged frame"); e.stop_and_drain();
    e.command(FES_DE25_SCANOUT_COMMAND_CLEAR); auto cleared = e.snapshot();
    require(cleared.status() & FES_DE25_SCANOUT_STATUS_CLOCK_FAULT, "clock fault cleared without cold reset");
    require(cleared.errors() & FES_DE25_SCANOUT_ERROR_CLOCK, "clock error cleared without cold reset");
    d.generation++; e.stage(d); e.command(FES_DE25_SCANOUT_COMMAND_SUBMIT);
    equal(e.snapshot().accepted(), 0, "clock fault permitted a new allocation lease");
}
static void display_readiness_loss_stops_without_releasing_traffic() {
    Endpoint e; Descriptor d;
    e.stall = true; e.responses = false; e.submit(d); e.wait_request(); e.idle(2);
    e.dut.stream_ready = 0;
    e.dut.display_ready = 0; e.idle(4);
    auto lost = e.snapshot();
    require(lost.errors() & FES_DE25_SCANOUT_ERROR_DISPLAY, "visibility loss was not latched");
    equal(lost.errors(), FES_DE25_SCANOUT_ERROR_DISPLAY, "readiness-only loss mixed unrelated faults");
    equal(lost.words[8], 0, "HDMI readiness loss invented a scanout underflow");
    require(!(lost.status() & FES_DE25_SCANOUT_STATUS_DRAINED) && lost.outstanding(),
            "visibility loss manufactured a drain proof");
    equal(lost.displayed(), 0, "invisible frame was acknowledged");
    require(e.dut.avm_read, "visibility loss withdrew a held memory command");
    e.stop_and_drain(); e.dut.stream_ready = 1; e.dut.display_ready = 1; e.idle(4);
    require(e.snapshot().errors() & FES_DE25_SCANOUT_ERROR_DISPLAY, "readiness recovery erased the fault");
    e.command(FES_DE25_SCANOUT_COMMAND_CLEAR);
    equal(e.snapshot().errors(), 0, "checked drain could not retire visibility fault");
}

static void configuration_interval_preserves_grant_and_requires_visible_frame() {
    const std::array<std::array<unsigned,2>,4> phases{{{{0,0}},{{641,65}},{{1279,719}},{{1280,719}}}};
    for(const auto& phase:phases) {
        Endpoint e;Descriptor d;e.submit(d);
        auto* r=e.dut.rootp;
        for(unsigned n=0;;++n){
            require(n<3000000,"configuration phase not reached");
            if(r->fes_de25_scanout__DOT__video__DOT__state==3&&
               r->fes_de25_scanout__DOT__video__DOT__internal_h==phase[0]&&
               r->fes_de25_scanout__DOT__video__DOT__internal_v==phase[1])break;
            // The ACK-adoption edge has moved the video to DRAIN.
            if(phase[0]==1280&&r->fes_de25_scanout__DOT__video_frame_presented)break;
            e.idle();
        }
        e.dut.display_ready=0;e.idle(64);
        auto suspended=e.snapshot();equal(suspended.errors(),0,"configuration-only interval faulted");
        equal(suspended.words[8],0,"configuration-only interval underflowed");
        equal(suspended.generation(),d.generation,"configuration changed grant generation");
        equal(suspended.accepted(),d.sequence,"configuration changed accepted publication");
        equal(suspended.displayed(),0,"invisible or stale frame was acknowledged");
        require(suspended.status()&FES_DE25_SCANOUT_STATUS_PENDING,"configuration lost pending slot");
        e.dut.display_ready=1;
        const auto rearm=e.cycles;
        for(unsigned n=0;n<1000;++n)e.idle();
        equal(e.snapshot().displayed(),0,"readiness return adopted old sticky video ACK");
        auto shown=e.wait_displayed(1);
        require(e.cycles-rearm>=719U*1650U+1280U,"completion did not wait a whole fresh visible frame");
        equal(shown.generation(),d.generation,"rearm changed grant");
        equal((shown.slots()>>8)&1U,1,"rearm completed wrong slot");
        e.stop_and_drain();
    }
    // STOP still owns a held command even when TX configuration is unavailable.
    Endpoint e;Descriptor d;e.stall=true;e.responses=false;e.submit(d);e.wait_request();
    e.dut.display_ready=0;e.idle(4);require(e.dut.avm_read,"config withdrew held command");
    e.stop_and_drain();equal(e.snapshot().errors(),0,"STOP during config faulted");
    e.command(FES_DE25_SCANOUT_COMMAND_CLEAR);equal(e.snapshot().generation(),0,"drained config CLEAR failed");
}

static void expect_first_fault(Endpoint& e, const FirstFault& expected) {
    const auto got = e.first_fault();
    for (unsigned i = 0; i < got.words.size(); ++i)
        equal(got.words[i], expected.words[i], "exact first-fault CSR readback");
    equal(got.words[1] & ~FES_DE25_SCANOUT_FIRST_FAULT_OCCUPANCY_MASK, 0, "reserved occupancy bits");
}

static void first_fault_case(const std::string& mode, bool illegal_clear = false) {
    Endpoint e; Descriptor d; e.observe_first_fault = true;
    expect_first_fault(e, {});
    const bool baseline = mode == "baseline";
    const bool saturation = mode.find("saturate") != std::string::npos;
    const bool command = mode == "command" || mode == "saturate-command";
    const bool response = mode == "response" || mode == "saturate-response";
    if (mode == "prefetch" || (saturation && response)) e.responses = false;
    if (saturation) {
        e.hold_blank_for_age_test = true;
        if (command) e.stall = true;
    }
    e.submit(d);
    if (!saturation && mode != "prefetch") {
        e.check_pixels = true;
        for (unsigned i = 0; e.active_pixels < 12000 && i < 200000; ++i) e.idle();
        require(e.active_pixels >= 12000, "first-fault test never reached active display");
        e.check_pixels = false;
        if (command) e.stall = true;
        if (response) e.responses = false;
    }
    if (baseline) e.wait_displayed(1);
    else if (saturation) {
        e.idle(70000);
        e.hold_blank_for_age_test = false;
        e.dut.rootp->fes_de25_scanout__DOT__video__DOT__internal_h = 0;
        e.dut.rootp->fes_de25_scanout__DOT__video__DOT__internal_v = 0;
        e.idle();
    } else e.idle(mode == "prefetch" ? 60000 : 12000);
    const auto first = e.expected_fault;
    expect_first_fault(e, first);
    equal(first.valid(), !baseline, "first-underflow record validity");
    if (!baseline) {
        equal(first.fifo(), 0, "controlled stall must exhaust buffered pixels");
        // The memory fixture's returned beats independently determine the first
        // unavailable pixel; larger bursts change the refill phase/raster edge.
        const auto missing = 4u * e.returned;
        equal(first.h(), saturation ? 0 : missing % 1280, "first missing pixel horizontal position");
        equal(first.v(), saturation ? 0 : missing / 1280, "first missing pixel vertical position");
        equal(first.state(), saturation ? FES_DE25_SCANOUT_FIRST_FAULT_VIDEO_STATE_FETCH :
              (mode == "prefetch" ? FES_DE25_SCANOUT_FIRST_FAULT_VIDEO_STATE_DRAIN :
               FES_DE25_SCANOUT_FIRST_FAULT_VIDEO_STATE_DISPLAY), "pre-cancel video state");
        equal(first.words[0] & FES_DE25_SCANOUT_FIRST_FAULT_PIXEL_VALID_MASK, 0, "exhausted reader pixel validity");
        equal((first.words[0] >> FES_DE25_SCANOUT_FIRST_FAULT_INDEX_RELATION_SHIFT) &
              FES_DE25_SCANOUT_FIRST_FAULT_INDEX_RELATION_MASK,
              FES_DE25_SCANOUT_FIRST_FAULT_INDEX_RELATION_EQUAL, "retained pixel/raster relation");
    }
    if (command) {
        equal(first.outstanding(), 0, "held command is not accepted response credit");
        equal(first.response_gap(), 0, "response-gap age grew without accepted credit");
        equal(first.words[0] & (FES_DE25_SCANOUT_FIRST_FAULT_READ_MASK | FES_DE25_SCANOUT_FIRST_FAULT_WAITREQUEST_MASK),
              FES_DE25_SCANOUT_FIRST_FAULT_READ_MASK | FES_DE25_SCANOUT_FIRST_FAULT_WAITREQUEST_MASK, "held-command flags");
        if (saturation) equal(first.command_age(), 65535, "saturating held-command age");
        else require(first.command_age() > 0, "held command did not accumulate age");
    }
    if (response || mode == "prefetch") {
        equal(first.outstanding(), e.pending.size(), "response stall lost independently queued accepted credit before capture");
        require(first.outstanding() > 0 && first.outstanding() <= 511, "response-stall credit bound");
        equal(first.command_age(), 0, "response stall manufactured held-command age");
        if (saturation) equal(first.response_gap(), 65535, "saturating response gap");
        else require(first.response_gap() > 0, "pending responses did not accumulate gap");
    }
    // Independent read-only CSRs leave the existing snapshot/serial unchanged.
    const auto frozen = e.snapshot();
    const auto serial = e.read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET);
    e.write(FES_DE25_SCANOUT_FIRST_FAULT_STATE_OFFSET, 0);
    e.write(FES_DE25_SCANOUT_FIRST_FAULT_OCCUPANCY_OFFSET, 0xffffffffu);
    e.write(FES_DE25_SCANOUT_FIRST_FAULT_GAPS_OFFSET, 0);
    expect_first_fault(e, first);
    equal(e.read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET), serial, "first-fault CSR access changed snapshot serial");
    for (unsigned i = 0; i < frozen.words.size(); ++i)
        equal(e.read(FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET + 4u * i), frozen.words[i], "first-fault access changed frozen snapshot");
    if (illegal_clear) {
        require(e.snapshot().outstanding() > 0, "illegal CLEAR test needs held or accepted traffic");
        e.command(FES_DE25_SCANOUT_COMMAND_CLEAR);
        expect_first_fault(e, first);
        require(e.snapshot().errors() & FES_DE25_SCANOUT_ERROR_COMMAND, "illegal CLEAR was not rejected");
    }
    e.stall = false; e.responses = true; e.stop_and_drain();
    expect_first_fault(e, first);
    const auto terminal = e.snapshot();
    equal(terminal.words[8], baseline ? 0 : 1, "first-fault capture changed underflow stopping");
    equal(terminal.displayed(), baseline ? 1 : 0, "first-fault capture changed full-frame ACK");
    equal(terminal.outstanding(), 0, "first-fault capture changed ownership/drain");
    equal(terminal.errors(), baseline ? 0 : (illegal_clear ? 10 : 8), "first-fault capture changed fault classification");
    equal(terminal.status(), baseline ? 0x49 : 0x59, "first-fault capture changed terminal status");
    e.idle(1300000); expect_first_fault(e, first);
    e.command(FES_DE25_SCANOUT_COMMAND_CLEAR); expect_first_fault(e, {});
    equal(e.snapshot().errors(), 0, "legal CLEAR retained old fault");
    if (!baseline) {
        e.new_grant_epoch(); e.responses = false; e.submit(d); e.idle(1300000);
        require(e.expected_fault.valid(), "legal CLEAR did not rearm capture without configuration reset");
        expect_first_fault(e, e.expected_fault);
        require(e.expected_fault.response_gap() < 65535 && !e.expected_fault.command_age(), "new fetch retained old age maxima");
    }
    e.reset_epoch(); expect_first_fault(e, {});
    std::cout << "PASS first_fault_" << mode << (illegal_clear ? "_illegal_clear" : "") << '\n';
}

static void first_fault_capture_and_lifetime() {
    for (const std::string mode : {"baseline", "command", "response", "prefetch", "saturate-command", "saturate-response"})
        first_fault_case(mode);
    first_fault_case("command", true);
    first_fault_case("response", true);
}

static void sustained_response_latency(bool old_window) {
    // Controlled memory service, not a measured board latency guarantee. Each
    // accepted burst becomes ready after1500 pixel clocks, then returns ordered
    // at one128-bit beat per clock. The same service starves16/256 and sustains
    //64/511. A full initial FIFO rules out a changed prefill prerequisite.
    Endpoint e; Descriptor d;
    e.max_burst = old_window ? 16 : 64;
    e.response_limit = old_window ? 256 : 511;
    e.response_latency = 1500; e.observe_first_fault = true; e.submit(d);
    unsigned prefill = 0;
    for (unsigned cycle = 0; cycle < 1500000; ++cycle) {
        auto* r = e.dut.rootp;
        if (!prefill && r->fes_de25_scanout__DOT__video__DOT__state == 3)
            prefill = r->fes_de25_scanout__DOT__video__DOT__reader__DOT__count;
        // The old control verifies every delivered pixel before its expected
        // first missing pixel. The candidate verifies every visible pixel.
        e.check_pixels = !old_window || r->fes_de25_scanout__DOT__video__DOT__pixel_matches;
        e.idle();
        if (e.expected_fault.valid() || r->fes_de25_scanout__DOT__video__DOT__displayed_sequence == 1) break;
    }
    e.check_pixels = false;
    equal(prefill, 2048, "latency case did not prefill the entire actual FIFO");
    require(!e.bursts.empty(), "latency case issued no reads");
    equal(e.bursts.front().beats, e.max_burst, "latency case compiled the wrong burst parameter");
    const auto state = e.snapshot();
    if (old_window) {
        require(e.expected_fault.valid(), "old16/256 control no longer reproduces service deficit");
        equal(e.expected_fault.fifo(), 0, "old control failed for a reason other than FIFO exhaustion");
        require(e.expected_fault.outstanding() != 0, "old control had no accepted memory traffic");
        equal(state.displayed(), 0, "old control acknowledged an incomplete frame");
        equal(state.errors(), FES_DE25_SCANOUT_ERROR_UNDERFLOW, "old control fault classification");
    } else {
        require(!e.expected_fault.valid(), "new64/511 window failed modeled service requirement");
        equal(state.errors(), 0, "new window reported memory/underflow errors");
        equal(state.displayed(), 1, "new window did not acknowledge the complete frame");
        equal(e.active_pixels, FES_DE25_SCANOUT_FRAME_BYTES / 4u, "new window skipped/repeated visible pixels");
        equal(e.returned, FES_DE25_SCANOUT_FRAME_BYTES / 16u, "new window fetched incomplete/extra frame data");
    }
    e.stop_and_drain();
    require(e.pending_commands.empty() && e.response_ready.empty(), "latency case left accepted commands after drain");
    std::cout << "PASS modeled1500-cycle service " << (old_window ? "old16/256 exhausts FIFO" : "new64/511 full exact frame")
              << " max_reserved=" << e.max_response_credit << " max_commands=" << e.max_commands << '\n';
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    unsigned groups = 0;
    try {
        if (argc == 2 && (std::string(argv[1]) == "--latency-old" || std::string(argv[1]) == "--latency-new")) {
            sustained_response_latency(std::string(argv[1]) == "--latency-old"); return 0;
        }
#define RUN(name) do { name(); ++groups; std::cout << "PASS " #name "\n"; } while (0)
        RUN(disabled_and_snapshot);
        RUN(display_readiness_loss_stops_without_releasing_traffic);
        RUN(configuration_interval_preserves_grant_and_requires_visible_frame);
        RUN(raster_sync_runs_while_disabled_and_survives_reset);
        RUN(clock_fault_does_not_shorten_active_hsync);
        two_complete_frames(false); ++groups; std::cout << "PASS two_complete_frames_delayed\n";
        two_complete_frames(true); ++groups; std::cout << "PASS two_complete_frames_zero_latency\n";
        RUN(transient_ddr_contention_preserves_complete_frame);
        RUN(stop_keeps_stalled_command_and_accepted_responses);
        RUN(read_ahead_reserves_capacity_and_stop_drains_every_accepted_beat);
        RUN(partial_response_reserves_exact_511_through_held_stop);
        RUN(reset_midburst_restores_idle_submit_and_clear);
        RUN(reset_cancels_a_stalled_command);
        RUN(boundaries_and_low_iova);
        RUN(invalid_descriptors_never_fetch);
        RUN(pending_and_sequence_errors);
        response_error(2); ++groups; std::cout << "PASS response_slverr\n";
        response_error(3); ++groups; std::cout << "PASS response_decerr\n";
        RUN(unexpected_response_is_terminal);
        RUN(underflow_never_completes_frame);
        RUN(clock_fault_cannot_clear_into_new_lease);
        RUN(first_fault_capture_and_lifetime);
        sustained_response_latency(false); ++groups;
        std::cout << "PASS DE25 scanout endpoint: " << groups << " groups\n";
    } catch (const std::exception& error) {
        std::cerr << "FAIL after " << groups << " groups: " << error.what() << '\n'; return 1;
    }
}
