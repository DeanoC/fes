#include "verilated.h"

#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

constexpr uint32_t kSignature = 0xd4100000U;

uint32_t command(bool we, uint8_t wdata, uint8_t addr) {
    return (static_cast<uint32_t>(we) << 16) | (static_cast<uint32_t>(wdata) << 8) |
           (static_cast<uint32_t>(addr) & 0x1fU);
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

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();

    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) {
        return EXIT_FAILURE;
    }

    const uint8_t pairs[][2] = {{1, 0xa5}, {2, 0x5a}, {31, 0x3c}};
    for (const auto &pair : pairs) {
        gpo(top) = command(true, pair[1], pair[0]);
        tick(top);
        gpo(top) = command(false, 0, pair[0]);
        tick(top);
        if (!expect_word(data_word(gpi(top)), observed(pair[1]), "write data")) {
            return EXIT_FAILURE;
        }
    }

    gpo(top) = command(false, 0, 2);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x5a), "read back index 2")) {
        return EXIT_FAILURE;
    }
    gpo(top) = command(false, 0, 1);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0xa5), "read back index 1")) {
        return EXIT_FAILURE;
    }
    gpo(top) = command(false, 0, 31);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x3c), "read back index 31")) {
        return EXIT_FAILURE;
    }

    gpo(top) = command(true, 0x11, 1);
    tick(top);
    gpo(top) = command(false, 0, 2);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x5a), "untouched index 2")) {
        return EXIT_FAILURE;
    }
    gpo(top) = command(false, 0, 1);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x11), "replaced index 1")) {
        return EXIT_FAILURE;
    }

    std::cout << "PASS: HPS peek/poke write-then-read of eight-bit table verified\n";
    return EXIT_SUCCESS;
}
