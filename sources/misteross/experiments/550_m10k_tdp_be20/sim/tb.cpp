#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr int kWidth = 20;
constexpr int kLane = 10;
constexpr int kAddrBits = 9;
constexpr unsigned kDepth = 512;
constexpr uint32_t kSignature = 0xd41f0000U;
constexpr uint32_t kArm = 0x13579bdfU;
constexpr uint32_t kMask = (1u << kWidth) - 1u;
constexpr uint32_t kLaneMask = (1u << kLane) - 1u;

uint32_t initial_word(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & kMask;
}

uint32_t write_value(unsigned port, unsigned seed) {
    return (seed ^ (port ? 0x2bc00u : 0x93a00u)) & kMask;
}

uint32_t written_bits(unsigned byte_mask) {
    uint32_t bits = 0;
    if (byte_mask & 1u) bits |= kLaneMask;
    if (byte_mask & 2u) bits |= kLaneMask << kLane;
    return bits;
}

uint32_t command(unsigned addr, unsigned seed, unsigned port, unsigned mask_a, unsigned mask_b,
                 unsigned enable, unsigned write, unsigned window) {
    return (addr & ((1u << kAddrBits) - 1u)) | ((seed & 0x3ffu) << 16) | (port << 26) |
           ((mask_a & 3u) << 10) | ((mask_b & 3u) << 12) | (1u << 14) | (window << 27) |
           (enable << 28) | (write << 30);
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
    unsigned masks[2] = {3, 3};

    void put(Vtop &top, unsigned port, unsigned enable = 0, unsigned write = 0,
             unsigned window = 0) {
        gpo(top) = command(addr[port], seed[port], port, masks[0], masks[1], enable, write, window);
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

    const unsigned mask_cases[][2] = {{1u, 0x155u}, {2u, 0x2aau}, {0u, 0x3ffu}, {3u, 0x123u}};
    for (unsigned port = 0; port < 2; ++port) {
        for (unsigned address : {0u, 7u, kDepth - 1}) {
            for (const auto &entry : mask_cases) {
                unsigned byte_mask = entry[0];
                unsigned seed = entry[1];
                if (!ports.read(top, port, address, "before masked write", memory))
                    return EXIT_FAILURE;
                uint32_t held = memory[address];
                ports.addr[port] = address;
                ports.seed[port] = seed;
                ports.masks[port] = byte_mask;
                ports.put(top, port);
                if (!ports.check(top, port, held, 1u << port, 1u << port, "output hold"))
                    return EXIT_FAILURE;
                ports.put(top, port);
                uint32_t bits = written_bits(byte_mask);
                memory[address] = (memory[address] & ~bits) | (write_value(port, seed) & bits);
                if (!ports.read(top, port, address, "own-port preserved", memory))
                    return EXIT_FAILURE;
                if (!ports.read(top, 1u - port, address, "opposite-port preserved", memory))
                    return EXIT_FAILURE;
            }
        }
    }

    for (unsigned port = 0; port < 2; ++port) {
        ports.addr[port] = 20u + port;
        ports.seed[port] = 0x321u - port;
        ports.masks[port] = 1u << port;
        ports.put(top, port);
    }
    ports.put(top, 0, 3, 3);
    ports.put(top, 0);
    for (unsigned port = 0; port < 2; ++port) {
        uint32_t bits = written_bits(ports.masks[port]);
        unsigned address = 20u + port;
        memory[address] =
            (memory[address] & ~bits) | (write_value(port, ports.seed[port]) & bits);
        if (!ports.read(top, port, address, "simultaneous own", memory)) return EXIT_FAILURE;
        if (!ports.read(top, 1u - port, address, "simultaneous opposite", memory))
            return EXIT_FAILURE;
    }

    for (unsigned port = 0; port < 2; ++port) {
        if (!ports.read(top, port, 0, "hold setup", memory)) return EXIT_FAILURE;
        uint32_t held = memory[0];
        ports.addr[port] = 31;
        ports.seed[port] = 0x2aa;
        ports.masks[port] = 3;
        if (!ports.check(top, port, held, 0, 1u << port, "enable hold")) return EXIT_FAILURE;
        ports.put(top, port);
        if (!ports.read(top, port, 31, "suppressed write", memory)) return EXIT_FAILURE;
    }

    std::cout << "PASS: byte-masked true dual-port 20-bit M10K init, low/high/zero/full "
                 "masks, output hold, simultaneous writes, enable hold\n";
    return EXIT_SUCCESS;
}
