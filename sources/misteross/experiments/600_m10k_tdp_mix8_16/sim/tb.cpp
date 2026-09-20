#include "verilated.h"
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {
constexpr int kUnit = 8;
constexpr int kALanes = 1;
constexpr int kBLanes = 2;
constexpr int kAAddrBits = 10;
constexpr int kBAddrBits = 9;
constexpr unsigned kDepth = 1024;
constexpr uint32_t kSignature = 0xd4240000U;
constexpr uint32_t kArm = 0x13579bdfU;
constexpr uint32_t kMask = (1u << kUnit) - 1u;
constexpr int kLanes[2] = {kALanes, kBLanes};
constexpr int kAddrBits[2] = {kAAddrBits, kBAddrBits};

uint32_t initial_lane(unsigned addr) {
    return ((addr * 73u) ^ (addr >> 1) ^ 0xa6u) & kMask;
}

uint32_t lane_data(unsigned port, unsigned seed, unsigned lane) {
    return (seed ^ ((lane + 2u * port) * 0x93u)) & kMask;
}

uint32_t word_value(unsigned port, unsigned address, const uint32_t *memory) {
    uint32_t value = 0;
    for (int i = 0; i < kLanes[port]; ++i)
        value |= memory[address * kLanes[port] + i] << (kUnit * i);
    return value;
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
    uint32_t last = 0;

    void emit(Vtop &top, uint32_t word) {
        gpo(top) = word;
        ticks(top, 8);
        last = word;
    }

    uint32_t command(unsigned port, unsigned enable, unsigned write, unsigned window) const {
        unsigned bits = kAddrBits[port];
        return (addr[port] & ((1u << bits) - 1u)) | ((seed[port] & kMask) << 16) | (port << 26) |
               (1u << 14) | (window << 27) | (enable << 28) | (write << 30);
    }

    void put(Vtop &top, unsigned port, unsigned enable = 0, unsigned write = 0,
             unsigned window = 0) {
        uint32_t word = command(port, enable, write, window);
        constexpr uint32_t controls = 0xf0000000u;
        constexpr uint32_t selector = 1u << 26;
        constexpr uint32_t payload = 0x03ff03ffu;
        if ((last ^ word) & (selector | payload)) {
            if (last & controls) emit(top, last & ~controls);
            if ((last ^ word) & selector) emit(top, last ^ selector);
            emit(top, word & ~controls);
        }
        emit(top, word);
    }

    bool check(Vtop &top, unsigned port, uint32_t value, unsigned enable, unsigned write,
               const char *label) {
        int width = kUnit * kLanes[port];
        for (unsigned window = 0; static_cast<int>(window * 16) < width; ++window) {
            put(top, port, enable, write, window);
            ticks(top, 64);
            uint32_t expected =
                kSignature | static_cast<uint32_t>((value >> (window * 16)) & 0xffffu);
            if (!expect_word(gpi(top), expected, label)) return false;
        }
        return true;
    }

    bool read(Vtop &top, unsigned port, unsigned address, const char *label,
              const uint32_t *memory) {
        put(top, port);
        addr[port] = address;
        put(top, port);
        if (!check(top, port, word_value(port, address, memory), 1u << port, 0, label))
            return false;
        put(top, port);
        return true;
    }

    void update(uint32_t *memory, unsigned port) {
        unsigned start = addr[port] * kLanes[port];
        for (int i = 0; i < kLanes[port]; ++i)
            memory[start + i] = lane_data(port, seed[port], i);
    }

    bool read_region(Vtop &top, unsigned start, unsigned stop, const char *label,
                     const uint32_t *memory) {
        for (unsigned port = 0; port < 2; ++port) {
            unsigned first = start / kLanes[port];
            unsigned last = (stop - 1) / kLanes[port];
            for (unsigned address = first; address <= last; ++address) {
                if (!read(top, port, address, label, memory)) return false;
            }
        }
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
    if (initial_lane(0) != 0xa6u) {
        std::cerr << "contents(0) must be 0xa6\n";
        return EXIT_FAILURE;
    }

    uint32_t memory[kDepth];
    for (unsigned addr = 0; addr < kDepth; ++addr) memory[addr] = initial_lane(addr);

    Ports ports;
    for (unsigned port = 0; port < 2; ++port) {
        unsigned last = kDepth / kLanes[port] - 1;
        for (unsigned address : {0u, 1u, 7u, 31u, last}) {
            if (!ports.read(top, port, address, "init", memory)) return EXIT_FAILURE;
        }
    }

    gpo(top) = kArm;
    ticks(top, 8);
    ports.last = kArm;

    for (unsigned port = 0; port < 2; ++port) {
        for (unsigned base : {0u, 32u, 1020u}) {
            for (unsigned offset = 0; offset < 4; offset += kLanes[port]) {
                ports.put(top, port);
                ports.addr[port] = (base + offset) / kLanes[port];
                ports.seed[port] = (0x155u + 0x37u * offset + 0x21u * port) & kMask;
                ports.put(top, port);
                uint32_t expected = 0;
                for (int i = 0; i < kLanes[port]; ++i)
                    expected |= lane_data(port, ports.seed[port], i) << (kUnit * i);
                if (!ports.check(top, port, expected, 1u << port, 1u << port, "write-through"))
                    return EXIT_FAILURE;
                ports.put(top, port);
                ports.update(memory, port);
                unsigned start = base > 2 ? base - 2 : 0;
                unsigned stop = base + 6 > kDepth ? kDepth : base + 6;
                if (!ports.read_region(top, start, stop, "neighbors", memory))
                    return EXIT_FAILURE;
            }
        }
    }

    for (unsigned port = 0; port < 2; ++port) {
        ports.put(top, port);
        ports.addr[port] = (64u + 8u * port) / kLanes[port];
        ports.seed[port] = (0x321u - 0x59u * port) & kMask;
        ports.put(top, port);
    }
    ports.put(top, 0, 3, 3);
    ports.put(top, 0);
    for (unsigned port = 0; port < 2; ++port) ports.update(memory, port);
    if (!ports.read_region(top, 62, 76, "simultaneous", memory)) return EXIT_FAILURE;

    for (unsigned port = 0; port < 2; ++port) {
        if (!ports.read(top, port, 0, "hold setup", memory)) return EXIT_FAILURE;
        uint32_t held = word_value(port, 0, memory);
        ports.addr[port] = 96u / kLanes[port];
        ports.seed[port] = (0x2aau - port) & kMask;
        ports.put(top, port);
        if (!ports.check(top, port, held, 0, 1u << port, "enable hold")) return EXIT_FAILURE;
        ports.put(top, port);
        if (!ports.read_region(top, 94, 100, "suppressed write", memory)) return EXIT_FAILURE;
    }

    std::cout << "PASS: mixed-width TDP init, both writers, NEW_DATA, cross-width "
                 "readback, preserved neighbors, simultaneous writes, enable hold\n";
    return EXIT_SUCCESS;
}
