// SPDX-License-Identifier: GPL-2.0-or-later
// Testbench access to the Spectrum RAM. Each bank is its own memory in
// spectrum_ram.v: stored bank b holds Spectrum bank b, or banks 5, 2 and 0 on
// the 48K. Compile with the model define the RTL was built with.
#pragma once

#include "Vspectrum_sim_top___024root.h"

#include <cstdint>

#define SPECTRUM_RAM_BANK(b) spectrum_sim_top__DOT__machine__DOT__ram__DOT__bank__BRA__##b##__KET____DOT__mem

// The 16 KiB of Spectrum bank `number`, or nullptr if the model has no such bank.
inline uint8_t* spectrum_ram_bank(Vspectrum_sim_top___024root& root, unsigned number) {
#if defined(SPECTRUM_MODEL_128K) || defined(SPECTRUM_MODEL_PLUS3) || defined(SPECTRUM_MODEL_PENTAGON)
    switch (number) {
    case 0: return &root.SPECTRUM_RAM_BANK(0)[0];
    case 1: return &root.SPECTRUM_RAM_BANK(1)[0];
    case 2: return &root.SPECTRUM_RAM_BANK(2)[0];
    case 3: return &root.SPECTRUM_RAM_BANK(3)[0];
    case 4: return &root.SPECTRUM_RAM_BANK(4)[0];
    case 5: return &root.SPECTRUM_RAM_BANK(5)[0];
    case 6: return &root.SPECTRUM_RAM_BANK(6)[0];
    case 7: return &root.SPECTRUM_RAM_BANK(7)[0];
    default: return nullptr;
    }
#else
    switch (number) {
    case 5: return &root.SPECTRUM_RAM_BANK(0)[0];
    case 2: return &root.SPECTRUM_RAM_BANK(1)[0];
    case 0: return &root.SPECTRUM_RAM_BANK(2)[0];
    default: return nullptr;
    }
#endif
}
