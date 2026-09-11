#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd6190000U;
void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}
uint32_t &gpo(Vtop &top) { return top.top->hps_gp->gpo; }
uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }
bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected)
        return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}
bool expect_low(Vtop &top, uint8_t left, uint8_t right, uint8_t other, bool second, uint16_t low,
                const char *label) {
    gpo(top) = (static_cast<uint32_t>(second) << 24) | (static_cast<uint32_t>(other) << 16) |
               (static_cast<uint32_t>(right) << 8) | left;
    tick(top);
    return expect_word(gpi(top), kSignature | low, label);
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature"))
        return EXIT_FAILURE;
    if (!expect_low(top, 10, 12, 7, false, 120, "ten times twelve"))
        return EXIT_FAILURE;
    if (!expect_low(top, 10, 12, 7, true, 21, "seven times three"))
        return EXIT_FAILURE;
    if (!expect_low(top, 16, 16, 5, false, 256, "sixteen squared"))
        return EXIT_FAILURE;
    if (!expect_low(top, 16, 16, 5, true, 15, "five times three"))
        return EXIT_FAILURE;
    std::cout << "PASS: HPS peek/poke of native 18x19 dual product verified\n";
    return EXIT_SUCCESS;
}
