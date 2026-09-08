#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr int kWidth = 40;
constexpr uint32_t kSignature = 0xd4190000U;
constexpr uint32_t kArm = 0x13579bdfU;
constexpr int kAddrBits = 8;

uint64_t initial_word(unsigned addr) {
    uint64_t low = ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0xfffffu;
    if (kWidth == 40) return ((~low & 0xfffffu) << 20) | low;
    return low;
}

uint32_t command(unsigned addr, unsigned window, bool we, bool re, bool clkena) {
    return (static_cast<uint32_t>(we) << 31) | (static_cast<uint32_t>(re) << 30) |
           (static_cast<uint32_t>(clkena) << 29) | (window << 27) |
           (addr & ((1u << kAddrBits) - 1u));
}

uint32_t write_command(unsigned addr, uint32_t data20, bool clkena) {
    return (1u << 31) | (1u << 30) | (static_cast<uint32_t>(clkena) << 29) |
           ((data20 & 0xfffffu) << 9) | (addr & ((1u << kAddrBits) - 1u));
}

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

void ticks(Vtop &top, int count) {
    for (int i = 0; i < count; ++i) tick(top);
}

uint32_t &gpo(Vtop &top) { return top.top->hps_gp->gpo; }
uint32_t gpi(const Vtop &top) { return top.top->hps_gp->gpi; }

bool expect_word(uint32_t value, uint32_t expected, const char *label) {
    if (value == expected) return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << value << std::dec << '\n';
    return false;
}

bool expect_read(Vtop &top, unsigned addr, uint64_t word, const char *label) {
    unsigned window = 0;
    while (static_cast<int>(window * 16) < kWidth) {
        gpo(top) = command(addr, window, false, true, true);
        ticks(top, 64);
        uint32_t expected = kSignature | static_cast<uint32_t>((word >> (window * 16)) & 0xffffu);
        if (!expect_word(gpi(top), expected, label)) return false;
        window += 1;
    }
    return true;
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    gpo(top) = 0;
    top.eval();
    ticks(top, 32);
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) return EXIT_FAILURE;
    if ((initial_word(0) & 0xfffffu) != 0xa6u) {
        std::cerr << "contents(0) low half must be 0xa6\n";
        return EXIT_FAILURE;
    }
    for (unsigned addr : {0u, 1u, 2u, 3u, 7u, 15u, 31u, 63u, 127u, 255u}) {
        if (!expect_read(top, addr, initial_word(addr), "init")) return EXIT_FAILURE;
    }
    const uint64_t old_word = initial_word(1);
    uint32_t written = 0xa5a3c;
    uint64_t written_word = (static_cast<uint64_t>(~written & 0xfffffu) << 20) | written;

    gpo(top) = kArm;
    ticks(top, 8);
    if (!expect_read(top, 1, old_word, "addr1 before write")) return EXIT_FAILURE;
    gpo(top) = command(1, 0, false, true, false);
    ticks(top, 16);
    gpo(top) = write_command(7, written, false);
    ticks(top, 16);
    gpo(top) = command(7, 0, false, true, false);
    ticks(top, 16);
    if (!expect_word(gpi(top), kSignature | static_cast<uint32_t>(old_word & 0xffffu),
                     "held while read clock stopped"))
        return EXIT_FAILURE;
    if (!expect_read(top, 7, written_word, "resume after stopped write")) return EXIT_FAILURE;
    if (!expect_read(top, 1, old_word, "untouched addr1")) return EXIT_FAILURE;
    gpo(top) = command(7, 0, false, false, true);
    ticks(top, 16);
    if (!expect_word(gpi(top), kSignature | static_cast<uint32_t>(old_word & 0xffffu),
                     "read-enable hold"))
        return EXIT_FAILURE;
    if (!expect_read(top, 7, written_word, "read-enable resume")) return EXIT_FAILURE;
    gpo(top) = command(7, 0, false, true, true) | (0x1234u << 9);
    ticks(top, 16);
    if (!expect_read(top, 7, written_word, "write-enable hold")) return EXIT_FAILURE;
    std::cout << "PASS: independent-clock 40-bit M10K SDP init, stopped-clock write, enables\n";
    return EXIT_SUCCESS;
}
