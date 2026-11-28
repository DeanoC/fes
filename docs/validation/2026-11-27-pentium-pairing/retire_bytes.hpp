// Simulation-only companion to retire_source.py.
#pragma once

#include "pentium_pairing.hpp"

namespace pentium_pairing {

// 33 fetched, 32 CS.D, 31:28 prefix count, 27:24 total encoded length,
// 23:0 first three bytes AFTER any prefix, in little-endian order.
inline bool has_fetch_tag(std::uint64_t tag) {
    return (tag >> 33) & 1;
}

// The whitelist needs no immediate or displacement VALUES. It only consumes
// their length, plus the opcode, ModR/M and SIB. Prefixed instructions must
// be rejected before exposing the prefix-stripped bytes to the classifier.
inline bool from_retire_tag(std::uint64_t tag, Instruction& out) {
    out = {};
    out.default32 = (tag >> 32) & 1;
    out.length = (tag >> 24) & 15;
    if (!has_fetch_tag(tag) || ((tag >> 28) & 15) || !out.length)
        return false;
    for (unsigned i = 0; i < 3 && i < out.length; ++i)
        out.bytes[i] = static_cast<std::uint8_t>(tag >> (8 * i));
    return true;
}

} // namespace pentium_pairing
