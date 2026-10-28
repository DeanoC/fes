// Host transport simulation: actual generated Avalon128/AXI256 path, modeled AXI memory.
#include "Vfes_de25_interconnect_test.h"
#include "verilated.h"
#include "generated/fes_de25_ddr_probe.hpp"
#include <cstdint>
#include <deque>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

namespace g = mister::native::generated;

static void require(bool ok, const std::string& message) {
    if (!ok) throw std::runtime_error(message);
}

static uint32_t memory_word(uint32_t address) {
    // Different lower/upper lanes and addresses; independent of the DUT's checksum.
    uint32_t value = address ^ 0x9e3779b9u;
    value ^= value >> 16;
    value *= 0x85ebca6bu;
    return value ^ (value >> 13);
}

struct Burst { uint32_t address; unsigned beats, size, id, position = 0; };

struct Simulation {
    Vfes_de25_interconnect_test dut;
    std::deque<Burst> pending;
    uint64_t cycle = 0;
    uint32_t first = 0;
    uint32_t grant_base = 0;
    uint64_t grant_end = 0;
    unsigned expected_beats = 0, returned = 0, axi_beats = 0, axi_bursts = 0, av_bursts = 0;
    unsigned injected_response = 0, observed_errors = 0;
    unsigned ar_stalls = 0, r_stalls = 0;
    std::vector<Burst> transactions;
    bool holding_response = false;
    bool stalled_ar = false;
    uint32_t held_araddr = 0;
    unsigned held_arlen = 0, held_arsize = 0, held_arid = 0;

    Simulation() {
        dut.clk = 0; dut.reset_n = 0;
        dut.address = 0; dut.read = 0; dut.write = 0;
        dut.byteenable = 15; dut.writedata = 0;
        dut.arready = 0; dut.rvalid = 0; dut.rlast = 0; dut.rresp = 0; dut.rid = 0;
        for (int lane = 0; lane < 8; ++lane) dut.rdata[lane] = 0;
        for (int i = 0; i < 8; ++i) tick();
        dut.reset_n = 1;
        for (int i = 0; i < 20; ++i) tick();
    }

    void tick() {
        dut.arready = cycle % 4 != 1;
        if (!holding_response && !pending.empty() && cycle % 5 != 0) {
            const auto& burst = pending.front();
            uint32_t beat_address = burst.position == 0 ? burst.address :
                (burst.address & ~((1u << burst.size) - 1)) + (burst.position << burst.size);
            for (unsigned lane = 0; lane < 8; ++lane)
                dut.rdata[lane] = memory_word((beat_address & ~31u) + lane * 4);
            dut.rid = burst.id;
            dut.rlast = burst.position + 1 == burst.beats;
            dut.rresp = axi_beats == 0 ? injected_response : 0;
            holding_response = true;
        }
        dut.rvalid = holding_response;
        dut.clk = 0; dut.eval();
        bool ar_accepted = dut.arvalid && dut.arready;
        bool r_accepted = dut.rvalid && dut.rready;
        Burst new_burst{dut.araddr, unsigned(dut.arlen) + 1, dut.arsize, dut.arid};
        if (dut.reset_n) {
            require(!dut.awvalid && !dut.wvalid, "read-only path issued an AXI write");
            if (stalled_ar) {
                require(dut.arvalid && dut.araddr == held_araddr && dut.arlen == held_arlen &&
                        dut.arsize == held_arsize && dut.arid == held_arid,
                        "AXI request changed under AR backpressure");
            }
            stalled_ar = dut.arvalid && !dut.arready;
            if (stalled_ar) ++ar_stalls;
            if (dut.rvalid && !dut.rready) ++r_stalls;
            held_araddr = dut.araddr; held_arlen = dut.arlen;
            held_arsize = dut.arsize; held_arid = dut.arid;
            if (ar_accepted) {
                require(new_burst.size <= 5 && dut.arburst == 1, "unsupported AXI transfer shape");
                uint64_t end = (uint64_t(new_burst.address) & ~((1ull << new_burst.size) - 1)) +
                               (uint64_t(new_burst.beats) << new_burst.size);
                require(end <= (uint64_t(new_burst.address) & ~4095ull) + 4096,
                        "AXI burst crosses a 4 KiB boundary");
                require(new_burst.address >= grant_base && end <= grant_end,
                        "AXI read footprint exceeds the granted allocation: address=" +
                        std::to_string(new_burst.address) + " end=" + std::to_string(end) +
                        " grant_end=" + std::to_string(grant_end));
                ++axi_bursts;
                transactions.push_back(new_burst);
            }
            if (dut.probe_read && !dut.probe_waitrequest) ++av_bursts;
            if (dut.probe_readdatavalid) {
                require(returned < expected_beats, "extra Avalon response beat");
                uint32_t address = first + returned * 16;
                for (unsigned lane = 0; lane < 4; ++lane)
                    require(dut.probe_readdata[lane] == memory_word(address + lane * 4),
                            "wrong 128-bit lane/data at returned beat " + std::to_string(returned));
                if (dut.probe_response) ++observed_errors;
                ++returned;
            }
        }
        dut.clk = 1; dut.eval();
        if (r_accepted) {
            require(!pending.empty(), "AXI response without an accepted request");
            if (++pending.front().position == pending.front().beats) pending.pop_front();
            ++axi_beats;
            holding_response = false;
        }
        if (ar_accepted) pending.push_back(new_burst);
        dut.clk = 0; dut.eval();
        ++cycle;
    }

    void write(uint32_t offset, uint32_t value) {
        dut.address = offset; dut.writedata = value; dut.write = 1;
        tick(); dut.write = 0;
    }

    uint32_t read(uint32_t offset) {
        dut.address = offset; dut.read = 1;
        tick(); dut.read = 0;
        require(dut.readdatavalid, "missing probe control read response");
        return dut.readdata;
    }

    void run(const char* name, uint32_t base, uint32_t offset, unsigned beats, unsigned response = 0) {
        first = base + offset; expected_beats = beats; injected_response = response;
        grant_base = base; grant_end = uint64_t(base) + offset + beats * 16;
        write(g::FesDe25DdrProbeGrantLoOffset, base);
        write(g::FesDe25DdrProbeGrantHiOffset, 0);
        write(g::FesDe25DdrProbeGrantSpanOffset, offset + beats * 16);
        write(g::FesDe25DdrProbeReadOffsetOffset, offset);
        write(g::FesDe25DdrProbeReadBytesOffset, beats * 16);
        write(g::FesDe25DdrProbeGenerationLoOffset, 17);
        write(g::FesDe25DdrProbeGenerationHiOffset, 2);
        write(g::FesDe25DdrProbeSequenceOffset, 3);
        write(g::FesDe25DdrProbeCommandOffset, g::FesDe25DdrProbeCommandStart);
        uint32_t status = 0;
        for (unsigned i = 0; i < 20000; ++i) {
            status = read(g::FesDe25DdrProbeStatusOffset);
            if (status & g::FesDe25DdrProbeStatusIdle) break;
        }
        require(status & g::FesDe25DdrProbeStatusIdle, "transport did not drain");
        require(returned == beats && pending.empty() && !holding_response,
                "lost/undrained AXI or Avalon response");
        require(read(g::FesDe25DdrProbeCompletedBytesOffset) == beats * 16, "wrong completed byte count");
        require(read(g::FesDe25DdrProbeAcceptedBytesOffset) == beats * 16, "wrong accepted byte count");
        uint32_t expected_checksum = 0;
        for (unsigned word = 0; word < beats * 4; ++word) expected_checksum ^= memory_word(first + word * 4);
        require(read(g::FesDe25DdrProbeChecksumOffset) == expected_checksum, "wrong end-to-end checksum");
        for (unsigned lane = 0; lane < 4; ++lane)
            require(read(g::FesDe25DdrProbeLastData0Offset + lane * 4) ==
                    memory_word(first + (beats - 1) * 16 + lane * 4), "wrong last data lanes");
        if (response) {
            require(observed_errors != 0 && (status & g::FesDe25DdrProbeStatusFault) &&
                    !(status & g::FesDe25DdrProbeStatusDone) &&
                    read(g::FesDe25DdrProbeErrorOffset) == g::FesDe25DdrProbeErrorResponse,
                    "AXI error did not reach the real probe's sticky fault");
        } else {
            require(observed_errors == 0 && status == (g::FesDe25DdrProbeStatusIdle | g::FesDe25DdrProbeStatusDone) &&
                    read(g::FesDe25DdrProbeErrorOffset) == 0, "successful transport faulted");
        }
        require(axi_bursts > 0 && axi_beats > 0 && av_bursts > 0, "no actual transport traffic observed");
        std::cout << "{\"case\":\"" << name << "\",\"status\":\"pass\",\"avalon128_beats\":" << returned
                  << ",\"avalon_bursts\":" << av_bursts << ",\"axi256_beats\":" << axi_beats
                  << ",\"axi_bursts\":" << axi_bursts << ",\"injected_axi_response\":" << response
                  << ",\"avalon_error_beats\":" << observed_errors
                  << ",\"ar_stall_cycles\":" << ar_stalls << ",\"r_stall_cycles\":" << r_stalls
                  << ",\"axi_transactions\":[";
        for (unsigned i = 0; i < transactions.size(); ++i) {
            if (i) std::cout << ',';
            const auto& burst = transactions[i];
            std::cout << "{\"address\":" << burst.address << ",\"beats\":" << burst.beats
                      << ",\"bytes_per_beat\":" << (1u << burst.size) << '}';
        }
        std::cout << "]}\n";
    }
};

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        { Simulation sim; sim.run("aligned-single", 0x80000000, 0, 1); }
        { Simulation sim; sim.run("upper-half-single", 0x80000000, 16, 1); }
        { Simulation sim; sim.run("upper-half-odd-three", 0x80000100, 16, 3); }
        { Simulation sim; sim.run("aligned-odd-five", 0x80000200, 0, 5); }
        { Simulation sim; sim.run("upper-half-sixteen", 0x80000300, 16, 16); }
        { Simulation sim; sim.run("upper-half-seventeen", 0x80000400, 16, 17); }
        { Simulation sim; sim.run("4k-upper-half-tail", 0x80000fe0, 16, 3); }
        { Simulation sim; sim.run("4k-head-tail-nineteen", 0x80000fc0, 16, 19); }
        { Simulation sim; sim.run("slverr-upper-half", 0x80000200, 16, 3, 2); }
        { Simulation sim; sim.run("decerr-aligned", 0x80000200, 0, 3, 3); }
    } catch (const std::exception& error) {
        std::cerr << "FAIL: " << error.what() << '\n';
        return 1;
    }
    return 0;
}
