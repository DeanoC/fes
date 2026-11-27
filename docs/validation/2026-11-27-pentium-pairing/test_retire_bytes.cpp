// Check that dropping displacement / immediate values preserves the complete
// classifier result. Exercise every opcode and ModR/M, both CS widths, every
// encoded length and representative SIB forms (including no base and no index).
#include "retire_bytes.hpp"

#include <cassert>

int main() {
    using namespace pentium_pairing;
    Instruction full{}, compact{};
    for (unsigned width : {0u, 1u}) {
        full.default32 = width;
        for (unsigned op = 0; op < 256; ++op) {
            full.bytes[0] = op;
            for (unsigned rm = 0; rm < 256; ++rm) {
                full.bytes[1] = rm;
                for (unsigned sib : {0x00u, 0x25u, 0xfbu}) {
                    full.bytes[2] = sib;
                    for (unsigned i = 3; i < full.bytes.size(); ++i)
                        full.bytes[i] = op + rm * i + sib;
                    for (unsigned len = 1; len <= 15; ++len) {
                        full.length = len;
                        const auto tag = (1ull << 33) | (std::uint64_t(width) << 32) |
                                         (len << 24) | (sib << 16) | (rm << 8) | op;
                        assert(has_fetch_tag(tag) && from_retire_tag(tag, compact));
                        assert(compact.default32 == full.default32 && compact.length == len);
                        const auto a = classify(full), b = classify(compact);
                        assert(a.reads == b.reads && a.writes == b.writes && a.u == b.u && a.v == b.v);
                    }
                }
            }
        }
    }
    assert(!from_retire_tag(0, compact));                                 // synthetic injection
    assert(!from_retire_tag(1ull << 33, compact));                        // invalid length
    for (unsigned prefixes = 1; prefixes < 16; ++prefixes) {
        const auto tag = (1ull << 33) | (std::uint64_t(prefixes) << 28) | (6u << 24) | 0xb8;
        assert(has_fetch_tag(tag) && !from_retire_tag(tag, compact));     // stripped prefixes
    }
}
