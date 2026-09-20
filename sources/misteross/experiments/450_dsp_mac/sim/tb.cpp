#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd6150000U;
void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1; top.eval();
    top.FPGA_CLK1_50 = 0; top.eval();
}
uint32_t &gpo(Vtop &top) { return top.top->hps_gp->gpo; }
uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }
bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected) return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}
bool expect_low(Vtop &top, uint8_t left, uint8_t right, uint16_t addend, uint16_t low, const char *label) {
    gpo(top) = (static_cast<uint32_t>(addend) << 16) | (static_cast<uint32_t>(right) << 8) | left;
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
    const struct { uint8_t left, right; uint16_t addend, low; const char *label; } cases[] = {
        {0x0a, 0x0c, 0x0000, 0x0078, "ten times twelve"},
        {0x0a, 0x0c, 0x0005, 0x007d, "ten times twelve plus five"},
        {0xff, 0xff, 0x0000, 0xfe01, "0xff times 0xff"},
        {0x10, 0x10, 0x0100, 0x0200, "sixteen squared plus 0x100"},
    };
    for (const auto &item : cases) {
        if (!expect_low(top, item.left, item.right, item.addend, item.low, item.label)) return EXIT_FAILURE;
    }
    std::cout << "PASS: HPS peek/poke of M18 multiply-add verified\n";
    return EXIT_SUCCESS;
}
