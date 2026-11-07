// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_scanout.h"
#include "generated/fes_de25_scanout.h"
#include "verilated.h"

#include <array>
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

    Endpoint() {
        dut.clk = 0; dut.reset_n = 0; dut.clock_fault = 0;
        dut.address = 0; dut.read = 0; dut.write = 0; dut.byteenable = 0; dut.writedata = 0;
        dut.avm_waitrequest = 0; dut.avm_readdatavalid = 0; dut.avm_response = 0;
        for (unsigned lane = 0; lane < 4; ++lane) dut.avm_readdata[lane] = 0;
        dut.eval(); tick(); tick(); dut.reset_n = 1; idle(4);
        equal(read(FES_DE25_SCANOUT_ID_OFFSET), FES_DE25_SCANOUT_IDENTITY, "identity");
        equal(read(FES_DE25_SCANOUT_VERSION_OFFSET), FES_DE25_SCANOUT_VERSION, "version");
        equal(read(FES_DE25_SCANOUT_STATUS_OFFSET), FES_DE25_SCANOUT_STATUS_DRAINED, "cold state");
        require(bursts.empty() && !dut.avm_read, "cold endpoint fetched memory");
    }
    ~Endpoint() { dut.final(); }

    void tick() {
        dut.clk = 0; dut.avm_waitrequest = stall;
        dut.avm_readdatavalid = 0; dut.avm_response = 0; dut.eval();
        const bool accepts = dut.reset_n && dut.avm_read && !stall;
        const bool old_response = !pending.empty();
        if (dut.reset_n) {
            if (held_) {
                require(dut.avm_read, "stalled command withdrawn");
                equal(dut.avm_address, held_address_, "stalled address changed");
                equal(dut.avm_burstcount, held_beats_, "stalled burst changed");
                equal(dut.avm_byteenable, 0xffff, "stalled byte enable changed");
            }
            if (dut.avm_read) {
                require(bounded_, "memory access without an accepted prospective grant");
                require(dut.avm_burstcount && dut.avm_burstcount <= FES_DE25_SCANOUT_MAX_BURST_BEATS,
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
                require(pending.empty(), "more than one accepted burst outstanding");
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
                for (unsigned beat = 0; beat < dut.avm_burstcount; ++beat)
                    pending.push_back(dut.avm_address + beat * 16u);
                next_address_ += 16u * dut.avm_burstcount;
            }
            if (responses && (old_response || (zero_latency && accepts))) {
                const auto address = pending.front(); pending.pop_front();
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
        dut.clk = 1; dut.eval(); dut.clk = 0; dut.eval();
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
        pending.clear(); held_ = false; have_frame_ = false; bounded_ = false;
        allowed_slots_ = 0; frame_start_ = 0; next_address_ = 0;
        idle(2);
        dut.reset_n = 1; dut.eval(); idle(4);
    }
private:
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
    equal(empty.status(), FES_DE25_SCANOUT_STATUS_DRAINED, "CLEAR empty state");
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
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    unsigned groups = 0;
    try {
#define RUN(name) do { name(); ++groups; std::cout << "PASS " #name "\n"; } while (0)
        RUN(disabled_and_snapshot);
        RUN(raster_sync_runs_while_disabled_and_survives_reset);
        RUN(clock_fault_does_not_shorten_active_hsync);
        two_complete_frames(false); ++groups; std::cout << "PASS two_complete_frames_delayed\n";
        two_complete_frames(true); ++groups; std::cout << "PASS two_complete_frames_zero_latency\n";
        RUN(stop_keeps_stalled_command_and_accepted_responses);
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
        std::cout << "PASS DE25 scanout endpoint: " << groups << " groups\n";
    } catch (const std::exception& error) {
        std::cerr << "FAIL after " << groups << " groups: " << error.what() << '\n'; return 1;
    }
}
