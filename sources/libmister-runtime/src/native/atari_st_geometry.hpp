// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/generated/fes_computer.hpp"
#include <cstddef>
#include <cstdint>
namespace mister { namespace native {
struct AtariStGeometry { unsigned tracks = 0, heads = 0, sectors = 0; };
inline bool InferAtariStGeometry(std::uint32_t size, AtariStGeometry* output = nullptr)
{
    using namespace generated;
    AtariStGeometry found;
    unsigned matches = 0;
    for (unsigned t = FesComputerAtariStFloppyGeometryMinTracks; t <= FesComputerAtariStFloppyGeometryMaxTracks; ++t)
        for (unsigned h = FesComputerAtariStFloppyGeometryMinHeads; h <= FesComputerAtariStFloppyGeometryMaxHeads; ++h)
            for (unsigned s = FesComputerAtariStFloppyGeometryMinSectors; s <= FesComputerAtariStFloppyGeometryMaxSectors; ++s)
                if (t * h * s * FesComputerAtariStFloppySectorBytes == size) {
                    found = {t, h, s}; ++matches;
                }
    if (matches != 1) return false;
    if (output) *output = found;
    return true;
}
inline bool AtariStSizeAdmitted(std::uint32_t size, bool geometry)
{
    return size == generated::FesComputerAtariStFloppyBytes || (geometry && InferAtariStGeometry(size));
}
// Immutable nonlegacy images declare the same geometry in their boot BPB.
// Saved guest bytes are validated by length instead: a guest can rewrite its BPB.
inline bool AtariStBaseBpbValid(const unsigned char* bytes, std::size_t length, std::uint32_t size)
{
    if (size == generated::FesComputerAtariStFloppyBytes) return true;
    AtariStGeometry geometry;
    if (!bytes || length < 28 || !InferAtariStGeometry(size, &geometry)) return false;
    const auto word = [bytes](unsigned at) { return unsigned(bytes[at]) | (unsigned(bytes[at + 1]) << 8); };
    return word(11) == generated::FesComputerAtariStFloppySectorBytes &&
        word(19) == size / generated::FesComputerAtariStFloppySectorBytes &&
        word(24) == geometry.sectors && word(26) == geometry.heads;
}
} }
