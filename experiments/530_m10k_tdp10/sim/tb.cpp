#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr int kWidth = 10;
constexpr int kAddrBits = 10;
constexpr unsigned kDepth = 1024;
constexpr uint32_t kSignature = 0xd41d0000U;
constexpr uint32_t kArm = 0x13579bdfU;
constexpr uint32_t kMask = (1u << kWidth) - 1u;

uint32_t initial_word(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & kMask;
}

uint32_t write_value(unsigned port, unsigned seed) {
    return (seed ^ (port ? 0x2bc00u : 0x93a00u)) & kMask;
}

uint32_t command(unsigned addr, unsigned seed, unsigned port, unsigned enable, unsigned write,
                 unsigned window) {
    return (addr & ((1u << kAddrBits) - 1u)) | ((seed & 0x3ffu) << 16) | (port << 26) |
           (1u << 14) | (window << 27) | (enable << 28) | (write << 30);
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

struct Ports {
    unsigned addr[2] = {0, 0};
    unsigned seed[2] = {0, 0};

    void put(Vtop &top, unsigned port, unsigned enable = 0, unsigned write = 0,
             unsigned window = 0) {
        gpo(top) = command(addr[port], seed[port], port, enable, write, window);
        ticks(top, 8);
    }

    bool check(Vtop &top, unsigned port, uint32_t value, unsigned enable = 0, unsigned write = 0,
               const char *label = "value") {
        unsigned window = 0;
        while (static_cast<int>(window * 16) < kWidth) {
            put(top, port, enable, write, window);
            ticks(top, 64);
            uint32_t expected =
                kSignature | static_cast<uint32_t>((value >> (window * 16)) & 0xffffu);
            if (!expect_word(gpi(top), expected, label)) return false;
            window += 1;
        }
        return true;
    }

    bool read(Vtop &top, unsigned port, unsigned address, const char *label, uint32_t *memory) {
        put(top, port);
        addr[port] = address;
        put(top, port);
        if (!check(top, port, memory[address], 1u << port, 0, label)) return false;
        put(top, port);
        return true;
    }
};
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    gpo(top) = 0;
    top.eval();
    ticks(top, 32);
    if (!expect_word(gpi(top) & 0xffff0000U, kSignature, "signature")) return EXIT_FAILURE;
    if (initial_word(0) != 0xa6u) {
        std::cerr << "contents(0) must be 0xa6\n";
        return EXIT_FAILURE;
    }

    uint32_t memory[kDepth];
    for (unsigned addr = 0; addr < kDepth; ++addr) memory[addr] = initial_word(addr);

    Ports ports;
    for (unsigned port = 0; port < 2; ++port) {
        for (unsigned address : {0u, 1u, 7u, 31u, kDepth - 1}) {
            if (!ports.read(top, port, address, "init", memory)) return EXIT_FAILURE;
        }
    }

    gpo(top) = kArm;
    ticks(top, 8);

    const unsigned write_cases[][2] = {{0u, 0u}, {7u, 0x155u}, {kDepth - 1, 0x3ffu}};
    for (unsigned port = 0; port < 2; ++port) {
        for (const auto &entry : write_cases) {
            unsigned address = entry[0];
            unsigned seed = entry[1];
            ports.put(top, port);
            ports.addr[port] = address;
            ports.seed[port] = seed;
            ports.put(top, port);
            uint32_t value = write_value(port, seed);
            if (!ports.check(top, port, value, 1u << port, 1u << port, "write-through"))
                return EXIT_FAILURE;
            ports.put(top, port);
            memory[address] = value;
            if (!ports.read(top, port, address, "own-port readback", memory)) return EXIT_FAILURE;
            if (!ports.read(top, 1u - port, address, "opposite-port readback", memory))
                return EXIT_FAILURE;
        }
    }

    for (unsigned port = 0; port < 2; ++port) {
        ports.addr[port] = 20u + port;
        ports.seed[port] = 0x123u + port;
        ports.put(top, port);
    }
    ports.put(top, 0, 3, 3);
    ports.put(top, 0);
    for (unsigned port = 0; port < 2; ++port) {
        memory[20u + port] = write_value(port, ports.seed[port]);
        if (!ports.read(top, port, 20u + port, "simultaneous own", memory)) return EXIT_FAILURE;
        if (!ports.read(top, 1u - port, 20u + port, "simultaneous opposite", memory))
            return EXIT_FAILURE;
    }

    for (unsigned port = 0; port < 2; ++port) {
        if (!ports.read(top, port, 0, "hold setup", memory)) return EXIT_FAILURE;
        uint32_t held = memory[0];
        ports.addr[port] = 31;
        ports.seed[port] = 0x2aa;
        if (!ports.check(top, port, held, 0, 1u << port, "enable hold")) return EXIT_FAILURE;
        ports.put(top, port);
        if (!ports.read(top, port, 31, "suppressed write", memory)) return EXIT_FAILURE;
    }

    std::cout << "PASS: true dual-port 10-bit M10K init, both writers, write-through, "
                 "opposite-port readback, simultaneous writes, enable hold\n";
    return EXIT_SUCCESS;
}
