#include "verilated.h"

#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

constexpr uint32_t kSignature = 0xd7100000U;

uint32_t command(bool we, bool bank, uint8_t wdata, uint8_t addr) {
    return (static_cast<uint32_t>(bank) << 17) | (static_cast<uint32_t>(we) << 16) |
           (static_cast<uint32_t>(wdata) << 8) | addr;
}

uint32_t observed(uint8_t rdata) {
    return kSignature | rdata;
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

bool write_then_read(Vtop &top, bool bank, uint8_t addr, uint8_t wdata, const char *label) {
    gpo(top) = command(true, bank, wdata, addr);
    tick(top);
    gpo(top) = command(false, bank, 0, addr);
    tick(top);
    return expect_word(data_word(gpi(top)), observed(wdata), label);
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

    if (!write_then_read(top, false, 1, 0xa5, "lab index 1") ||
        !write_then_read(top, false, 2, 0x5a, "lab index 2") ||
        !write_then_read(top, false, 31, 0x3c, "lab index 31") ||
        !write_then_read(top, true, 0, 0x11, "block index 0") ||
        !write_then_read(top, true, 1, 0x22, "block index 1") ||
        !write_then_read(top, true, 255, 0xee, "block index 255")) {
        return EXIT_FAILURE;
    }

    gpo(top) = command(false, false, 0, 2);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x5a), "lab index 2 still")) {
        return EXIT_FAILURE;
    }
    gpo(top) = command(false, true, 0, 1);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x22), "block index 1 still")) {
        return EXIT_FAILURE;
    }

    gpo(top) = command(true, false, 0x77, 1);
    tick(top);
    gpo(top) = command(false, true, 0, 1);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x22), "block untouched by lab write")) {
        return EXIT_FAILURE;
    }
    gpo(top) = command(false, false, 0, 1);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x77), "replaced lab index 1")) {
        return EXIT_FAILURE;
    }

    std::cout << "PASS: HPS peek/poke of separate lab and block tables verified\n";
    return EXIT_SUCCESS;
}
