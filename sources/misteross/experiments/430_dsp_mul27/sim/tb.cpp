#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd6130000U;
void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}
uint32_t &gpo(Vtop &top) { return top.top->hps_gp->gpo; }
uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }
bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected) return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}
bool expect_low(Vtop &top, uint32_t left, uint8_t right, uint16_t low, const char *label) {
    gpo(top) = (static_cast<uint32_t>(right) << 20) | (left & 0xfffffU);
    tick(top);
    return expect_word(gpi(top), kSignature | low, label);
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) return EXIT_FAILURE;
    const struct { uint32_t left; uint8_t right; uint16_t low; const char *label; } cases[] = {
        {0x00000, 0x00, 0x0000, "zero"},
        {0x0000a, 0x0c, 0x0078, "ten times twelve"},
        {0x10000, 0x02, 0x0000, "2^16 times two low"},
        {0x12345, 0x03, 0x69cf, "0x12345 times three"},
        {0xfffff, 0x02, 0xfffe, "20-bit ones times two"},
    };
    for (const auto &item : cases) {
        if (!expect_low(top, item.left, item.right, item.low, item.label)) return EXIT_FAILURE;
    }
    std::cout << "PASS: HPS peek/poke of twenty-by-eight hard product verified\n";
    return EXIT_SUCCESS;
}
