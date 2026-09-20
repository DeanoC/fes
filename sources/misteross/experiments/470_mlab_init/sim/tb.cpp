#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd4170000U;

uint8_t contents(uint8_t addr) {
    return static_cast<uint8_t>(((addr * 73) ^ (addr >> 1) ^ 0xa6) & 0xff);
}

uint32_t command(bool we, uint8_t wdata, uint8_t addr) {
    return (static_cast<uint32_t>(we) << 16) | (static_cast<uint32_t>(wdata) << 8) |
           (static_cast<uint32_t>(addr) & 0x1fU);
}

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
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

bool expect_read(Vtop &top, uint8_t addr, uint8_t data, const char *label) {
    gpo(top) = command(false, 0, addr);
    tick(top);
    return expect_word(data_word(gpi(top)), kSignature | data, label);
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) return EXIT_FAILURE;
    tick(top);
    if (!expect_read(top, 0, contents(0), "init address 0")) return EXIT_FAILURE;
    if (contents(0) != 0xa6) {
        std::cerr << "contents(0) must be 0xa6\n";
        return EXIT_FAILURE;
    }
    for (uint8_t addr : {uint8_t{1}, uint8_t{2}, uint8_t{17}, uint8_t{31}}) {
        if (!expect_read(top, addr, contents(addr), "init address")) return EXIT_FAILURE;
    }
    gpo(top) = command(true, 0x11, 2);
    tick(top);
    if (!expect_read(top, 2, 0x11, "replaced even address")) return EXIT_FAILURE;
    if (!expect_read(top, 1, contents(1), "untouched odd address")) return EXIT_FAILURE;
    if (!expect_read(top, 3, contents(3), "untouched later odd address")) return EXIT_FAILURE;
    std::cout << "PASS: HPS peek/poke of initialized MLAB table verified\n";
    return EXIT_SUCCESS;
}
