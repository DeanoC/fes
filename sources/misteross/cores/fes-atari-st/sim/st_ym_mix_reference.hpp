// SPDX-License-Identifier: GPL-2.0-or-later
// Independent test oracle: Hatari v2.5.0 sound.c interpolate_volumetable.
// Measured input: Paulo Simoes, Copyright 2012, preserved in ../data.
// This computes the complete ordered table, without ROM compression or ranking.
#ifndef ST_YM_MIX_REFERENCE_HPP
#define ST_YM_MIX_REFERENCE_HPP

#include <array>
#include <cmath>
#include <cstdint>

namespace st_ym_mix_reference {
inline const std::array<uint16_t, 32768>& normalized_table()
{
    static const auto result = [] {
        static const uint16_t measured[16][16][16] =
#include "../data/ym2149_fixed_vol.h"
        uint16_t voltage[32][32][32] = {};
        const auto geometric_mean = [](uint16_t left, uint16_t right) {
            return static_cast<uint16_t>(0.5 + std::sqrt(double(left) * double(right)));
        };
        // Preserve primary axis order and every intermediate rounding operation.
        for (int c = 1; c < 32; c += 2) {
            for (int b = 1; b < 32; b += 2) {
                for (int a = 1; a < 32; a += 2)
                    voltage[c][b][a] = measured[(c-1)/2][(b-1)/2][(a-1)/2];
                voltage[c][b][0] = voltage[c][b][1];
                voltage[c][b][1] = voltage[c][b][3];
                voltage[c][b][3] = geometric_mean(voltage[c][b][1], voltage[c][b][5]);
                for (int a = 2; a < 32; a += 2)
                    voltage[c][b][a] = geometric_mean(voltage[c][b][a-1], voltage[c][b][a+1]);
            }
            for (int a = 0; a < 32; ++a) {
                voltage[c][0][a] = voltage[c][1][a];
                voltage[c][1][a] = voltage[c][3][a];
                voltage[c][3][a] = geometric_mean(voltage[c][1][a], voltage[c][5][a]);
            }
            for (int b = 2; b < 32; b += 2)
                for (int a = 0; a < 32; ++a)
                    voltage[c][b][a] = geometric_mean(voltage[c][b-1][a], voltage[c][b+1][a]);
        }
        for (int b = 0; b < 32; ++b) {
            for (int a = 0; a < 32; ++a) {
                voltage[0][b][a] = voltage[1][b][a];
                voltage[1][b][a] = voltage[3][b][a];
                voltage[3][b][a] = geometric_mean(voltage[1][b][a], voltage[5][b][a]);
            }
        }
        for (int c = 2; c < 32; c += 2)
            for (int b = 0; b < 32; ++b)
                for (int a = 0; a < 32; ++a)
                    voltage[c][b][a] = geometric_mean(voltage[c-1][b][a], voltage[c+1][b][a]);
        std::array<uint16_t, 32768> normalized{};
        for (unsigned c = 0; c < 32; ++c)
            for (unsigned b = 0; b < 32; ++b)
                for (unsigned a = 0; a < 32; ++a)
                    normalized[(c << 10) | (b << 5) | a] =
                        uint32_t(voltage[c][b][a]) * 32767u / 65119u;
        return normalized;
    }();
    return result;
}

inline uint16_t sample(uint16_t packed_levels)
{
    return normalized_table()[packed_levels & 0x7fff];
}
} // namespace st_ym_mix_reference
#endif
