// SPDX-License-Identifier: MIT
// Nuked-OPL3 in OPL2 mode, stepped beside the pc386 OPL2 (docs/pc386-machine-design.md,
// "OPL2", verification). The wrapper hands the import a register write only
// between samples, so the reference applies every write the RTL released
// before a sample tick, generates that sample, and expects the RTL's next
// sample to equal it. Call clock() after each RTL clock edge with the
// wrapper's observation points (sim/pc386_audio.vlt makes them public).
#pragma once

#include "opl3.h"

#include <cstdint>
#include <cstdio>
#include <deque>
#include <string>

class Opl2Reference {
public:
    Opl2Reference() { OPL3_Reset(&chip, 49716); }

    // write: the opl2_reg_wr_t bits {valid, address, data}; tick: the
    // wrapper's sample enable; valid/sample: its output.
    void clock(uint32_t write, bool tick, bool valid, int16_t sample) {
        if (write >> 16 & 1) {
            OPL3_WriteReg(&chip, uint16_t(write >> 8 & 0xff), uint8_t(write));
            ++writes;
        }
        ++since_tick;
        if (tick) {
            since_tick = 0;
            int16_t out[2];
            OPL3_Generate(&chip, out);
            expected.push_back(out[0]);
        }
        if (valid) {
            if (since_tick > busy_clocks) busy_clocks = since_tick;
            if (expected.empty()) {
                note("RTL sample with no reference sample");
                return;
            }
            int16_t want = expected.front();
            expected.pop_front();
            if (sample == want) ++exact;
            else {
                if (!mismatches) {
                    char text[96];
                    std::snprintf(text, sizeof text, "sample %llu: RTL %d, Nuked-OPL3 %d",
                                  (unsigned long long)samples, sample, want);
                    first_mismatch = text;
                }
                ++mismatches;
            }
            if (sample) ++nonzero;
            ++samples;
        }
    }

    uint64_t samples = 0, exact = 0, mismatches = 0, nonzero = 0, writes = 0;
    uint64_t busy_clocks = 0;      // longest sample computation, tick to output
    std::string first_mismatch;

private:
    void note(const char *text) {
        if (!mismatches) first_mismatch = text;
        ++mismatches;
    }
    opl3_chip chip;
    std::deque<int16_t> expected;
    uint64_t since_tick = 0;
};
