#include "verilated.h"

#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

constexpr uint32_t kSignature = 0xd9100000U;

uint32_t command(bool high_select, uint8_t addr, uint8_t left) {
    return (static_cast<uint32_t>(high_select) << 16) | (static_cast<uint32_t>(addr) << 8) | left;
}

uint32_t observed(uint8_t selected) {
    return kSignature | selected;
}

uint32_t data_word(uint32_t value) {
    return (value & 0xffff00ffU);
}

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
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

bool expect_product(Vtop &top, uint8_t left, uint8_t addr, uint16_t wide, const char *label) {
    gpo(top) = command(false, addr, left);
    tick(top);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(static_cast<uint8_t>(wide)), label)) {
        return false;
    }
    gpo(top) = command(true, addr, left);
    tick(top);
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

    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) {
        return EXIT_FAILURE;
    }

    const struct {
        uint8_t left;
        uint8_t addr;
        uint16_t wide;
        const char *label;
    } cases[] = {
        {0x00, 0x00, 0x0000, "zero times table[0]"},
        {0x0c, 0x00, 0x07bc, "0x0c times table[0]"},
        {0x02, 0x01, 0x0148, "0x02 times table[1]"},
        {0x01, 0x0a, 0x00af, "0x01 times table[0x0a]"},
        {0xff, 0xff, 0x59a6, "0xff times table[0xff]"},
    };
    for (const auto &item : cases) {
        if (!expect_product(top, item.left, item.addr, item.wide, item.label)) {
            return EXIT_FAILURE;
        }
    }

    gpo(top) = command(false, 0x00, 0x0c);
    tick(top);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0xbc), "repeat low byte")) {
        return EXIT_FAILURE;
    }

    std::cout << "PASS: HPS peek/poke of DSP product from initialized table verified\n";
    return EXIT_SUCCESS;
}
