// SPDX-License-Identifier: GPL-2.0-or-later
// Run the ZX80 cassette entry and verify its first two bytes land at $4000.
#include "Vzx81_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES ZX80 tape load: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

void tick(Vzx81_machine &dut) {
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vzx81_machine dut;
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.keyboard = 0xffffffffffull;
    dut.tape_ready = 1;
    dut.tape_size = 2;
    dut.tape_data = 0xa5;
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

    unsigned writes = 0;
    for (uint64_t cycle = 0; cycle != 1000000 && writes != 2; ++cycle) {
        // The synchronous RAM samples these outputs on the next rising edge.
        if (dut.ram_write_enable) {
            const uint16_t address = uint16_t(dut.bus_addr);
            const uint8_t data = uint8_t(dut.ram_write_data);
            if (address != uint16_t(0x4000 + writes))
                fail(writes == 0 ? "first byte did not land at $4000"
                                 : "second byte did not land at $4001");
            require(data == 0xa5, "tape byte data changed during the write");
            ++writes;
        }
        tick(dut);
    }
    require(writes == 2, "ZX80 cassette entry did not write two tape bytes");
    std::cout << "FES ZX80 tape load wrote $4000-$4001\n";
    return 0;
}
