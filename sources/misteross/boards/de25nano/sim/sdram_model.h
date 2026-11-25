// SPDX-License-Identifier: GPL-2.0-or-later
#pragma once

#include <array>
#include <cstdint>
#include <map>

struct SdramModel {
    // Logical CL2/BL1 SDRAM: the
    // chips sample on the fabric falling edge, and read data is valid from
    // the edge after the command until the one after that.
    std::array<std::array<int, 4>, 2> rows;
    std::map<uint32_t, uint16_t> memory;
    unsigned edge = 0, due = 0, expires = 0;
    uint16_t pending = 0;
    bool have_pending = false, dq_valid = false;

    SdramModel() {
        for (auto& chip : rows) chip.fill(-1);
    }

    template<class Pins, class Check>
    void falling(Pins& dut, Check require) {
        ++edge;
        if (dq_valid && edge == expires) {
            dq_valid = false;
            dut.dram_dq_i = 0xdead;
            dut.eval();
        }
        if (have_pending && edge == due) {
            dut.dram_dq_i = pending;
            have_pending = false;
            dq_valid = true;
            expires = edge + 1;
            dut.eval();
        }
        const unsigned command = (dut.dram_ras_n << 2) | (dut.dram_cas_n << 1) | dut.dram_we_n;
        for (unsigned chip = 0; chip < 2; ++chip) {
            if ((dut.dram_cs_n & (1u << chip)) || !dut.dram_cke) continue;
            auto& row = rows[chip][dut.dram_ba];
            if (command == 3) {
                require(row < 0, "SDRAM ACT on an open bank");
                row = dut.dram_addr;
            } else if (command == 2) {
                if (dut.dram_addr & 0x400) rows[chip].fill(-1);
                else row = -1;
            } else if (command == 1) {
                for (int open : rows[chip]) require(open < 0, "SDRAM refresh on an open bank");
            } else if (command == 4 || command == 5) {
                require(row >= 0, "SDRAM data command without an active bank");
                const uint32_t address = (chip << 25) | (unsigned(dut.dram_ba) << 23) |
                                         (unsigned(row) << 10) | (dut.dram_addr & 1023);
                if (command == 4) {
                    require(dut.dram_dq_oe, "SDRAM WRITE with floating DQ");
                    uint16_t& value = memory[address];
                    for (unsigned lane = 0; lane < 2; ++lane)
                        if (!(dut.dram_dqm & (1u << lane))) {
                            const unsigned mask = 255u << (8 * lane);
                            value = (value & ~mask) | (dut.dram_dq_o & mask);
                        }
                } else {
                    require(!dut.dram_dq_oe && !have_pending, "overlapping SDRAM READ");
                    pending = memory[address];
                    due = edge + 1;
                    have_pending = true;
                }
            } else {
                require(command == 0 || command == 7, "unsupported SDRAM command");
            }
        }
    }
    uint8_t byte(uint32_t address) const {
        // The model's key is the controller's halfword address.
        return (memory.at(address >> 1) >> (8 * (address & 1))) & 0xff;
    }
};
