#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd6140000U;
uint32_t command(bool high_select, uint8_t z, uint8_t right, uint8_t left) {
    return (static_cast<uint32_t>(high_select) << 24) | (static_cast<uint32_t>(z) << 16) |
           (static_cast<uint32_t>(right) << 8) | left;
}
void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1; top.eval();
    top.FPGA_CLK1_50 = 0; top.eval();
}
uint32_t &gpo(Vtop &top) { return top.top->hps_gp->gpo; }
uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }
uint32_t data_word(uint32_t value) { return value & 0xffff00ffU; }
bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected) return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}
bool expect_product(Vtop &top, uint8_t left, uint8_t right, uint8_t z, uint16_t wide, const char *label) {
    gpo(top) = command(false, z, right, left);
    tick(top);
    if (!expect_word(data_word(gpi(top)), kSignature | (wide & 0xff), label)) return false;
    gpo(top) = command(true, z, right, left);
    tick(top);
    return expect_word(data_word(gpi(top)), kSignature | ((wide >> 8) & 0xff), label);
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) return EXIT_FAILURE;
    const struct { uint8_t left, right, z; uint16_t wide; const char *label; } cases[] = {
        {0x0a, 0x0c, 0x00, 0x0078, "ten times twelve"},
        {0x0a, 0x0c, 0x02, 0x0064, "ten times (twelve minus two)"},
        {0xff, 0x05, 0x01, 0x03fc, "0xff times four"},
        {0x10, 0x10, 0x10, 0x0000, "sixteen times zero"},
    };
    for (const auto &item : cases) {
        if (!expect_product(top, item.left, item.right, item.z, item.wide, item.label)) return EXIT_FAILURE;
    }
    std::cout << "PASS: HPS peek/poke of M9 preadder product verified\n";
    return EXIT_SUCCESS;
}
