// Generated CDC logic and real scanout WITH MODELED M20K STORAGE/AXI256 RAM.
#include "Vfes_de25_scanout_interconnect_test.h"
#include "verilated.h"
#include "generated/fes_de25_scanout.h"
#include <array>
#include <cstdint>
#include <deque>
#include <iostream>
#include <stdexcept>
#include <string>

static void require(bool ok, const std::string& message) {
    if (!ok) throw std::runtime_error(message);
}

static uint32_t pixel_word(unsigned slot, unsigned index) {
    return 0xff000000U | (slot << 23) | ((index * 1103515245U ^ (index >> 7)) & 0x7fffffU);
}

struct Burst { uint32_t address; unsigned beats, size, id, position = 0; uint64_t ready_cycle = 0; };
struct Snapshot {
    uint32_t serial, status, accepted, displayed, current, slots, errors, underflows, outstanding;
    uint64_t generation, dma;
    uint32_t span;
};

struct Simulation {
    Vfes_de25_scanout_interconnect_test dut;
    std::deque<Burst> pending;
    std::deque<uint32_t> endpoint_expected;
    // Exact frequency ratio 74.25/100 = 297/400, with an independent phase.
    // Simulation ticks are arbitrary rational time units, not timing signoff.
    uint64_t time = 0, next_mem = 297, next_pixel = 417;
    uint64_t mem_cycles = 0, pixel_cycles = 0;
    unsigned response_latency = 0;
    uint32_t base = 0x08000fe0U;
    uint64_t generation = 0x9e3779b912345678ULL;
    bool pixel_stopped = false, hold_responses = false, holding_response = false;
    bool mem_rise = false, accepted_control = false, control_response = false;
    uint32_t control_data = 0;
    unsigned injected_response = 0, error_beats = 0, endpoint_error_beats = 0;
    uint64_t axi_bursts = 0, axi_beats = 0, endpoint_beats = 0, ar_stalls = 0, r_stalls = 0;
    unsigned visible_index = 0, visible_slot = 0;
    std::array<unsigned, 2> complete_frames{};
    std::array<unsigned, 2> frames_at_accept{};
    uint32_t observed_accepted = 0, observed_displayed = 0;
    bool check_pixels = true, ar_stalled = false;
    bool ram_storage_self_test_passed = false;
    Burst held_ar{};

    Simulation() {
        dut.clk_mem = 0; dut.clk_pixel = 0; dut.reset_n = 0; dut.clock_fault = 0;
        dut.address = 0; dut.read = 0; dut.write = 0;
        dut.byteenable = 15; dut.writedata = 0;
        dut.ram_test_write_address = 0; dut.ram_test_read_address = 2;
        dut.ram_test_data = 0; dut.ram_test_write = 0; dut.ram_test_clock_enable = 1;
        dut.arready = 0; dut.rvalid = 0; dut.rlast = 0; dut.rresp = 0; dut.rid = 0;
        for (unsigned lane = 0; lane < 8; ++lane) dut.rdata[lane] = 0;
        cycles(32);
        dut.reset_n = 1;
        cycles(64);
    }

    uint32_t memory_word(uint32_t address) const {
        // Model every 32-bit lane independently; bytes outside a narrow AXI
        // transfer are irrelevant, but actual transferred footprints are bounded.
        uint32_t relative = address - base;
        unsigned slot = relative / FES_DE25_SCANOUT_SLOT_BYTES;
        unsigned index = relative % FES_DE25_SCANOUT_SLOT_BYTES / 4;
        return pixel_word(slot & 1, index);
    }

    void step() {
        bool memory_event = next_mem <= next_pixel;
        bool pixel_event = next_pixel <= next_mem;
        time = memory_event ? next_mem : next_pixel;
        mem_rise = memory_event && !dut.clk_mem;
        bool pixel_rise = pixel_event && !pixel_stopped && !dut.clk_pixel;
        accepted_control = false; control_response = false;
        // Withholding responses must not prevent AXI command acceptance:
        // STOP/stall cases own real accepted AXI reads before cancellation.
        dut.arready = mem_cycles % 17 != 3;
        if (!holding_response && !hold_responses && !pending.empty() && mem_cycles >= pending.front().ready_cycle && mem_cycles % 11 != 0) {
            const auto& burst = pending.front();
            uint32_t address = burst.address + (burst.position << burst.size);
            for (unsigned lane = 0; lane < 8; ++lane) {
                uint32_t lane_address = (address & ~31U) + lane * 4;
                // Only the selected narrow-transfer lanes contain memory data.
                // Poison others so a wrong AR lane cannot be hidden by filling
                // unrelated bytes with otherwise plausible neighboring data.
                dut.rdata[lane] = lane_address >= address && uint64_t(lane_address) < uint64_t(address) + 16U ?
                    memory_word(lane_address) : (0xdeadbeefU ^ lane);
            }
            dut.rid = burst.id;
            dut.rlast = burst.position + 1 == burst.beats;
            dut.rresp = axi_beats == 0 ? injected_response : 0;
            holding_response = true;
        }
        dut.rvalid = holding_response && !hold_responses;
        dut.eval();
        bool ar_accepted = mem_rise && dut.arvalid && dut.arready;
        bool r_accepted = mem_rise && dut.rvalid && dut.rready;
        Burst new_burst{dut.araddr, unsigned(dut.arlen) + 1, dut.arsize, dut.arid};
        if (mem_rise && dut.reset_n) {
            accepted_control = (dut.read || dut.write) && !dut.waitrequest;
            control_response = dut.readdatavalid;
            control_data = dut.readdata;
            require(!dut.awvalid && !dut.wvalid, "read-only scanout issued AXI write");
            if (ar_stalled)
                require(dut.arvalid && dut.araddr == held_ar.address && unsigned(dut.arlen) + 1 == held_ar.beats &&
                        dut.arsize == held_ar.size && dut.arid == held_ar.id, "AR changed under backpressure");
            ar_stalled = dut.arvalid && !dut.arready;
            held_ar = new_burst;
            if (ar_stalled) ++ar_stalls;
            if (dut.rvalid && !dut.rready) ++r_stalls;
            if (ar_accepted) {
                require(new_burst.size == 4 && dut.arburst == 1, "scanout must retain narrow 16-byte AXI reads");
                require((new_burst.address & 15U) == 0, "unaligned AXI read changes the actual byte footprint");
                uint64_t end = uint64_t(new_burst.address) + (uint64_t(new_burst.beats) << new_burst.size);
                require(end <= (uint64_t(new_burst.address) & ~4095ULL) + 4096, "AXI burst crosses 4 KiB");
                require(new_burst.address >= base && end <= uint64_t(base) + FES_DE25_SCANOUT_ALLOCATION_BYTES,
                        "actual AXI footprint exceeds allocated grant");
                require(new_burst.beats <= FES_DE25_SCANOUT_MAX_BURST_BEATS, "unexpected AXI burst length");
                ++axi_bursts;
            }
        }
        if (pixel_rise && dut.reset_n) {
            if (dut.endpoint_read && !dut.endpoint_waitrequest) {
                unsigned beats = dut.endpoint_burstcount;
                uint64_t end = uint64_t(dut.endpoint_address) + beats * 16;
                require(beats && beats <= 16 && dut.endpoint_address >= base &&
                        end <= uint64_t(base) + FES_DE25_SCANOUT_ALLOCATION_BYTES,
                        "endpoint read exceeds allocated grant");
                require(end <= (uint64_t(dut.endpoint_address) & ~4095ULL) + 4096,
                        "endpoint burst crosses 4 KiB");
                for (unsigned beat = 0; beat < beats; ++beat)
                    endpoint_expected.push_back(dut.endpoint_address + beat * 16);
            }
            if (dut.endpoint_readdatavalid) {
                require(!endpoint_expected.empty(), "generated transport returned an unsolicited beat");
                uint32_t expected = endpoint_expected.front();
                endpoint_expected.pop_front();
                for (unsigned lane = 0; lane < 4; ++lane)
                    require(dut.endpoint_readdata[lane] == memory_word(expected + lane * 4),
                            "128-bit data/lane changed across width/clock adapters");
                ++endpoint_beats;
                if (dut.endpoint_response) {
                    require(dut.endpoint_response == injected_response,
                            "two-bit RRESP changed across width/clock adapters");
                    ++endpoint_error_beats;
                }
            }
        }
        if (pixel_rise && dut.reset_n && dut.video_de && check_pixels) {
            if (!visible_index) visible_slot = (dut.video_rgb >> 23) & 1;
            require(dut.video_rgb == (pixel_word(visible_slot, visible_index) & 0xffffffU),
                    "wrong full-frame pixel/lane at index " + std::to_string(visible_index));
            if (++visible_index == FES_DE25_SCANOUT_FRAME_WIDTH * FES_DE25_SCANOUT_FRAME_HEIGHT) {
                ++complete_frames[visible_slot];
                visible_index = 0;
            }
        }
        if (memory_event) { dut.clk_mem = !dut.clk_mem; next_mem += 297; }
        if (pixel_event) {
            if (!pixel_stopped) dut.clk_pixel = !dut.clk_pixel;
            next_pixel += 400;
        }
        dut.eval();
        if (pixel_rise && dut.reset_n) {
            if (dut.endpoint_accepted_sequence != observed_accepted) {
                observed_accepted = dut.endpoint_accepted_sequence;
                frames_at_accept = complete_frames;
            }
            if (dut.endpoint_displayed_sequence != observed_displayed) {
                observed_displayed = dut.endpoint_displayed_sequence;
                if (observed_displayed) {
                    unsigned slot = dut.endpoint_displayed_slot;
                    require(observed_displayed == observed_accepted &&
                            slot == dut.endpoint_pending_slot && complete_frames[slot] > frames_at_accept[slot],
                            "actual displayed ACK preceded the newly accepted full pixel frame");
                }
            }
        }
        if (r_accepted) {
            require(!pending.empty(), "modeled AXI response has no request");
            if (dut.rresp) ++error_beats;
            if (++pending.front().position == pending.front().beats) pending.pop_front();
            ++axi_beats;
            holding_response = false;
        }
        if (ar_accepted) { new_burst.ready_cycle = mem_cycles + response_latency; pending.push_back(new_burst); }
        if (mem_rise) ++mem_cycles;
        if (pixel_rise) ++pixel_cycles;
    }

    void cycles(uint64_t count) {
        uint64_t end = mem_cycles + count;
        while (mem_cycles < end) step();
    }

    void test_ram_storage_contract() {
        // Direct self-test of the explicit storage model, not of a vendor
        // primitive. No same-address mixed-port collision is used as evidence.
        dut.ram_test_clock_enable = 0;  // BYPASS must ignore these enable pins.
        auto ram_write = [&](unsigned address, unsigned data, bool enable) {
            dut.ram_test_write_address = address; dut.ram_test_data = data;
            dut.ram_test_write = enable; dut.eval();
            do { step(); } while (!mem_rise);
            dut.ram_test_write = 0; dut.eval();
        };
        auto read_edge = [&]() {
            uint64_t before = pixel_cycles;
            while (pixel_cycles == before) step();
        };
        ram_write(0, 0x3c, true); ram_write(1, 0xa5, true);
        dut.ram_test_read_address = 0; read_edge();
        require(dut.ram_test_q == 0x3c, "RAM registered-address/unregistered-output latency differs");
        while (dut.clk_pixel) step();
        pixel_stopped = true; dut.ram_test_read_address = 1; cycles(4);
        require(dut.ram_test_q == 0x3c, "RAM read address changed without its clock edge");
        ram_write(2, 0x5a, true); ram_write(1, 0x99, false);
        require(dut.ram_test_q == 0x3c, "independent writes changed the selected unrelated RAM word");
        pixel_stopped = false; read_edge();
        require(dut.ram_test_q == 0xa5, "disabled RAM write or BYPASS read enable was modeled incorrectly");
        while (dut.clk_pixel) step();
        pixel_stopped = true; dut.ram_test_read_address = 2; cycles(3);
        require(dut.ram_test_q == 0xa5, "RAM output added an asynchronous address path");
        pixel_stopped = false; read_edge();
        require(dut.ram_test_q == 0x5a, "independent RAM write or one-edge read latency was lost");
        dut.ram_test_clock_enable = 1;
        ram_storage_self_test_passed = true;
    }

    void write(uint32_t offset, uint32_t value) {
        dut.address = FES_DE25_SCANOUT_PERIPHERAL_OFFSET + offset;
        dut.writedata = value; dut.write = 1; dut.eval();
        unsigned limit = 100000;
        do { step(); require(--limit, "control write timed out"); } while (!accepted_control);
        dut.write = 0; dut.eval();
    }

    uint32_t read(uint32_t offset) {
        dut.address = FES_DE25_SCANOUT_PERIPHERAL_OFFSET + offset;
        dut.read = 1; dut.eval();
        uint64_t end = mem_cycles + FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES + 1024;
        do { step(); require(mem_cycles < end, "control read acceptance timed out"); } while (!accepted_control);
        dut.read = 0; dut.eval();
        while (!control_response) { step(); require(mem_cycles < end, "control read response timed out"); }
        return control_data;
    }

    Snapshot snapshot() {
        uint32_t old = read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET), serial = old;
        write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_SNAPSHOT);
        for (unsigned i = 0; i < 128 && serial == old; ++i) serial = read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET);
        require(serial == old + 1U, "snapshot serial did not advance exactly once");
        Snapshot s{}; s.serial = serial;
        s.status = read(FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET);
        uint32_t lo = read(FES_DE25_SCANOUT_SNAPSHOT_GENERATION_LO_OFFSET);
        uint32_t hi = read(FES_DE25_SCANOUT_SNAPSHOT_GENERATION_HI_OFFSET);
        s.generation = (uint64_t(hi) << 32) | lo;
        s.accepted = read(FES_DE25_SCANOUT_SNAPSHOT_ACCEPTED_SEQUENCE_OFFSET);
        s.displayed = read(FES_DE25_SCANOUT_SNAPSHOT_DISPLAYED_SEQUENCE_OFFSET);
        s.current = read(FES_DE25_SCANOUT_SNAPSHOT_CURRENT_SEQUENCE_OFFSET);
        s.slots = read(FES_DE25_SCANOUT_SNAPSHOT_SLOTS_OFFSET);
        s.errors = read(FES_DE25_SCANOUT_SNAPSHOT_ERRORS_OFFSET);
        s.underflows = read(FES_DE25_SCANOUT_SNAPSHOT_UNDERFLOWS_OFFSET);
        s.outstanding = read(FES_DE25_SCANOUT_SNAPSHOT_OUTSTANDING_OFFSET);
        lo = read(FES_DE25_SCANOUT_SNAPSHOT_GRANT_LO_OFFSET);
        hi = read(FES_DE25_SCANOUT_SNAPSHOT_GRANT_HI_OFFSET);
        s.dma = (uint64_t(hi) << 32) | lo;
        s.span = read(FES_DE25_SCANOUT_SNAPSHOT_GRANT_SPAN_OFFSET);
        require(read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET) == serial, "snapshot changed during multword read");
        return s;
    }

    void submit(unsigned sequence, unsigned slot) {
        write(FES_DE25_SCANOUT_GRANT_LO_OFFSET, base);
        write(FES_DE25_SCANOUT_GRANT_HI_OFFSET, 0);
        write(FES_DE25_SCANOUT_GRANT_SPAN_OFFSET, FES_DE25_SCANOUT_ALLOCATION_BYTES);
        write(FES_DE25_SCANOUT_GENERATION_LO_OFFSET, uint32_t(generation));
        write(FES_DE25_SCANOUT_GENERATION_HI_OFFSET, uint32_t(generation >> 32));
        write(FES_DE25_SCANOUT_SEQUENCE_OFFSET, sequence);
        write(FES_DE25_SCANOUT_SLOT_OFFSET, slot);
        write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_SUBMIT);
    }

    void wait_request() {
        uint64_t end = mem_cycles + 3000000;
        while (!dut.endpoint_outstanding) { step(); require(mem_cycles < end, "no accepted reader command"); }
    }

    void wait_axi_request() {
        uint64_t end = mem_cycles + 10000;
        while (pending.empty()) { step(); require(mem_cycles < end, "no accepted AXI read awaiting response"); }
        require(dut.endpoint_outstanding && !axi_beats, "AXI response was not held for cancellation test");
    }

    void stop_and_drain(bool expect_fault = false) {
        check_pixels = false;
        write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_STOP);
        Snapshot s{};
        for (unsigned i = 0; i < 1024; ++i) {
            s = snapshot();
            if (s.status & FES_DE25_SCANOUT_STATUS_DRAINED) break;
        }
        require((s.status & FES_DE25_SCANOUT_STATUS_DRAINED) && !s.outstanding &&
                !(s.status & (FES_DE25_SCANOUT_STATUS_RUNNING | FES_DE25_SCANOUT_STATUS_PENDING)), "STOP did not drain");
        require(s.generation == generation && s.dma == base && s.span == FES_DE25_SCANOUT_ALLOCATION_BYTES,
                "STOP destroyed allocation identity before drain proof");
        require(bool(s.status & FES_DE25_SCANOUT_STATUS_FAULT) == expect_fault, "unexpected drain fault state");
        require(pending.empty() && !holding_response && endpoint_expected.empty(),
                "drained with AXI/Avalon response still in flight");
    }

    void report(const char* name) const {
        std::cout << "{\"case\":\"" << name << "\",\"status\":\"pass\",\"axi_bursts\":" << axi_bursts
                  << ",\"axi_beats\":" << axi_beats << ",\"full_frames_slot0\":" << complete_frames[0]
                  << ",\"full_frames_slot1\":" << complete_frames[1] << ",\"ar_stall_cycles\":" << ar_stalls
                  << ",\"r_stall_cycles\":" << r_stalls << ",\"endpoint128_beats\":" << endpoint_beats
                  << ",\"endpoint_error_beats\":" << endpoint_error_beats
                  << ",\"ram_storage_self_test_passed\":" << (ram_storage_self_test_passed ? "true" : "false")
                  << "}" << std::endl;
    }
};

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        {
            Simulation s;
            s.test_ram_storage_contract();
            require(s.read(FES_DE25_SCANOUT_ID_OFFSET) == FES_DE25_SCANOUT_IDENTITY, "wrong generated control decode");
            require(s.read(FES_DE25_SCANOUT_VERSION_OFFSET) == FES_DE25_SCANOUT_VERSION, "wrong register version");
            auto v = s.snapshot();
            require(v.status == FES_DE25_SCANOUT_STATUS_DRAINED && !v.generation && !v.outstanding &&
                    !v.errors && !s.axi_bursts && !s.dut.video_de, "reset enabled traffic or display");
            s.report("reset_disabled");
        }
        {
            Simulation s; s.base = 0; s.hold_responses = true; s.submit(0x12345678U, 1);
            s.wait_request();
            auto a = s.snapshot();
            require(a.generation == s.generation && a.accepted == 0x12345678U && a.dma == s.base &&
                    a.span == FES_DE25_SCANOUT_ALLOCATION_BYTES && a.outstanding &&
                    !(a.status & FES_DE25_SCANOUT_STATUS_DRAINED), "64-bit descriptor tore through control CDC");
            s.check_pixels = false;
            s.write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_STOP);
            s.hold_responses = false; s.cycles(1000);
            require(!s.dut.endpoint_outstanding && s.dut.endpoint_drained,
                    "atomic-snapshot test did not change live state after freezing");
            require(s.read(FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET) == a.serial &&
                    s.read(FES_DE25_SCANOUT_SNAPSHOT_OUTSTANDING_OFFSET) == a.outstanding &&
                    s.read(FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET) == a.status &&
                    s.read(FES_DE25_SCANOUT_SNAPSHOT_ACCEPTED_SEQUENCE_OFFSET) == a.accepted,
                    "live state mutated a frozen snapshot");
            auto b = s.snapshot();
            require(b.serial == a.serial + 1 && !b.outstanding &&
                    (b.status & FES_DE25_SCANOUT_STATUS_DRAINED), "second snapshot lost command ordering/new state");
            s.stop_and_drain(); s.report("atomic_snapshot");
        }
        for (unsigned mode : {0U, 1U, 2U}) {
            const bool linux_iova = mode != 0;
            Simulation s;
            // Observed on the physical trace-enabled kernel: an 8 MiB grant
            // at the very top of the 32-bit IOVA space. Keep modeled HPS
            // responses; this does not establish physical SMMU translation.
            if (linux_iova) s.base = 0xff800000U;
            // A modeled 2.56 us command-to-first-response delay at 100 MHz.
            // Each accepted burst keeps its own ready time and ordered beats;
            // this tests latency hiding, not measured HPS/DDR performance.
            if (mode == 2) s.response_latency = 256;
            std::array<unsigned, 2> frames_at_submit = s.complete_frames;
            s.submit(1, 1);
            for (unsigned sequence = 1; sequence <= 3; ++sequence) {
                unsigned slot = sequence & 1;
                uint64_t end = s.mem_cycles + 5000000;
                Snapshot v{};
                do {
                    s.cycles(2000); v = s.snapshot();
                    require(!v.errors && !v.underflows, "scanout fault/underflow in complete-frame run");
                    require(s.mem_cycles < end, "full-frame completion timed out");
                    if (v.displayed == sequence)
                        require(s.complete_frames[slot] > frames_at_submit[slot],
                                "display ACK preceded a new complete non-underflow frame");
                } while (v.displayed != sequence);
                require(v.accepted == sequence && v.current == sequence &&
                        !(v.status & FES_DE25_SCANOUT_STATUS_PENDING) &&
                        ((v.slots >> FES_DE25_SCANOUT_CURRENT_SLOT_SHIFT) & 1) == slot &&
                        ((v.slots >> FES_DE25_SCANOUT_DISPLAYED_SLOT_SHIFT) & 1) == slot,
                        "completion identity/slot mismatch");
                if (sequence < 3) {
                    frames_at_submit = s.complete_frames;
                    s.submit(sequence + 1, slot ^ 1);
                    auto pending = s.snapshot();
                    require(pending.accepted == sequence + 1 && pending.displayed == sequence &&
                            (pending.status & FES_DE25_SCANOUT_STATUS_PENDING), "acceptance released current front slot");
                }
            }
            s.stop_and_drain(); s.report(mode == 2 ? "bounded_transaction_latency" :
                (linux_iova ? "linux_iova_last_8mib" : "complete_frames_and_slot_reuse"));
        }
        {
            Simulation s; s.hold_responses = true; s.submit(1, 1); s.wait_request(); s.wait_axi_request();
            s.write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_STOP);
            auto v = s.snapshot();
            require(!(v.status & FES_DE25_SCANOUT_STATUS_DRAINED) && v.outstanding,
                    "STOP discarded accepted commands under AXI stall");
            s.hold_responses = false; s.stop_and_drain(); s.report("stop_drains_accepted_reads");
        }
        {
            Simulation s; s.hold_responses = true; s.submit(1, 1); s.wait_request(); s.wait_axi_request();
            s.cycles(50); unsigned before = s.dut.endpoint_outstanding;
            require(before && !s.dut.endpoint_drained, "clock-stall test has no owned traffic");
            // Freeze low without any reset. STOP can enter the control FIFO,
            // while returning memory data must stay retained by the CDC FIFO.
            while (s.dut.clk_pixel) s.step();
            s.pixel_stopped = true; s.hold_responses = false; s.check_pixels = false;
            s.write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_STOP);
            s.cycles(1000);
            require(s.dut.endpoint_outstanding == before && !s.dut.endpoint_drained,
                    "stopped pixel clock lost outstanding ownership");
            require(s.axi_beats > 0, "memory response was not exercised during pixel stall");
            s.pixel_stopped = false; s.stop_and_drain();
            s.report("pixel_clock_stall_preserves_responses");
        }
        {
            Simulation s; s.hold_responses = true; s.submit(1, 1); s.wait_request(); s.wait_axi_request();
            s.write(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET, 0x12345678);
            require(s.read(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET) == 0x12345678, "local guard epoch not captured");
            while (s.dut.clk_pixel) s.step();
            s.pixel_stopped = true; s.check_pixels = false;
            uint64_t before = s.mem_cycles;
            require(s.read(FES_DE25_SCANOUT_ID_OFFSET) == 0, "dead pixel clock returned endpoint identity");
            require(s.mem_cycles - before >= FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES &&
                    s.mem_cycles - before <= FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES + 128,
                    "generated CPU route did not bound a stalled read");
            require(s.read(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET) == FES_DE25_SCANOUT_GUARD_FAULT_TIMEOUT,
                    "generated route lost timeout status");
            require(s.read(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET) == 15,
                    "accepted downstream lifetime or epoch was lost at timeout");
            require(s.dut.endpoint_outstanding && !s.dut.endpoint_drained,
                    "control timeout manufactured DMA drain");
            s.pixel_stopped = false; s.cycles(512);
            require(s.read(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET) == 9,
                    "late crossing response healed poison or failed to retire control request");
            s.write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_CLEAR); s.cycles(256);
            require(s.read(FES_DE25_SCANOUT_ID_OFFSET) == 0 &&
                    s.dut.endpoint_outstanding && !s.dut.endpoint_drained,
                    "poison permitted endpoint re-entry or discarded DMA ownership");
            s.report("bounded_control_during_pixel_clock_loss");
        }
        {
            for (unsigned response : {2U, 3U}) {
                Simulation s; s.injected_response = response; s.submit(1, 1); s.wait_request();
                s.cycles(500); auto v = s.snapshot();
                require((v.errors & FES_DE25_SCANOUT_ERROR_RESPONSE) && (v.status & FES_DE25_SCANOUT_STATUS_FAULT) &&
                        !v.displayed && s.error_beats && s.endpoint_error_beats,
                        "AXI RRESP lost by width/clock crossing");
                s.stop_and_drain(true);
                std::cout << "AXI RRESP " << response << ": exact endpoint response and drain passed" << std::endl;
                if (response == 3) s.report("axi_error_crosses_cdc");
            }
        }
        {
            Simulation s; s.submit(1, 1); s.wait_request(); s.dut.clock_fault = 1; s.check_pixels = false;
            s.cycles(20); s.dut.clock_fault = 0;
            s.cycles(1000); s.stop_and_drain(true);
            s.write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_CLEAR);
            auto v = s.snapshot();
            require((v.status & FES_DE25_SCANOUT_STATUS_CLOCK_FAULT) && (v.errors & FES_DE25_SCANOUT_ERROR_CLOCK) &&
                    !s.dut.video_de, "CLEAR erased sticky physical-clock-fault gate");
            s.submit(2, 1); v = s.snapshot();
            require(!(v.status & FES_DE25_SCANOUT_STATUS_RUNNING), "clock fault permitted restart");
            s.report("clock_fault_is_sticky");
        }
    } catch (const std::exception& error) {
        std::cerr << "FAIL: " << error.what() << std::endl;
        return 1;
    }
    return 0;
}
