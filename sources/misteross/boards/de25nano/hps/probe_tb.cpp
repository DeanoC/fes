// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_ddr_probe.h"
#include "generated/fes_de25_ddr_probe.hpp"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <deque>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

namespace r = mister::native::generated;

static void require(bool value, const std::string& message) {
    if (!value) throw std::runtime_error(message);
}

static void expect_equal(std::uint64_t actual, std::uint64_t expected, const std::string& message) {
    if (actual != expected) {
        std::ostringstream detail;
        detail << message << ": got 0x" << std::hex << actual << ", expected 0x" << expected;
        throw std::runtime_error(detail.str());
    }
}

// Model each 32-bit memory word independently; lane order and upper/lower
// halves of an adapted 256-bit word must not accidentally share a pattern.
static std::uint32_t memory_word(std::uint32_t byte_address) {
    const std::uint32_t mixed = (byte_address / 4u) * 0x9e3779b9u + 0x6d2b79f5u;
    return (mixed << 7) | (mixed >> 25);
}

struct Descriptor {
    std::uint64_t base = 0x10000000;
    std::uint32_t span = 8192, offset = 0, bytes = 512;
    std::uint64_t generation = 0x123456789abcdef0ull;
    std::uint32_t sequence = 7;
};

struct Burst { std::uint32_t address, beats; };

class Probe {
public:
    Vfes_de25_ddr_probe dut;
    bool stall = true, responses = false, zero_latency = false, unsolicited = false;
    int error_response = -1;
    std::vector<Burst> bursts;
    std::deque<std::uint32_t> pending;
    std::uint32_t returned = 0, expected_checksum = 0;
    std::array<std::uint32_t, 4> expected_last{};

    Probe() {
        dut.clk = 0; dut.reset_n = 0;
        dut.read = 0; dut.write = 0; dut.address = 0; dut.byteenable = 0; dut.writedata = 0;
        dut.avm_waitrequest = 1; dut.avm_readdatavalid = 0; dut.avm_response = 0;
        for (unsigned lane = 0; lane < 4; ++lane) dut.avm_readdata[lane] = 0;
        dut.eval(); tick(); tick();
        require(dut.waitrequest && !dut.readdatavalid, "reset did not block control access");
        dut.reset_n = 1; tick();
        expect_equal(read(r::FesDe25DdrProbeStatusOffset), r::FesDe25DdrProbeStatusIdle, "reset status");
    }
    ~Probe() { dut.final(); }

    void tick() {
        dut.clk = 0;
        dut.avm_waitrequest = stall;
        dut.avm_readdatavalid = 0;
        dut.avm_response = 0;
        dut.eval();
        const bool accepts = dut.reset_n && dut.avm_read && !stall;
        const bool old_pending = !pending.empty();
        if (dut.reset_n) {
            if (held_) {
                require(dut.avm_read, "stalled request was withdrawn");
                expect_equal(dut.avm_address, held_address_, "stalled address changed");
                expect_equal(dut.avm_burstcount, held_beats_, "stalled burst length changed");
                expect_equal(dut.avm_byteenable, 0xffff, "stalled byte enables changed");
            }
            if (dut.avm_read) {
                require(bounded_, "master read without a valid test descriptor");
                require(dut.avm_burstcount >= 1 && dut.avm_burstcount <= 16, "invalid burst size");
                require((dut.avm_address & 15) == 0, "unaligned master address");
                require((dut.avm_address & 4095) + 16u * dut.avm_burstcount <= 4096,
                        "burst crossed a 4 KiB boundary");
                require(std::uint64_t(dut.avm_address) >= lower_ &&
                        std::uint64_t(dut.avm_address) + 16u * dut.avm_burstcount <= upper_,
                        "master read outside the requested byte range");
                expect_equal(dut.avm_byteenable, 0xffff, "read byte enables");
            }
            if (accepts) {
                require(pending.empty(), "more than one accepted burst is outstanding");
                expect_equal(dut.avm_address, expected_next_, "accepted read skipped or repeated bytes");
                bursts.push_back({dut.avm_address, dut.avm_burstcount});
                for (unsigned beat = 0; beat < dut.avm_burstcount; ++beat)
                    pending.push_back(dut.avm_address + beat * 16u);
                expected_next_ += 16u * dut.avm_burstcount;
            }
            if (responses && (old_pending || (zero_latency && accepts))) {
                const auto address = pending.front();
                pending.pop_front();
                for (unsigned lane = 0; lane < 4; ++lane) {
                    expected_last[lane] = memory_word(address + lane * 4u);
                    dut.avm_readdata[lane] = expected_last[lane];
                    expected_checksum ^= expected_last[lane];
                }
                dut.avm_readdatavalid = 1;
                dut.avm_response = int(returned) == error_response ? 2 : 0;
                ++returned;
            } else if (unsolicited) {
                require(pending.empty() && !accepts, "unsolicited injection is not unsolicited");
                dut.avm_readdatavalid = 1;
            }
            held_ = dut.avm_read && stall;
            held_address_ = dut.avm_address;
            held_beats_ = dut.avm_burstcount;
        }
        dut.eval(); dut.clk = 1; dut.eval(); dut.clk = 0; dut.eval();
    }

    void idle(unsigned clocks = 1) {
        dut.read = 0; dut.write = 0;
        while (clocks--) tick();
    }
    void write(std::uint32_t address, std::uint32_t value, std::uint8_t enables = 15) {
        dut.address = address; dut.writedata = value; dut.byteenable = enables;
        dut.write = 1; dut.read = 0; tick();
        require(!dut.readdatavalid, "control write generated a read response");
        idle();
    }
    std::uint32_t read(std::uint32_t address) {
        dut.address = address; dut.read = 1; dut.write = 0; tick();
        require(dut.readdatavalid && !dut.waitrequest, "control read lacked a registered response");
        const std::uint32_t value = dut.readdata;
        idle();
        return value;
    }
    void stage(const Descriptor& d) {
        write(r::FesDe25DdrProbeGrantLoOffset, std::uint32_t(d.base));
        write(r::FesDe25DdrProbeGrantHiOffset, std::uint32_t(d.base >> 32));
        write(r::FesDe25DdrProbeGrantSpanOffset, d.span);
        write(r::FesDe25DdrProbeReadOffsetOffset, d.offset);
        write(r::FesDe25DdrProbeReadBytesOffset, d.bytes);
        write(r::FesDe25DdrProbeGenerationLoOffset, std::uint32_t(d.generation));
        write(r::FesDe25DdrProbeGenerationHiOffset, std::uint32_t(d.generation >> 32));
        write(r::FesDe25DdrProbeSequenceOffset, d.sequence);
    }
    void start(const Descriptor& d) {
        stage(d);
        lower_ = d.base + d.offset; upper_ = lower_ + d.bytes; expected_next_ = lower_; bounded_ = true;
        write(r::FesDe25DdrProbeCommandOffset, r::FesDe25DdrProbeCommandStart);
    }
    std::uint32_t wait_idle(unsigned limit = 4000) {
        for (unsigned clock = 0; clock < limit; ++clock) {
            const auto status = read(r::FesDe25DdrProbeStatusOffset);
            if (status & r::FesDe25DdrProbeStatusIdle) {
                require(!dut.avm_read && pending.empty(), "IDLE before accepted traffic drained");
                return status;
            }
        }
        throw std::runtime_error("operation did not drain within the test bound");
    }
    void successful(const Descriptor& d) {
        const auto status = wait_idle();
        expect_equal(status, r::FesDe25DdrProbeStatusIdle | r::FesDe25DdrProbeStatusDone, "successful status");
        expect_equal(read(r::FesDe25DdrProbeCompletedBytesOffset), d.bytes, "completed byte count");
        expect_equal(read(r::FesDe25DdrProbeAcceptedBytesOffset), d.bytes, "accepted byte count");
        expect_equal(read(r::FesDe25DdrProbeChecksumOffset), expected_checksum, "XOR32 checksum");
        expect_equal(read(r::FesDe25DdrProbeErrorOffset), 0, "successful error flags");
        for (unsigned lane = 0; lane < 4; ++lane)
            expect_equal(read(r::FesDe25DdrProbeLastData0Offset + lane * 4), expected_last[lane], "last returned lane");
        expect_equal(read(r::FesDe25DdrProbeActiveGenerationLoOffset), std::uint32_t(d.generation), "active generation low");
        expect_equal(read(r::FesDe25DdrProbeActiveGenerationHiOffset), std::uint32_t(d.generation >> 32), "active generation high");
        expect_equal(read(r::FesDe25DdrProbeActiveSequenceOffset), d.sequence, "active sequence");
        const auto count = bursts.size(); idle(20); expect_equal(bursts.size(), count, "read continued after completion");
    }
private:
    bool held_ = false, bounded_ = false;
    std::uint32_t held_address_ = 0, held_beats_ = 0;
    std::uint64_t lower_ = 0, upper_ = 0, expected_next_ = 0;
};

static void register_access() {
    Probe p;
    expect_equal(p.read(r::FesDe25DdrProbeIdOffset), r::FesDe25DdrProbeIdentity, "identity");
    expect_equal(p.read(r::FesDe25DdrProbeVersionOffset), r::FesDe25DdrProbeVersion, "version");
    expect_equal(p.read(3), 0, "misaligned read"); expect_equal(p.read(0x7c), 0, "reserved read");
    p.write(r::FesDe25DdrProbeGrantLoOffset, 0x12345678);
    p.write(r::FesDe25DdrProbeGrantLoOffset, 0xaabbccdd, 5);
    expect_equal(p.read(r::FesDe25DdrProbeGrantLoOffset), 0x12bb56dd, "byte-enabled staged write");
    p.write(r::FesDe25DdrProbeGrantLoOffset, 0, 0);
    expect_equal(p.read(r::FesDe25DdrProbeGrantLoOffset), 0x12bb56dd, "zero byte enable changed staging");
    p.write(r::FesDe25DdrProbeIdOffset, 0);
    expect_equal(p.read(r::FesDe25DdrProbeIdOffset), r::FesDe25DdrProbeIdentity, "identity is writable");
    p.write(r::FesDe25DdrProbeCommandOffset, r::FesDe25DdrProbeCommandStart, 0);
    expect_equal(p.read(r::FesDe25DdrProbeStatusOffset), r::FesDe25DdrProbeStatusIdle, "masked command executed");
}

static void invalid_descriptors() {
    std::vector<Descriptor> invalid;
    Descriptor d;
    d.base = 0x100000000ull; invalid.push_back(d);
    d = {}; d.base += 16; invalid.push_back(d);
    d = {}; d.span = 0; invalid.push_back(d);
    d = {}; d.span = 17; invalid.push_back(d);
    d = {}; d.offset = 1; invalid.push_back(d);
    d = {}; d.bytes = 0; invalid.push_back(d);
    d = {}; d.bytes = 17; invalid.push_back(d);
    d = {}; d.generation = 0; invalid.push_back(d);
    d = {}; d.sequence = 0; invalid.push_back(d);
    d = {}; d.offset = 8192; d.bytes = 16; invalid.push_back(d);
    d = {}; d.offset = 8176; d.bytes = 32; invalid.push_back(d);
    d = {}; d.offset = 0xfffffff0u; d.bytes = 32; invalid.push_back(d);
    d = {}; d.base = 0xffffffe0; d.span = 48; d.bytes = 16; invalid.push_back(d);
    for (unsigned index = 0; index < invalid.size(); ++index) {
        Probe p; p.stage(invalid[index]);
        p.write(r::FesDe25DdrProbeCommandOffset, r::FesDe25DdrProbeCommandStart); p.idle(10);
        expect_equal(p.read(r::FesDe25DdrProbeStatusOffset),
              r::FesDe25DdrProbeStatusIdle | r::FesDe25DdrProbeStatusFault,
              "invalid descriptor status " + std::to_string(index));
        expect_equal(p.read(r::FesDe25DdrProbeErrorOffset), r::FesDe25DdrProbeErrorDescriptor, "descriptor error");
        require(p.bursts.empty() && !p.dut.avm_read, "invalid descriptor presented a master request");
        p.write(r::FesDe25DdrProbeCommandOffset, r::FesDe25DdrProbeCommandClear);
        expect_equal(p.read(r::FesDe25DdrProbeStatusOffset), r::FesDe25DdrProbeStatusIdle, "CLEAR did not clear idle fault");
    }
}

static void successful_bounds_and_lanes(bool zero_latency) {
    std::vector<Descriptor> cases;
    Descriptor d; d.offset = 16; d.bytes = 64; cases.push_back(d);
    d = {}; d.base = 0; d.span = 16; d.bytes = 16; cases.push_back(d);
    d = {}; d.base = 0xffffffe0; d.span = 32; d.offset = 16; d.bytes = 16; cases.push_back(d);
    d = {}; d.bytes = 16; d.generation = 0x100000000ull; d.sequence = 0xffffffffu; cases.push_back(d);
    for (const auto& descriptor : cases) {
        Probe p; p.zero_latency = zero_latency; p.start(descriptor);
        p.stall = false; p.responses = true; p.successful(descriptor);
    }
}

static void boundary_splitting() {
    Probe p; Descriptor d; d.offset = 4080; d.bytes = 544;
    p.start(d); p.stall = false; p.responses = true; p.successful(d);
    require(p.bursts.size() == 4, "unexpected 4 KiB split count");
    const std::array<unsigned, 4> lengths{{1, 16, 16, 1}};
    const std::array<unsigned, 4> offsets{{4080, 4096, 4352, 4608}};
    for (unsigned n = 0; n < lengths.size(); ++n) {
        expect_equal(p.bursts[n].beats, lengths[n], "boundary split length");
        expect_equal(p.bursts[n].address, d.base + offsets[n], "boundary split address");
    }
}

static void staged_snapshot() {
    Probe p; Descriptor d; d.bytes = 32;
    p.start(d); require(p.dut.avm_read, "expected staged operation to issue a request");
    Descriptor changed; changed.base = 0x70000000; changed.offset = 16;
    changed.generation = 9; changed.sequence = 10;
    p.stage(changed); p.idle(10);
    expect_equal(p.read(r::FesDe25DdrProbeGrantLoOffset), changed.base, "busy staged writes were lost");
    expect_equal(p.read(r::FesDe25DdrProbeActiveSequenceOffset), d.sequence, "staging changed active sequence");
    p.stall = false; p.responses = true; p.successful(d);
}

static void cancelled_stalled_request() {
    Probe p; Descriptor d;
    p.start(d); require(p.dut.avm_read, "no stalled request to cancel");
    const auto address = p.dut.avm_address;
    const auto beats = p.dut.avm_burstcount;
    p.write(r::FesDe25DdrProbeCommandOffset, r::FesDe25DdrProbeCommandCancel); p.idle(12);
    require(p.dut.avm_read && p.pending.empty(), "cancel withdrew an unaccepted request");
    expect_equal(p.dut.avm_address, address, "cancel changed stalled address");
    expect_equal(p.dut.avm_burstcount, beats, "cancel changed stalled burst");
    auto status = p.read(r::FesDe25DdrProbeStatusOffset);
    expect_equal(status, r::FesDe25DdrProbeStatusBusy | r::FesDe25DdrProbeStatusCancelled, "stalled cancel status");
    p.stall = false; p.idle(1); p.idle(20);
    expect_equal(p.read(r::FesDe25DdrProbeCompletedBytesOffset), 0, "missing responses counted as complete");
    require(p.read(r::FesDe25DdrProbeStatusOffset) & r::FesDe25DdrProbeStatusBusy, "missing responses lost BUSY");
    p.responses = true;
    status = p.wait_idle();
    expect_equal(status, r::FesDe25DdrProbeStatusIdle | r::FesDe25DdrProbeStatusCancelled, "cancel drain became successful DONE");
    expect_equal(p.bursts.size(), 1, "cancel allowed a new burst");
    expect_equal(p.read(r::FesDe25DdrProbeCompletedBytesOffset), beats * 16u, "cancelled accepted burst not drained");
}

static void busy_command_fault(std::uint32_t command, std::uint32_t expected_error) {
    Probe p; Descriptor d;
    p.start(d); p.stall = false; p.idle(1);
    require(!p.pending.empty(), "no accepted traffic for busy-command test");
    p.write(r::FesDe25DdrProbeCommandOffset, command); p.idle(20);
    expect_equal(p.read(r::FesDe25DdrProbeStatusOffset),
          r::FesDe25DdrProbeStatusBusy | r::FesDe25DdrProbeStatusFault, "busy command abandoned accepted traffic");
    p.responses = true;
    expect_equal(p.wait_idle(), r::FesDe25DdrProbeStatusIdle | r::FesDe25DdrProbeStatusFault,
          "busy fault drain became successful DONE");
    expect_equal(p.read(r::FesDe25DdrProbeErrorOffset), expected_error, "busy-command error flags");
    expect_equal(p.bursts.size(), 1, "fault allowed a later burst");
    expect_equal(p.read(r::FesDe25DdrProbeCompletedBytesOffset), p.returned * 16u, "fault drain count");
}

static void response_error(int position) {
    Probe p; Descriptor d;
    p.error_response = position; p.start(d); p.stall = false; p.responses = true;
    expect_equal(p.wait_idle(), r::FesDe25DdrProbeStatusIdle | r::FesDe25DdrProbeStatusFault,
          "read-response error became successful DONE");
    expect_equal(p.read(r::FesDe25DdrProbeErrorOffset), r::FesDe25DdrProbeErrorResponse, "response error flag");
    expect_equal(p.bursts.size(), 1, "response error failed to stop new bursts");
    expect_equal(p.read(r::FesDe25DdrProbeCompletedBytesOffset), 256, "response error failed to drain all accepted beats");
}

static void unsolicited_response() {
    Probe p;
    p.unsolicited = true; p.idle(); p.unsolicited = false;
    expect_equal(p.read(r::FesDe25DdrProbeStatusOffset), r::FesDe25DdrProbeStatusIdle | r::FesDe25DdrProbeStatusFault,
          "unsolicited response status");
    expect_equal(p.read(r::FesDe25DdrProbeErrorOffset), r::FesDe25DdrProbeErrorUnexpectedResponse, "unsolicited error");
    expect_equal(p.read(r::FesDe25DdrProbeCompletedBytesOffset), 0, "unsolicited response changed completion count");
    expect_equal(p.read(r::FesDe25DdrProbeChecksumOffset), 0, "unsolicited response changed checksum");
}

static void clear_does_not_erase_same_edge_fault(bool prior_response_error) {
    Probe p; Descriptor d; d.bytes = 256;
    if (prior_response_error) p.error_response = 0;
    p.start(d); p.stall = false; p.responses = true;
    const auto expected_status = r::FesDe25DdrProbeStatusIdle |
        (prior_response_error ? r::FesDe25DdrProbeStatusFault : r::FesDe25DdrProbeStatusDone);
    expect_equal(p.wait_idle(), expected_status, "setup operation status");

    const auto completed = p.read(r::FesDe25DdrProbeCompletedBytesOffset);
    const auto accepted = p.read(r::FesDe25DdrProbeAcceptedBytesOffset);
    const auto checksum = p.read(r::FesDe25DdrProbeChecksumOffset);
    std::array<std::uint32_t, 4> last{};
    for (unsigned lane = 0; lane < last.size(); ++lane)
        last[lane] = p.read(r::FesDe25DdrProbeLastData0Offset + lane * 4);

    p.dut.address = r::FesDe25DdrProbeCommandOffset;
    p.dut.writedata = r::FesDe25DdrProbeCommandClear;
    p.dut.byteenable = 15;
    p.dut.write = 1; p.dut.read = 0;
    p.unsolicited = true;
    p.tick();
    require(!p.dut.readdatavalid, "same-edge CLEAR generated a control read response");
    p.unsolicited = false;
    p.idle();
    expect_equal(p.read(r::FesDe25DdrProbeStatusOffset),
          r::FesDe25DdrProbeStatusIdle | r::FesDe25DdrProbeStatusFault,
          "same-edge fault was cleared");
    const auto expected_errors = r::FesDe25DdrProbeErrorUnexpectedResponse |
        (prior_response_error ? r::FesDe25DdrProbeErrorResponse : 0);
    expect_equal(p.read(r::FesDe25DdrProbeErrorOffset), expected_errors,
          "same-edge CLEAR lost fault reasons");
    expect_equal(p.read(r::FesDe25DdrProbeCompletedBytesOffset), completed,
          "same-edge CLEAR erased completion accounting");
    expect_equal(p.read(r::FesDe25DdrProbeAcceptedBytesOffset), accepted,
          "same-edge CLEAR erased accepted accounting");
    expect_equal(p.read(r::FesDe25DdrProbeChecksumOffset), checksum,
          "same-edge CLEAR erased checksum");
    for (unsigned lane = 0; lane < last.size(); ++lane)
        expect_equal(p.read(r::FesDe25DdrProbeLastData0Offset + lane * 4), last[lane],
              "same-edge CLEAR erased last data");
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        register_access(); invalid_descriptors();
        successful_bounds_and_lanes(false); successful_bounds_and_lanes(true);
        boundary_splitting(); staged_snapshot(); cancelled_stalled_request();
        busy_command_fault(r::FesDe25DdrProbeCommandStart, r::FesDe25DdrProbeErrorBusyCommand);
        busy_command_fault(r::FesDe25DdrProbeCommandClear, r::FesDe25DdrProbeErrorBusyCommand);
        busy_command_fault(3, r::FesDe25DdrProbeErrorCommand);
        response_error(0); response_error(15); unsolicited_response();
        clear_does_not_erase_same_edge_fault(false);
        clear_does_not_erase_same_edge_fault(true);
        std::cout << "DE25 DDR probe: 15 groups passed (host RTL simulation only)\n";
        return 0;
    } catch (const std::exception& error) {
        std::cerr << "DE25 DDR probe: " << error.what() << '\n';
        return 1;
    }
}
