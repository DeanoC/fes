// SPDX-License-Identifier: GPL-2.0-or-later
// Probe RAM width, the ROM window, the PAL/NTSC keyboard bit and NMI.
// The image is cores/fes-zx81/sim/variant_probe.hex. Pass the model name.
// The handler at $0066 stores 0x66 and clears NMIlatch with OUT ($FD),A
// before HALT. One push from the reset stack lands at $FFFF/$FFFE and does
// not alias $4000, $4010 or the wider-RAM discriminators.
#include "Vzx81_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES ZX81 variant: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

void tick(Vzx81_machine &dut) {
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
}

uint8_t peek(Vzx81_machine &dut, uint16_t addr) {
    dut.peek_addr = addr;
    dut.eval();
    tick(dut);
    return uint8_t(dut.peek_data);
}

struct Expect {
    const char *name;
    uint8_t port, rom, nmi, ram4000, ram4400, ram4800;
};

const Expect kExpect[] = {
    {"zx81", 0x5f, 0x5a, 0x66, 0xc3, 0xc3, 0xc3},
    {"zx80", 0x5f, 0xff, 0x00, 0xc3, 0xc3, 0xc3},
    {"ts1000", 0x1f, 0x5a, 0x66, 0xc3, 0xb2, 0xc3},
    {"ts1500", 0x1f, 0x5a, 0x66, 0xa1, 0xb2, 0xc3},
};

const Expect *expect_for(const char *name) {
    for (const Expect &row : kExpect) {
        if (std::strcmp(row.name, name) == 0)
            return &row;
    }
    return nullptr;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 2, "usage: Vzx81_machine <zx81|zx80|ts1000|ts1500>");
    const Expect *want = expect_for(argv[1]);
    require(want != nullptr, std::string("unknown model ") + argv[1]);

    Vzx81_machine dut;
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.keyboard = 0xffffffffffull;
    dut.tape_ready = 0;
    dut.tape_size = 0;
    dut.tape_data = 0;
    dut.peek_addr = 0;
    dut.external_ram_data = 0;
    dut.external_peek_data = 0;
    dut.bus_rdata = 0;
    dut.bus_peek_data = 0;
    dut.bus_dsel = 0;
    dut.bus_romcs = 0;
    dut.bus_wait = 0;
    dut.bus_ram_present = 0;
    dut.eval();
    for (int i = 0; i != 2048; ++i)
        tick(dut);
    dut.reset = 0;

    for (uint64_t i = 0; i != 2000000ull; ++i)
        tick(dut);

    const uint8_t port = peek(dut, 0x4010);
    const uint8_t rom = peek(dut, 0x4011);
    const uint8_t nmi = peek(dut, 0x4012);
    const uint8_t ram4000 = peek(dut, 0x4000);
    const uint8_t ram4400 = peek(dut, 0x4400);
    const uint8_t ram4800 = peek(dut, 0x4800);
    std::cout << "FES ZX81 variant " << want->name
              << std::hex
              << " port=" << int(port)
              << " rom=" << int(rom)
              << " nmi=" << int(nmi)
              << " 4000=" << int(ram4000)
              << " 4400=" << int(ram4400)
              << " 4800=" << int(ram4800)
              << std::dec << '\n';
    require(port == want->port, "keyboard port byte");
    require(rom == want->rom, "ROM window byte");
    require(nmi == want->nmi, "NMI latch byte");
    require(ram4000 == want->ram4000, "$4000 RAM byte");
    require(ram4400 == want->ram4400, "$4400 RAM byte");
    require(ram4800 == want->ram4800, "$4800 RAM byte");
    return 0;
}
