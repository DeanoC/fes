// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vzx81_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES ZX81 machine: " << message << '\n';
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
    return uint8_t(dut.peek_data);
}

uint16_t dfile_ptr(Vzx81_machine &dut) {
    return uint16_t(peek(dut, 0x400c)) | (uint16_t(peek(dut, 0x400d)) << 8);
}

bool basic_ready(Vzx81_machine &dut) {
    const uint16_t dfile = dfile_ptr(dut);
    return peek(dut, 0x4000) == 0 && dfile >= 0x4070 && dfile < 0x8000 &&
           peek(dut, dfile) == 0x76;
}

void hold_row(Vzx81_machine &dut, unsigned row, uint8_t bits, unsigned cycles) {
    uint64_t keys = 0xffffffffffull;
    keys &= ~(0x1full << (row * 5));
    keys |= (uint64_t(bits) & 0x1full) << (row * 5);
    dut.keyboard = keys;
    for (unsigned i = 0; i != cycles; ++i) tick(dut);
    dut.keyboard = 0xffffffffffull;
    dut.eval();
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vzx81_machine dut;
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.keyboard = 0xffffffffffull;
    dut.tape_ready = 0;
    dut.tape_size = 0;
    dut.tape_data = 0;
    dut.peek_addr = 0x400c;
    dut.eval();
    for (int i = 0; i != 2048; ++i) tick(dut);
    dut.reset = 0;

    bool saw_halt = false;
    bool saw_basic = false;
    uint64_t video_hits = 0;
    const uint64_t limit = 40000000ull;
    for (uint64_t cycle = 0; cycle != limit; ++cycle) {
        tick(dut);
        if (!dut.halt_n)
            saw_halt = true;
        if (dut.ce_6m5 && !dut.hblank && !dut.vblank && dut.video_pixel)
            video_hits++;
        if (!saw_basic && (cycle & 0x1ffffull) == 0 && basic_ready(dut)) {
            saw_basic = true;
            std::cout << "FES ZX81: BASIC ready after " << cycle << " clocks\n";
        }
        if (saw_basic && saw_halt && video_hits > 0)
            break;
    }
    require(saw_basic, "NEW did not create a BASIC display file");
    require(saw_halt, "CPU never entered HALT display wait");
    require(video_hits > 0, "ULA produced no visible pixels");

    const uint16_t dfile = dfile_ptr(dut);
    std::cout << "FES ZX81: D_FILE=" << std::hex << dfile << std::dec
              << " video_hits=" << video_hits << '\n';

    dut.tape_ready = 1;
    dut.tape_size = 16;
    const uint8_t before = peek(dut, dfile + 1);
    hold_row(dut, 6, 0x17, 2000000);  // J / LOAD in K mode
    bool line_changed = peek(dut, dfile + 1) != before;
    hold_row(dut, 6, 0x1e, 1000000);  // ENTER
    require(basic_ready(dut), "keyboard lost BASIC");
    std::cout << "FES ZX81 machine simulation passed"
              << (line_changed ? " (line edited)\n" : " (BASIC retained)\n");
    return 0;
}
