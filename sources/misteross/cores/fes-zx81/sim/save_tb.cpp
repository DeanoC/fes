// SPDX-License-Identifier: GPL-2.0-or-later
// ROM SAVE of a typed line must produce the .p the emitter reads from $4009
// through E_LINE-1, and LOAD of that image must restore the same line.
#include "Vzx81_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES ZX81 SAVE: " << message << '\n';
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

uint16_t word_at(Vzx81_machine &dut, uint16_t addr) {
    return uint16_t(peek(dut, addr)) | (uint16_t(peek(dut, addr + 1)) << 8);
}

bool basic_ready(Vzx81_machine &dut) {
    const uint16_t dfile = word_at(dut, 0x400c);
    return peek(dut, 0x4000) == 0 && dfile >= 0x4070 && dfile < 0x8000 &&
           peek(dut, dfile) == 0x76;
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

// Address-bit rows, bit 0 on the left: SHIFT Z X C V / A S D F G / Q W E R T /
// 1 2 3 4 5 / 0 9 8 7 6 / P O I U Y / NEWLINE L K J H / SPACE . M N B.
uint64_t key(unsigned row, unsigned col) {
    return row_bits(row, uint8_t(0x1f & ~(1u << col)));
}

unsigned eline_count(Vzx81_machine &dut, uint8_t token) {
    unsigned n = 0;
    const uint16_t eline = word_at(dut, 0x4014);
    for (uint16_t a = eline; a < eline + 20 && a < 0x8000; ++a)
        if (peek(dut, a) == token)
            n++;
    return n;
}

bool wait_idle(Vzx81_machine &dut, uint64_t cycles) {
    all_up(dut);
    for (uint64_t i = 0; i != cycles; ++i) {
        tick(dut);
        if (dut.cpu_addr == 0x04cf && word_at(dut, 0x4025) == 0xffff &&
            peek(dut, 0x4027) == 0)
            return true;
    }
    return dut.cpu_addr == 0x04cf && word_at(dut, 0x4025) == 0xffff &&
           peek(dut, 0x4027) == 0;
}

bool type_until(Vzx81_machine &dut, uint64_t keys, uint8_t token, unsigned need,
                uint64_t cycles) {
    dut.keyboard = keys;
    for (uint64_t i = 0; i != cycles; ++i) {
        tick(dut);
        if ((i & 0xffff) == 0 && eline_count(dut, token) >= need)
            return true;
    }
    return eline_count(dut, token) >= need;
}

void type_key(Vzx81_machine &dut, uint64_t keys, uint8_t token, const char *name,
              unsigned need = 1) {
    require(type_until(dut, keys, token, need, 10000000ull),
            std::string(name) + " did not reach E_LINE");
    require(wait_idle(dut, 80000000ull), std::string(name) + " did not release");
}

bool has_line(Vzx81_machine &dut) {
    // 10 REM A — line number big-endian, length includes the 0x76 newline.
    static const uint8_t line[] = {0x00, 0x0a, 0x03, 0x00, 0xea, 0x26, 0x76};
    const uint16_t dfile = word_at(dut, 0x400c);
    if (dfile < 0x407d || dfile >= 0x8000)
        return false;
    for (uint16_t a = 0x4009; a + sizeof(line) <= dfile; ++a) {
        bool match = true;
        for (unsigned i = 0; i != sizeof(line); ++i)
            if (peek(dut, a + i) != line[i]) {
                match = false;
                break;
            }
        if (match)
            return true;
    }
    return false;
}

void boot(Vzx81_machine &dut) {
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.keyboard = 0xffffffffffull;
    dut.tape_ready = 0;
    dut.tape_size = 0;
    dut.tape_data = 0;
    dut.save_addr = 0;
    dut.peek_addr = 0x400c;
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
    bool saw_basic = false;
    for (uint64_t cycle = 0; cycle != 40000000ull; ++cycle) {
        tick(dut);
        if ((cycle & 0x1ffffull) == 0 && basic_ready(dut)) {
            saw_basic = true;
            break;
        }
    }
    require(saw_basic, "NEW did not create a BASIC display file");
    require(wait_idle(dut, 400000000ull), "SLOW-DISP never waited for a key");
}

std::vector<uint8_t> snapshot(Vzx81_machine &dut) {
    const uint16_t eline = word_at(dut, 0x4014);
    require(eline > 0x4009 && eline <= 0x8000, "E_LINE is outside the .p window");
    const unsigned count = unsigned(eline - 0x4009);
    require(count <= 16384u, "SAVE image is larger than 16 KiB");
    std::vector<uint8_t> bytes(count);
    for (unsigned i = 0; i != count; ++i)
        bytes[i] = peek(dut, uint16_t(0x4009 + i));
    return bytes;
}

bool contains_line(const std::vector<uint8_t> &bytes) {
    static const uint8_t line[] = {0x00, 0x0a, 0x03, 0x00, 0xea, 0x26, 0x76};
    if (bytes.size() < sizeof(line))
        return false;
    for (size_t i = 0; i + sizeof(line) <= bytes.size(); ++i) {
        bool match = true;
        for (unsigned n = 0; n != sizeof(line); ++n)
            if (bytes[i + n] != line[n]) {
                match = false;
                break;
            }
        if (match)
            return true;
    }
    return false;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vzx81_machine dut;
    boot(dut);
    std::cout << "FES ZX81 SAVE: BASIC ready\n";

    // K-mode digits, then REM, then the letter A. ENTER stores 10 REM A.
    type_key(dut, key(3, 0), 0x1d, "1");
    type_key(dut, key(4, 0), 0x1c, "0");
    type_key(dut, key(2, 2), 0xea, "REM");
    type_key(dut, key(1, 0), 0x26, "A");
    dut.keyboard = key(6, 0);
    bool stored = false;
    for (uint64_t i = 0; i != 80000000ull; ++i) {
        tick(dut);
        if ((i & 0xffff) == 0 && has_line(dut)) {
            stored = true;
            break;
        }
    }
    require(stored, "ENTER did not store 10 REM A");
    require(wait_idle(dut, 80000000ull), "BASIC did not return after the line");
    std::cout << "FES ZX81 SAVE: program stored\n";

    // Fresh line, K mode: S is SAVE. Quote is SHIFT+P.
    type_key(dut, key(1, 1), 0xf8, "SAVE");
    const uint64_t quote = key(0, 0) & key(5, 0);
    type_key(dut, quote, 0x0b, "quote");
    type_key(dut, key(1, 0), 0x26, "name");
    type_key(dut, quote, 0x0b, "closing quote", 2);

    const uint16_t edit = word_at(dut, 0x4014);
    std::cout << "FES ZX81 SAVE: edit";
    for (unsigned i = 0; i != 12; ++i)
        std::cout << ' ' << std::hex << int(peek(dut, edit + i));
    std::cout << std::dec << '\n';
    dut.keyboard = key(6, 0);
    std::vector<uint8_t> expected;
    bool saw_busy = false;
    bool saw_save_pc = false;
    for (uint64_t i = 0; i != 200000000ull && !dut.save_ready; ++i) {
        tick(dut);
        if (dut.cpu_addr >= 0x02f0 && dut.cpu_addr <= 0x0340)
            saw_save_pc = true;
        if (!saw_busy && dut.save_busy) {
            saw_busy = true;
            expected = snapshot(dut);
            std::cout << "FES ZX81 SAVE: snapshot " << expected.size() << " bytes\n";
        }
        if ((i & 0x1ffffffull) == 0 && i != 0)
            std::cout << " still saving at " << i << " busy=" << dut.save_busy
                      << " savepc=" << saw_save_pc << " pc=" << std::hex
                      << dut.cpu_addr << std::dec << '\n';
    }
    if (!saw_busy)
        std::cout << "FES ZX81 SAVE: never saw SAVE ROM, savepc=" << saw_save_pc
                  << '\n';
    require(saw_busy, "SAVE never started the program image");
    require(dut.save_ready, "SAVE did not publish a .p");
    require(contains_line(expected), "snapshot lost 10 REM A");
    require(dut.save_size == expected.size(), "save_size does not match E_LINE-$4009");
    for (unsigned i = 0; i != expected.size(); ++i) {
        dut.save_addr = i;
        tick(dut);
        if (uint8_t(dut.save_q) != expected[i]) {
            std::cerr << " byte " << i << " captured " << std::hex
                      << int(dut.save_q) << " ram " << int(expected[i]) << std::dec
                      << '\n';
            fail("captured .p differs from the RAM the emitter reads");
        }
    }
    require(dut.save_changed == 0, "save_changed stayed high after the pulse");
    std::cout << "FES ZX81 SAVE: captured " << expected.size() << " bytes\n";

    // Cold start, then LOAD "" of the captured tape. The line must come back.
    boot(dut);
    const std::vector<uint8_t> tape = expected;
    dut.tape_ready = 1;
    dut.tape_size = tape.size();
    type_key(dut, key(6, 3), 0xef, "LOAD");
    type_key(dut, quote, 0x0b, "LOAD quote");
    type_key(dut, quote, 0x0b, "LOAD closing quote", 2);
    dut.keyboard = key(6, 0);
    bool loaded = false;
    for (uint64_t i = 0; i != 250000000ull; ++i) {
        dut.tape_data = dut.tape_addr_out < tape.size() ? tape[dut.tape_addr_out] : 0;
        tick(dut);
        if ((i & 0xffff) == 0 && !dut.tape_busy && has_line(dut)) {
            loaded = true;
            break;
        }
    }
    require(loaded, "LOAD of the captured .p did not restore 10 REM A");
    std::cout << "FES ZX81 SAVE simulation passed\n";
    return 0;
}
