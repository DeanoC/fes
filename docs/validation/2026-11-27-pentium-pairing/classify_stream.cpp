// Apply the same restricted-slot classifier as the machine replay to this path.
#include "pentium_pairing.hpp"

#include <cstdint>
#include <iostream>
#include <string>

int main() {
    pentium_pairing::Instruction previous;
    std::uint32_t pc, previous_pc = 0;
    std::string hex;
    bool pending = false;
    while (std::cin >> std::hex >> pc >> hex) {
        if (hex.empty() || hex.size() > 30 || hex.size() % 2) return 1;
        pentium_pairing::Instruction current{};
        current.default32 = true;
        current.length = hex.size() / 2;
        for (unsigned i = 0; i < current.length; ++i)
            current.bytes[i] = std::stoul(hex.substr(2 * i, 2), nullptr, 16);
        current.boundary_before = pc != previous_pc + previous.length;
        if (pending && pentium_pairing::can_pair(previous, current)) {
            std::cout << std::hex << previous_pc << ' ' << pc << '\n';
            pending = false;
        } else {
            previous = current;
            previous_pc = pc;
            pending = true;
        }
    }
    return !std::cin.eof();
}
