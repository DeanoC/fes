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

uint16_t word_at(Vzx81_machine &dut, uint16_t addr) {
    return uint16_t(peek(dut, addr)) | (uint16_t(peek(dut, addr + 1)) << 8);
}

uint16_t dfile_ptr(Vzx81_machine &dut) {
    return word_at(dut, 0x400c);
}

uint16_t eline_ptr(Vzx81_machine &dut) {
    return word_at(dut, 0x4014);
}

bool basic_ready(Vzx81_machine &dut) {
    const uint16_t dfile = dfile_ptr(dut);
    return peek(dut, 0x4000) == 0 && dfile >= 0x4070 && dfile < 0x8000 &&
           peek(dut, dfile) == 0x76;
}

void set_keys(Vzx81_machine &dut, uint64_t keys) {
    dut.keyboard = keys;
}

void all_up(Vzx81_machine &dut) {
    dut.keyboard = 0xffffffffffull;
    dut.eval();
}

uint64_t row_bits(unsigned row, uint8_t bits) {
    uint64_t keys = 0xffffffffffull;
    keys &= ~(0x1full << (row * 5));
    keys |= (uint64_t(bits) & 0x1full) << (row * 5);
    return keys;
}

bool eline_has(Vzx81_machine &dut, uint8_t token) {
    const uint16_t eline = eline_ptr(dut);
    for (uint16_t a = eline; a < eline + 16 && a < 0x8000; ++a) {
        if (peek(dut, a) == token)
            return true;
    }
    return false;
}

unsigned eline_count(Vzx81_machine &dut, uint8_t token) {
    unsigned n = 0;
    const uint16_t eline = eline_ptr(dut);
    for (uint16_t a = eline; a < eline + 16 && a < 0x8000; ++a) {
        if (peek(dut, a) == token)
            n++;
    }
    return n;
}

void dump_state(Vzx81_machine &dut, const char *tag) {
    const uint16_t eline = eline_ptr(dut);
    std::cout << " " << tag << " E_LINE=" << std::hex << eline << " bytes=";
    for (uint16_t a = eline; a < eline + 8 && a < 0x8000; ++a)
        std::cout << int(peek(dut, a)) << ' ';
    std::cout << " LAST_K=" << word_at(dut, 0x4025)
              << " DEBOUNCE=" << int(peek(dut, 0x4027)) << std::dec << '\n';
}

bool wait_idle(Vzx81_machine &dut, uint64_t cycles) {
    all_up(dut);
    for (uint64_t i = 0; i != cycles; ++i) {
        tick(dut);
        if (dut.cpu_addr == 0x04cf && word_at(dut, 0x4025) == 0xffff &&
            peek(dut, 0x4027) == 0)
            return true;
        if ((i & 0x1ffffffull) == 0 && i != 0)
            std::cout << " still waiting for idle at " << i << " E_LINE="
                      << std::hex << eline_ptr(dut) << std::dec << '\n';
    }
    return dut.cpu_addr == 0x04cf && word_at(dut, 0x4025) == 0xffff &&
           peek(dut, 0x4027) == 0;
}

bool type_until(Vzx81_machine &dut, uint64_t keys, uint8_t token, unsigned need,
                uint64_t cycles) {
    set_keys(dut, keys);
    for (uint64_t i = 0; i != cycles; ++i) {
        tick(dut);
        if ((i & 0xffff) == 0 && eline_count(dut, token) >= need)
            return true;
    }
    return eline_count(dut, token) >= need;
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
    dut.bus_rdata = 0;
    dut.bus_peek_data = 0;
    dut.bus_dsel = 0;
    dut.bus_romcs = 0;
    dut.bus_wait = 0;
    dut.bus_ram_present = 0;
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
    std::cout << "FES ZX81: D_FILE=" << std::hex << dfile_ptr(dut) << std::dec
              << " video_hits=" << video_hits << '\n';

    require(wait_idle(dut, 400000000ull), "SLOW-DISP never waited for a key");
    dump_state(dut, "idle");

    require(type_until(dut, row_bits(6, 0x17), 0xef, 1, 10000000ull),
            "J did not write LOAD 0xEF into E_LINE");
    dump_state(dut, "J");
    require(wait_idle(dut, 80000000ull), "debounce did not return to 0 after J");

    // SHIFT+P is quote (0x0B). LOAD "" is a nameless program name.
    const uint64_t quote = row_bits(0, 0x1e) & row_bits(5, 0x1e);
    require(type_until(dut, quote, 0x0b, 1, 10000000ull),
            "SHIFT+P did not write quote");
    dump_state(dut, "quote1");
    require(wait_idle(dut, 80000000ull), "debounce did not return after quote");
    require(type_until(dut, quote, 0x0b, 2, 10000000ull),
            "second SHIFT+P did not write quote");
    dump_state(dut, "quote2");
    require(wait_idle(dut, 80000000ull), "debounce did not return after quotes");

    static const uint8_t kTape[16] = {
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x76, 0x80};
    dut.tape_ready = 1;
    dut.tape_size = 16;
    set_keys(dut, row_bits(6, 0x1e));
    bool hit_loader = false;
    uint64_t max_addr = 0;
    for (uint64_t cycle = 0; cycle != 250000000ull; ++cycle) {
        dut.tape_data = kTape[dut.tape_addr_out < 16 ? dut.tape_addr_out : 15];
        tick(dut);
        if (dut.cpu_addr == 0x0347)
            hit_loader = true;
        if (dut.tape_addr_out > max_addr)
            max_addr = dut.tape_addr_out;
        if (hit_loader && max_addr + 1 >= 16)
            break;
        if ((cycle & 0x1ffffffull) == 0 && cycle != 0)
            std::cout << " still waiting for LOAD at " << cycle
                      << " tape_addr=" << max_addr << " loader=" << hit_loader
                      << '\n';
    }
    dump_state(dut, "ENTER");
    require(hit_loader || max_addr > 0, "LOAD did not consume tape bytes");
    std::cout << "FES ZX81 machine simulation passed (tape_addr=" << max_addr
              << " loader=" << hit_loader << ")\n";
    return 0;
}
