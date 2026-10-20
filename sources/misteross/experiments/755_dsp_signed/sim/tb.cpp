#include "verilated.h"

#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

constexpr uint32_t kSignature = 0xd61a0000U;

int64_t sext(uint64_t value, int bits) {
    const uint64_t sign = uint64_t{1} << (bits - 1);
    return static_cast<int64_t>((value ^ sign) - sign);
}

// The 32-bit view the RTL exposes for one mode.
uint32_t expected_view(unsigned mode, uint32_t left, uint32_t right) {
    switch (mode) {
    case 0:
        return static_cast<uint32_t>(sext(left, 9) * sext(right, 9)) & 0x3ffffU;
    case 1:
        return (left * right) & 0x3ffffU;
    case 2: {
        const int64_t a = sext((left << 9) | right, 18);
        const int64_t b = sext((right << 9) | left, 18);
        return static_cast<uint32_t>(a * b);
    }
    default: {
        const int64_t a = sext((left << 18) | (right << 9) | left, 27);
        const int64_t b = sext((right << 18) | (left << 9) | right, 27);
        return static_cast<uint32_t>(a * b);
    }
    }
}

uint32_t command(unsigned mode, bool high_select, uint32_t right, uint32_t left) {
    return (mode << 19) | (static_cast<uint32_t>(high_select) << 18) | (right << 9) | left;
}

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

// Products are registered, then the selected half: two clocks per write.
void settle(Vtop &top) {
    tick(top);
    tick(top);
}

uint32_t &gpo(Vtop &top) { return top.top->hps_gp->gpo; }
uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }

bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected) return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();
    if (!expect_word(gpi(top), kSignature, "signature and reset value")) return EXIT_FAILURE;

    const struct { uint32_t left, right; const char *label; } operands[] = {
        {0x00a, 0x00c, "ten times twelve"},
        {0x1ff, 0x1ff, "minus one squared / 511 squared"},
        {0x100, 0x1ff, "minus 256 times minus one"},
        {0x155, 0x0aa, "0x155 times 0x0aa"},
        {0x1ff, 0x001, "minus one times one"},
        {0x080, 0x180, "128 times minus 128"},
    };
    const char *modes[] = {"signed 9x9", "unsigned 9x9", "signed 18x18", "signed 27x27"};
    for (const auto &item : operands) {
        for (unsigned mode = 0; mode < 4; ++mode) {
            const uint32_t view = expected_view(mode, item.left, item.right);
            gpo(top) = command(mode, false, item.right, item.left);
            settle(top);
            if (!expect_word(gpi(top), kSignature | (view & 0xffffU), modes[mode])) {
                std::cerr << "  operands: " << item.label << " low half\n";
                return EXIT_FAILURE;
            }
            gpo(top) = command(mode, true, item.right, item.left);
            settle(top);
            if (!expect_word(gpi(top), kSignature | (view >> 16), modes[mode])) {
                std::cerr << "  operands: " << item.label << " high half\n";
                return EXIT_FAILURE;
            }
        }
    }
    std::cout << "PASS: HPS peek/poke of signed and full-width unsigned hard products verified\n";
    return EXIT_SUCCESS;
}
