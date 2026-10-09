// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vst_memory_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

#ifndef ST_REFRESH_WAIT_CYCLES
#define ST_REFRESH_WAIT_CYCLES 4
#endif

// Digital SDRAM command model: packed row/bank/column addresses, CAS-2,
// single-word bursts, auto-precharge, byte masks, initialization and refresh.
// Timing limits are conservative whole cycles at 52.224 MHz, from the ISSI
// IS42S16320D data sheet. It does not model analog pad setup/hold or routing.
// Independent DQM delay exercises setup margin. Write masking has no chip
// latency; read-output masking uses the DQM sampled two chip clocks earlier.
// https://www.issi.com/WW/pdf/42-45R-S_86400D-16320D-32160D.pdf
static void require(bool value, const char *what, uint64_t cycle) {
    if (!value) {
        std::cerr << what << " at cycle " << cycle << '\n';
        std::exit(1);
    }
}

class Sdram {
public:
    explicit Sdram(unsigned delay = 0) : mask_delay(delay) {
        require(delay <= 2, "DQM delay must be zero, one or two clocks", 0);
    }
    std::vector<uint16_t> words = std::vector<uint16_t>(0xa6800, 0);
    uint64_t cycle = 0, reads = 0, writes = 0, refreshes = 0, mode_sets = 0;
    uint64_t last_refresh = 0, read_due = 0;
    uint16_t pending_read = 0;
    std::array<unsigned, 4> row{};
    std::array<bool, 4> open{};
    std::array<uint64_t, 4> activated{}, available{};
    const unsigned mask_delay;
    std::array<unsigned, 5> mask_history{};
    std::array<uint64_t, 4> write_masks{};
    uint64_t masked_after_refresh = 0;
    uint64_t last_refresh_any_chip = 0, min_refresh_gap = UINT64_MAX;
    bool after_refresh = false;
    bool precharged = false, initialized = false;

    uint16_t tick(const Vst_memory_sim_top &dut) {
        ++cycle;
        for (unsigned i = mask_history.size() - 1; i; --i)
            mask_history[i] = mask_history[i - 1];
        // Physical MiSTer addon: chip masks share the high row-address pins.
        mask_history[0] = (dut.sdram_a >> 11) & 3;
        const unsigned mask = mask_history[mask_delay];
        if (read_due == cycle)
            require(mask_history[mask_delay + 2] == 0, "READ output suppressed by delayed DQM", cycle);
        const uint16_t sample = read_due == cycle ? pending_read : 0xf13d;
        const unsigned command = (dut.sdram_nras << 2) | (dut.sdram_ncas << 1) | dut.sdram_nwe;
        // The controller sequences both chip-select phases. RAM/media live
        // on chip zero; check recovery after either refresh phase.
        if (dut.sdram_cke && command != 7) {
            if (initialized && last_refresh_any_chip) {
                const auto gap = cycle - last_refresh_any_chip;
                require(gap >= 5, "refresh recovery on either chip is too short", cycle);
                if (gap < min_refresh_gap) min_refresh_gap = gap;
            }
            if (command == 1) last_refresh_any_chip = cycle;
        }
        if (!dut.sdram_cke || dut.sdram_ncs) return sample;
        const unsigned bank = dut.sdram_ba;
        const unsigned address = ((dut.sdram_a & 0x3fc) << 15) |
                                 (row[bank] << 4) | (bank << 2) | (dut.sdram_a & 3);
        if (command != 7)
            require(cycle - last_refresh >= 5 || refreshes == 0, "SDRAM tRFC violated", cycle);
        switch (command) {
        case 3: // ACTIVATE
            require(initialized, "ACTIVATE before mode initialization", cycle);
            require(!open[bank] && cycle >= available[bank], "ACTIVATE before bank recovery", cycle);
            open[bank] = true;
            row[bank] = dut.sdram_a;
            activated[bank] = cycle;
            break;
        case 4: // WRITE
        case 5: // READ
            require(open[bank], "column command without open row", cycle);
            require(cycle - activated[bank] >= 2, "SDRAM tRCD violated", cycle);
            require(address < words.size(), "physical memory address escaped RAM/media bounds", cycle);
            require(dut.sdram_a & 0x400, "column command lacks auto-precharge", cycle);
            open[bank] = false;
            available[bank] = cycle + (command == 4 ? 2 : 3);
            if (command == 4) {
                require(dut.dq_oe, "WRITE without driven data", cycle);
                if (!(mask & 1)) words[address] = (words[address] & 0xff00) | (dut.dq_out & 0xff);
                if (!(mask & 2)) words[address] = (words[address] & 0xff) | (dut.dq_out & 0xff00);
                ++write_masks[mask];
                if (after_refresh && mask) ++masked_after_refresh;
                ++writes;
            } else {
                require(!dut.dq_oe, "READ has output data contention", cycle);
                require(mask == 0, "READ unexpectedly masked", cycle);
                read_due = cycle + 2;
                pending_read = words[address];
                ++reads;
            }
            after_refresh = false;
            break;
        case 2: // PRECHARGE ALL
            require(dut.sdram_a & 0x400, "PRECHARGE is not all-bank", cycle);
            require(cycle >= 5223, "SDRAM initialization power delay too short", cycle);
            for (unsigned b = 0; b < 4; ++b) {
                require(!open[b] || cycle - activated[b] >= 2, "SDRAM tRAS violated", cycle);
                require(cycle >= available[b], "PRECHARGE before write/read recovery", cycle);
                open[b] = false;
                available[b] = cycle + 1;
            }
            precharged = true;
            break;
        case 1: // AUTO REFRESH
            require(precharged, "refresh before initial precharge", cycle);
            for (unsigned b = 0; b < 4; ++b)
                require(!open[b] && cycle >= available[b], "refresh with active/recovering bank", cycle);
            last_refresh = cycle;
            after_refresh = true;
            ++refreshes;
            break;
        case 0: // MODE REGISTER SET
            require(refreshes >= 2, "mode set before two initialization refreshes", cycle);
            require(dut.sdram_a == 0x20, "SDRAM requires burst one, CAS two", cycle);
            initialized = true;
            ++mode_sets;
            break;
        case 7: break;
        default: require(false, "unexpected SDRAM command", cycle);
        }
        return sample;
    }
};

struct Request {
    uint32_t address = 0;
    uint16_t data = 0;
    unsigned lanes = 3;
    bool write = false;
};

class Simulation {
public:
    explicit Simulation(unsigned delay = 0) : memory(delay) {}
    Vst_memory_sim_top dut;
    Sdram memory;
    std::vector<uint16_t> reference = std::vector<uint16_t>(0xa6800, 0);
    uint64_t cycles = 0, completed = 0;
    unsigned max_latency = 0;
    unsigned read_min = 160, read_max = 0, write_min = 160, write_max = 0;

    void step() {
        dut.clk = 0;
        dut.eval();
        require(dut.sdram_clk == 1, "SDRAM clock pin polarity", cycles);
        dut.dq_sample = memory.tick(dut);
        dut.eval();
        dut.clk = 1;
        dut.eval();
        require(dut.sdram_clk == 0, "SDRAM clock high-half polarity", cycles);
        ++cycles;
    }

    void idle(unsigned count) { for (unsigned i = 0; i < count; ++i) step(); }

    void request(unsigned client, const Request &r, bool active = true) {
        switch (client) {
        case 0:
            dut.cpu_req = active; dut.cpu_addr = r.address; dut.cpu_write = r.write;
            dut.cpu_wdata = r.data; dut.cpu_byte_enable = r.lanes; break;
        case 1: dut.video_req = active; dut.video_addr = r.address; break;
        case 2:
            dut.dma_req = active; dut.dma_addr = r.address; dut.dma_write = r.write;
            dut.dma_wdata = r.data; dut.dma_byte_enable = r.lanes; break;
        case 3:
            dut.media_write_req = active; dut.media_write_addr = r.address;
            dut.media_write_wdata = r.data; dut.media_write_byte_enable = r.lanes; break;
        case 4: dut.media_read_req = active; dut.media_read_addr = r.address; break;
        }
    }

    bool ready(unsigned client) const {
        switch (client) {
        case 0: return dut.cpu_ready;
        case 1: return dut.video_ready;
        case 2: return dut.dma_ready;
        case 3: return dut.media_write_ready;
        default: return dut.media_read_ready;
        }
    }

    unsigned data(unsigned client) const {
        switch (client) {
        case 0: return dut.cpu_rdata;
        case 1: return dut.video_rdata;
        case 2: return dut.dma_rdata;
        default: return dut.media_read_rdata;
        }
    }

    void check(unsigned client, const Request &r) {
        const bool valid = client != 2 ? client == 3 ? r.address < 419840 :
                           client == 4 ? r.address < 839680 : true :
                           r.address >= 8 && r.address < 0x80000;
        const unsigned address = client == 2 ? r.address / 2 :
                                 client == 3 ? 0x40000 + r.address :
                                 client == 4 ? 0x40000 + r.address / 2 : r.address;
        if (r.write || client == 3) {
            if (valid) {
                if (r.lanes & 1) reference[address] = (reference[address] & 0xff00) | (r.data & 0xff);
                if (r.lanes & 2) reference[address] = (reference[address] & 0xff) | (r.data & 0xff00);
                require(memory.words[address] == reference[address], "physical WRITE data/byte mask mismatch", cycles);
            }
        } else {
            unsigned expected = valid ? reference[address] : 0;
            if (client == 4 && valid) expected = r.address & 1 ? expected & 0xff : expected >> 8;
            if (data(client) != expected) {
                std::cerr << "client=" << client << " address=0x" << std::hex << r.address
                          << " got=0x" << data(client) << " expected=0x" << expected << std::dec << '\n';
                require(false, "read data mismatch", cycles);
            }
        }
        ++completed;
    }

    unsigned transaction(unsigned client, const Request &r, unsigned hold = 0) {
        request(client, r);
        unsigned waited = 0;
        do {
            step();
            require(++waited <= 160, "single memory request timed out", cycles);
        } while (!ready(client));
        check(client, r);
        for (unsigned i = 0; i < hold; ++i) {
            step();
            require(!ready(client), "held request completed more than once", cycles);
        }
        request(client, r, false);
        step();
        return waited;
    }

    void initialize() {
        dut.clk = 1;
        dut.cold_reset = 1;
        dut.reset = 0;
        dut.dq_sample = 0;
        for (unsigned i = 0; i < 5; ++i) request(i, {}, false);
        dut.eval();
        step();
        dut.cold_reset = 0;
        const Request boot{4, 0x1234, 3, true};
        request(0, boot);
        while (!dut.initialized) {
            step();
            require(!dut.cpu_ready, "request completed before SDRAM initialization", cycles);
            require(cycles < 14000, "SDRAM initialization timeout", cycles);
        }
        require(memory.initialized && memory.mode_sets == 1, "initialization signal without physical mode set", cycles);
        require(memory.refreshes >= 2, "initialization refreshes missing", cycles);
        while (!ready(0)) step();
        check(0, boot);
        request(0, boot, false);
        idle(10);
    }

    void directed() {
        transaction(0, {4, 0xabcd, 3, true}, 1000);
        transaction(1, {4});
        transaction(0, {4, 0x5678, 2, true});
        transaction(0, {4});
        transaction(0, {4, 0x1234, 1, true});
        transaction(1, {4});
        transaction(0, {4, 0xffff, 0, true});
        transaction(2, {8});
        transaction(2, {8, 0xdeaf, 3, true});
        transaction(0, {4});
        transaction(0, {0x3ffff, 0x5aa5, 3, true});
        transaction(2, {0x7fffe});
        transaction(3, {0, 0xa53c, 3, true}, 700);
        transaction(4, {0});
        transaction(4, {1});
        transaction(3, {0, 0x00ab, 1, true});
        transaction(4, {0});
        transaction(4, {1});
        transaction(3, {419839, 0x9876, 3, true});
        transaction(4, {839678});
        transaction(4, {839679});
        transaction(0, {0}); // RAM zero does not alias media zero.
        transaction(0, {0x3ffff});

        const uint64_t physical = memory.reads + memory.writes;
        transaction(2, {0});
        transaction(2, {6, 0xffff, 3, true});
        transaction(2, {0x80000, 0xffff, 3, true});
        transaction(3, {419840, 0xffff, 3, true});
        transaction(4, {839680});
        transaction(4, {1048575});
        require(memory.reads + memory.writes == physical, "invalid memory access reached SDRAM", cycles);

        const auto refreshed = memory.refreshes;
        idle(5000);
        require(memory.refreshes >= refreshed + 10, "idle SDRAM is not refreshed", cycles);
        transaction(0, {4});
        transaction(4, {839679});

        // Reset after a physical ACTIVATE, before its WRITE. SDRAM must drain
        // and commit the command, while the invalidated client gets no ready.
        const Request drained{0x321, 0xbeef, 3, true};
        request(0, drained);
        const auto writes_before = memory.writes;
        while (dut.sdram_ncs || dut.sdram_nras || !dut.sdram_ncas || !dut.sdram_nwe) step();
        dut.reset = 1;
        request(0, drained, false);
        for (unsigned i = 0; i < 1000; ++i) {
            step();
            require(dut.initialized && dut.sdram_cke, "warm reset restarted SDRAM", cycles);
            require(!dut.cpu_ready, "warm-reset transaction produced a stale completion", cycles);
        }
        dut.reset = 0;
        idle(10);
        require(memory.writes == writes_before + 1, "warm reset did not drain pending physical WRITE", cycles);
        reference[drained.address] = drained.data;
        transaction(0, {drained.address});
        transaction(4, {839679});

        // A short CPU Hold withdraws only the CPU request. A newly issued
        // request must not consume the old physical read's completion.
        transaction(0, {0x100, 0x1111, 3, true});
        transaction(0, {0x101, 0x2222, 3, true});
        const Request cancelled{0x100};
        const Request replacement{0x101};
        request(0, cancelled);
        while (dut.sdram_ncs || dut.sdram_nras || !dut.sdram_ncas || !dut.sdram_nwe) step();
        request(0, cancelled, false);
        step();
        request(0, replacement);
        const auto cancel_start = cycles;
        while (!dut.cpu_ready) {
            step();
            require(cycles - cancel_start < 300, "replacement CPU request stalled after Hold", cycles);
        }
        require(dut.cpu_rdata == 0x2222, "short CPU Hold delivered a stale memory completion", cycles);
        request(0, replacement, false);
        idle(10);
    }

    void contention() {
        uint32_t random = 0x52068000;
        auto next = [&]() { random ^= random << 13; random ^= random >> 17; random ^= random << 5; return random; };
        std::array<Request, 5> pending{};
        std::array<bool, 5> active{};
        std::array<unsigned, 5> age{}, delay{}, remaining{{600, 600, 600, 600, 600}};
        unsigned outstanding = 3000;
        while (outstanding) {
            for (unsigned i = 0; i < 5; ++i) {
                if (!active[i] && remaining[i]) {
                    if (delay[i]) { --delay[i]; continue; }
                    auto &r = pending[i];
                    r.lanes = next() & 3;
                    r.data = next();
                    r.write = i == 0 || i == 2 ? next() & 1 : i == 3;
                    r.address = i == 0 ? 0x100 + (next() % 0x200) :
                                i == 1 ? 0x100 + (next() % 0x200) :
                                i == 2 ? 8 + (next() % 0x20000) * 2 :
                                i == 3 ? next() % 419840 : next() % 839680;
                    request(i, r);
                    active[i] = true;
                    age[i] = 0;
                }
            }
            step();
            require(cycles - memory.last_refresh < 450, "refresh starved during contention", cycles);
            unsigned completions = 0;
            for (unsigned i = 0; i < 5; ++i) {
                if (active[i]) {
                    ++age[i];
                    require(age[i] < 180, "round-robin client starved", cycles);
                }
                if (ready(i)) {
                    require(active[i], "completion without an outstanding request", cycles);
                    ++completions;
                    check(i, pending[i]);
                    max_latency = age[i] > max_latency ? age[i] : max_latency;
                    request(i, pending[i], false);
                    active[i] = false;
                    --remaining[i];
                    --outstanding;
                    delay[i] = 1 + next() % 7;
                }
            }
            require(completions <= 1, "multiple clients completed on one SDRAM transaction", cycles);
        }
        idle(10);
        require(reference == memory.words, "RAM and media differ from request reference", cycles);
    }

    void latency() {
        // Alternating writes/reads with varying idle gaps walks refresh
        // arrival phases. Every read checks the physical data just written;
        // all lane masks, high rows and chip-zero bank bits are exercised.
        for (unsigned i = 0; i < 4096; ++i) {
            idle(i % 17);
            const unsigned address = 0x10000 + (i % 0x1000);
            unsigned wait = transaction(0, {address, uint16_t(i ^ 0xa55a), i % 4, true});
            write_min = wait < write_min ? wait : write_min;
            write_max = wait > write_max ? wait : write_max;
            wait = transaction(0, {address});
            read_min = wait < read_min ? wait : read_min;
            read_max = wait > read_max ? wait : read_max;
        }
        require(read_min == 13 && write_min == 10, "ordinary access latency changed", cycles);
        require(memory.min_refresh_gap == ST_REFRESH_WAIT_CYCLES + 2,
                "runtime refresh recovery does not match the selected profile", cycles);
        require(read_max <= 18 + 2 * ST_REFRESH_WAIT_CYCLES &&
                write_max <= 15 + 2 * ST_REFRESH_WAIT_CYCLES,
                "refresh-induced latency exceeds its bound", cycles);
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc <= 2, "usage: memory_tb [DQM_DELAY=0|1|2]", 0);
    unsigned first = 0, last = 2;
    if (argc == 2) {
        require(argv[1][0] >= '0' && argv[1][0] <= '2' && argv[1][1] == '\0',
                "DQM delay must be zero, one or two clocks", 0);
        first = last = unsigned(argv[1][0] - '0');
    }
    for (unsigned delay = first; delay <= last; ++delay) {
        Simulation sim(delay);
        sim.initialize();
        sim.directed();
        sim.contention();
        sim.latency();
        for (const auto count : sim.memory.write_masks)
            require(count != 0, "missing DQM lane-mask coverage", sim.cycles);
        require(sim.memory.masked_after_refresh != 0, "missing masked write after refresh", sim.cycles);
        std::cout << "st_memory: DQM delay " << delay << ": " << sim.completed << " requests passed, "
                  << sim.memory.refreshes << " refreshes, " << sim.memory.masked_after_refresh
                  << " masked writes after refresh, max latency " << sim.max_latency
                  << " clocks over " << sim.cycles << " cycles\n";
        std::cout << "refresh wait " << ST_REFRESH_WAIT_CYCLES
                  << ": CPU read " << sim.read_min << ".." << sim.read_max
                  << ", write " << sim.write_min << ".." << sim.write_max
                  << ", minimum refresh command gap " << sim.memory.min_refresh_gap << " clocks\n";
    }
}
