// SPDX-License-Identifier: GPL-3.0-or-later
// Digital command model adapted from the ST memory simulation. No pad/route model.
#pragma once
#include <algorithm>
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>
static void require(bool value, const char *what, uint64_t cycle) {
    if (!value) {
        std::cerr << what << " at cycle " << cycle << '\n';
        std::exit(1);
    }
}

template<class Dut> class Sdram {
public:
    explicit Sdram(unsigned delay = 0) : mask_delay(delay) {
        require(delay <= 2, "DQM delay must be zero, one or two clocks", 0);
    }
    // 1 MiB guest address space followed by the 360 KiB floppy buffer.
    std::vector<uint16_t> words = std::vector<uint16_t>(0xad000, 0);
    uint64_t cycle = 0, reads = 0, writes = 0, refreshes = 0, mode_sets = 0;
    uint64_t last_refresh = 0, read_due = 0, max_refresh_gap = 0;
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
    bool restart_pending = false;
    struct RamWrite { unsigned address, data, lanes; };
    std::vector<RamWrite> ram_writes;

    // FPGA reset is not a reset pin on the SDRAM. A command already in the
    // output pipeline may retire before CKE falls. Start checking the new
    // initialization sequence at that observed pin boundary, retaining bank
    // recovery and chip RAM until the controller's PRECHARGE ALL.
    void cold_reset() { restart_pending = true; }
    uint16_t tick(const Dut &dut) {
        ++cycle;
        if (restart_pending && !dut.sdram_cke) {
            initialized = false;
            precharged = false;
            last_refresh = 0;
            last_refresh_any_chip = 0;
            restart_pending = false;
        }
        for (unsigned i = mask_history.size() - 1; i; --i)
            mask_history[i] = mask_history[i - 1];
        // Physical MiSTer addon: chip masks share the high row-address pins.
        mask_history[0] = (dut.sdram_a >> 11) & 3;
        const unsigned mask = mask_history[mask_delay];
        if (read_due == cycle) {
            require(mask_history[mask_delay + 2] == 0, "READ output suppressed by delayed DQM", cycle);
            // A later NOP or deselection does not cancel the pending CAS-2
            // return. Check the actual data cycle as well as
            // command admission. This is a digital overlap check, not tHZ/tLZ.
            require(!dut.dq_oe, "READ return has output data contention", cycle);
        }
        const uint16_t sample = read_due == cycle ? pending_read : 0xf13d;
        const unsigned command = (dut.sdram_nras << 2) | (dut.sdram_ncas << 1) | dut.sdram_nwe;
        // The controller sequences both chip-select phases. PC storage lives
        // on chip zero; check recovery after either refresh phase.
        if (dut.sdram_cke && command != 7) {
            if (initialized && last_refresh_any_chip) {
                const auto gap = cycle - last_refresh_any_chip;
                require(gap >= 6, "refresh recovery on either chip is too short", cycle);
                if (gap < min_refresh_gap) min_refresh_gap = gap;
            }
            if (command == 1) last_refresh_any_chip = cycle;
        }
        if (!dut.sdram_cke || dut.sdram_ncs) return sample;
        const unsigned bank = dut.sdram_ba;
        const unsigned address = ((dut.sdram_a & 0x3fc) << 15) |
                                 (row[bank] << 4) | (bank << 2) | (dut.sdram_a & 3);
        if (command != 7)
            require(cycle - last_refresh >= 6 || refreshes == 0, "SDRAM tRFC violated", cycle);
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
            require(address < words.size(), "physical memory address escaped PC storage bounds", cycle);
            require(dut.sdram_a & 0x400, "column command lacks auto-precharge", cycle);
            open[bank] = false;
            available[bank] = cycle + (command == 4 ? 2 : 3);
            if (command == 4) {
                require(dut.dq_oe, "WRITE without driven data", cycle);
                if (address < 0x40000)
                    ram_writes.push_back({address, unsigned(dut.dq_out),
                        ((dut.sdram_a & 0x1000) ? 0u : 2u) | ((dut.sdram_a & 0x800) ? 0u : 1u)});
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
            require(cycle >= 10000, "SDRAM initialization power delay too short", cycle);
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
            if (initialized) {
                const auto gap = cycle-last_refresh;
                max_refresh_gap = std::max(max_refresh_gap, gap);
                require(gap <= 850, "slot refresh exceeds 7.8 us", cycle);
            }
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
