#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xd4280000U;
constexpr uint32_t kArm = 0x13579bdfU;
constexpr uint32_t kAclr = 1u << 5;

uint32_t initial_word(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & 0xfffffu;
}

uint32_t command(unsigned addr, bool we, bool re, bool clkena, bool aclr) {
    return (static_cast<uint32_t>(we) << 31) | (static_cast<uint32_t>(re) << 30) |
           (static_cast<uint32_t>(clkena) << 29) | (addr & 0xfu) | (aclr ? kAclr : 0u);
}

uint32_t write_command(unsigned addr, uint32_t data20) {
    return (1u << 31) | (1u << 30) | ((data20 & 0xfffffu) << 9) | (addr & 0xfu);
}

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

void ticks(Vtop &top, int count) {
    for (int i = 0; i < count; ++i)
        tick(top);
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

bool expect_read(Vtop &top, unsigned addr, uint32_t word, const char *label) {
    gpo(top) = command(addr, false, true, true, false);
    ticks(top, 64);
    return expect_word(gpi(top), kSignature | (word & 0xffffu), label);
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    gpo(top) = 0;
    top.eval();
    ticks(top, 32);
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature"))
        return EXIT_FAILURE;
    for (unsigned addr : {0u, 1u, 2u, 3u, 7u, 15u}) {
        if (!expect_read(top, addr, initial_word(addr), "init"))
            return EXIT_FAILURE;
    }

    const unsigned addr = 7;
    const uint32_t old_word = initial_word(addr);
    gpo(top) = command(addr, false, true, true, true);
    ticks(top, 64);
    if (!expect_word(gpi(top), kSignature, "aclr init"))
        return EXIT_FAILURE;
    if (!expect_read(top, addr, old_word, "restore init"))
        return EXIT_FAILURE;

    gpo(top) = kArm;
    ticks(top, 8);
    const uint32_t written = 0x155u;
    gpo(top) = write_command(addr, written);
    ticks(top, 16);
    if (!expect_read(top, addr, written, "write"))
        return EXIT_FAILURE;
    gpo(top) = command(addr, false, true, true, true);
    ticks(top, 64);
    if (!expect_word(gpi(top), kSignature, "aclr write"))
        return EXIT_FAILURE;
    if (!expect_read(top, addr, written, "restore write"))
        return EXIT_FAILURE;
    if (!expect_read(top, 1u, initial_word(1u), "untouched addr1"))
        return EXIT_FAILURE;
    std::cout << "PASS: inferred M10K ACLR1 clears the read register and leaves storage intact\n";
    return EXIT_SUCCESS;
}
