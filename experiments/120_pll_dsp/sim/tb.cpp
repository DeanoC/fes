#include "verilated.h"

#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

constexpr uint32_t kSignature = 0xdc100000U;
constexpr uint32_t kLock = 1U << 13;

uint32_t command(bool high_select, uint8_t right, uint8_t left) {
    return (static_cast<uint32_t>(high_select) << 16) | (static_cast<uint32_t>(right) << 8) | left;
}

uint32_t observed(uint8_t selected) {
    return kSignature | kLock | selected;
}

uint32_t data_word(uint32_t value) {
    return value & 0xffff20ffU;
}

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

void settle(Vtop &top) {
    for (int i = 0; i < 32; ++i) {
        tick(top);
    }
}

uint32_t &gpo(Vtop &top) {
    return top.top->hps_gp->gpo;
}

uint32_t gpi(const Vtop &top) {
    return top.top->hps_gp->gpi;
}

bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected) {
        return true;
    }
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}

bool expect_product(Vtop &top, uint8_t left, uint8_t right, uint16_t wide, const char *label) {
    gpo(top) = command(false, right, left);
    settle(top);
    if (!expect_word(data_word(gpi(top)), observed(static_cast<uint8_t>(wide)), label)) {
        return false;
    }
    gpo(top) = command(true, right, left);
    settle(top);
    return expect_word(
        data_word(gpi(top)),
        observed(static_cast<uint8_t>(wide >> 8)),
        label
    );
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();
    gpo(top) = 0;
    settle(top);

    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) {
        return EXIT_FAILURE;
    }
    if ((gpi(top) & kLock) == 0) {
        std::cerr << "mismatch lock expected set observed=0x" << std::hex << gpi(top) << std::dec << '\n';
        return EXIT_FAILURE;
    }

    const struct {
        uint8_t left;
        uint8_t right;
        uint16_t wide;
        const char *label;
    } cases[] = {
        {0x00, 0x00, 0x0000, "zero"},
        {0x01, 0x01, 0x0001, "one"},
        {0x0a, 0x0c, 0x0078, "ten times twelve"},
        {0x12, 0x34, 0x03a8, "0x12 times 0x34"},
        {0xff, 0x02, 0x01fe, "0xff times two"},
        {0xff, 0xff, 0xfe01, "0xff times 0xff"},
    };
    for (const auto &item : cases) {
        if (!expect_product(top, item.left, item.right, item.wide, item.label)) {
            return EXIT_FAILURE;
        }
    }

    gpo(top) = command(false, 0x0c, 0x0a);
    settle(top);
    if (!expect_word(data_word(gpi(top)), observed(0x78), "repeat low byte")) {
        return EXIT_FAILURE;
    }

    std::cout << "PASS: HPS peek/poke of PLL-clocked eight-by-eight hard product verified\n";
    return EXIT_SUCCESS;
}
