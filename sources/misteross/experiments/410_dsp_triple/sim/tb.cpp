#include "verilated.h"

#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

constexpr uint32_t kSignature = 0xd6110000U;

uint32_t command(uint8_t lane, bool high_select, uint8_t right, uint8_t left) {
    return (static_cast<uint32_t>(lane) << 17) | (static_cast<uint32_t>(high_select) << 16) |
           (static_cast<uint32_t>(right) << 8) | left;
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

bool expect_product(
    Vtop &top,
    uint8_t lane,
    uint8_t left,
    uint8_t right,
    uint16_t wide,
    const char *label
) {
    gpo(top) = command(lane, false, right, left);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(static_cast<uint8_t>(wide)), label)) {
        return false;
    }
    gpo(top) = command(lane, true, right, left);
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
        uint8_t lane;
        uint8_t left;
        uint8_t right;
        uint16_t wide;
        const char *label;
    } cases[] = {
        {0, 0x00, 0x00, 0x0000, "zero ab"},
        {1, 0x00, 0x00, 0x0000, "zero anotb"},
        {2, 0x00, 0x00, 0x0000, "zero xor"},
        {0, 0x01, 0x01, 0x0001, "one ab"},
        {1, 0x01, 0x01, 0x00fe, "one anotb"},
        {2, 0x01, 0x01, 0x0000, "one xor"},
        {0, 0x0a, 0x0c, 0x0078, "ten times twelve ab"},
        {1, 0x0a, 0x0c, 0x097e, "ten times twelve anotb"},
        {2, 0x0a, 0x0c, 0x0082, "ten times twelve xor"},
        {0, 0x12, 0x34, 0x03a8, "0x12 times 0x34 ab"},
        {1, 0x12, 0x34, 0x0e46, "0x12 times 0x34 anotb"},
        {2, 0x12, 0x34, 0x03ba, "0x12 times 0x34 xor"},
        {0, 0xff, 0x02, 0x01fe, "0xff times two ab"},
        {1, 0xff, 0x02, 0xfc03, "0xff times two anotb"},
        {2, 0xff, 0x02, 0x02fd, "0xff times two xor"},
        {0, 0xff, 0xff, 0xfe01, "0xff times 0xff ab"},
        {1, 0xff, 0xff, 0x0000, "0xff times 0xff anotb"},
        {2, 0xff, 0xff, 0xfd02, "0xff times 0xff xor"},
    };
    for (const auto &item : cases) {
        if (!expect_product(top, item.lane, item.left, item.right, item.wide, item.label)) {
            return EXIT_FAILURE;
        }
    }

    gpo(top) = command(0, false, 0x0c, 0x0a);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x78), "repeat low byte")) {
        return EXIT_FAILURE;
    }

    std::cout << "PASS: HPS peek/poke of three packed eight-by-eight hard products verified\n";
    return EXIT_SUCCESS;
}
