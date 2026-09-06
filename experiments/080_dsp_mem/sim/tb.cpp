#include "verilated.h"

#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

constexpr uint32_t kSignature = 0xd8100000U;
constexpr uint32_t kProductView = 0;
constexpr uint32_t kLabView = 1U << 17;
constexpr uint32_t kBlockView = 2U << 17;

uint32_t command(uint32_t view, bool flag, uint8_t high, uint8_t low) {
    return view | (static_cast<uint32_t>(flag) << 16) | (static_cast<uint32_t>(high) << 8) | low;
}

uint32_t observed(uint8_t data) {
    return kSignature | data;
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

bool expect_product(Vtop &top, uint8_t left, uint8_t right, uint16_t wide, const char *label) {
    gpo(top) = command(kProductView, false, right, left);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(static_cast<uint8_t>(wide)), label)) {
        return false;
    }
    gpo(top) = command(kProductView, true, right, left);
    tick(top);
    return expect_word(
        data_word(gpi(top)),
        observed(static_cast<uint8_t>(wide >> 8)),
        label
    );
}

bool write_then_read(Vtop &top, uint32_t view, uint8_t addr, uint8_t wdata, const char *label) {
    gpo(top) = command(view, true, wdata, addr);
    tick(top);
    gpo(top) = command(view, false, 0, addr);
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

    if (!expect_product(top, 0x0a, 0x0c, 0x0078, "ten times twelve") ||
        !expect_product(top, 0xff, 0xff, 0xfe01, "0xff times 0xff")) {
        return EXIT_FAILURE;
    }

    if (!write_then_read(top, kLabView, 1, 0xa5, "lab index 1") ||
        !write_then_read(top, kLabView, 2, 0x5a, "lab index 2") ||
        !write_then_read(top, kBlockView, 1, 0x22, "block index 1") ||
        !write_then_read(top, kBlockView, 255, 0xee, "block index 255")) {
        return EXIT_FAILURE;
    }

    gpo(top) = command(kLabView, false, 0, 2);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x5a), "lab index 2 still")) {
        return EXIT_FAILURE;
    }
    gpo(top) = command(kBlockView, false, 0, 1);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0x22), "block index 1 still")) {
        return EXIT_FAILURE;
    }

    gpo(top) = command(kProductView, true, 0x77, 1);
    tick(top);
    gpo(top) = command(kLabView, false, 0, 1);
    tick(top);
    if (!expect_word(data_word(gpi(top)), observed(0xa5), "product view did not write lab")) {
        return EXIT_FAILURE;
    }

    if (!expect_product(top, 0xff, 0x02, 0x01fe, "product after table writes")) {
        return EXIT_FAILURE;
    }

    std::cout << "PASS: HPS peek/poke of DSP product and mixed tables verified\n";
    return EXIT_SUCCESS;
}
