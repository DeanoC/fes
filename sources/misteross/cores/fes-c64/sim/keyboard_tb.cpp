// SPDX-License-Identifier: MIT
#include "Vc64_keyboard.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>

static const unsigned keys[8][8] = {
    {0x2a,0x28,0x4f,0x40,0x3a,0x3c,0x3e,0x51},
    {0x20,0x1a,0x04,0x21,0x1d,0x16,0x08,0xe1},
    {0x22,0x15,0x07,0x23,0x06,0x09,0x17,0x1b},
    {0x24,0x1c,0x0a,0x25,0x05,0x0b,0x18,0x19},
    {0x26,0x0c,0x0d,0x27,0x10,0x0e,0x12,0x11},
    {0x57,0x13,0x0f,0x2d,0x37,0x34,0x2f,0x36},
    {0x31,0x30,0x33,0x4a,0xe5,0x2e,0x4b,0x38},
    {0x1e,0x35,0xe0,0x1f,0x2c,0xe2,0x14,0x29}
};
static void key(Vc64_keyboard &t, unsigned usage) {
    unsigned bit = usage >= 0xe0 ? 128 + usage - 0xe0 : usage;
    t.rows[bit / 32] |= 1u << (bit % 32);
}
static void clear(Vc64_keyboard &t) { for (int i=0; i<5; ++i) t.rows[i]=0; }
static void check(Vc64_keyboard &t, unsigned columns, unsigned expected) {
    t.columns_low = columns; t.eval();
    if (t.rows_low != expected) {
        std::printf("keyboard columns=%02x expected PB=%02x got=%02x\n", columns, expected, t.rows_low);
        std::exit(1);
    }
}
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vc64_keyboard t;
    // Check every native switch against every PA scan column.
    for (unsigned c=0; c<8; ++c) for (unsigned r=0; r<8; ++r) {
        clear(t); key(t, keys[c][r]);
        for (unsigned scan=0; scan<8; ++scan) {
            unsigned expected = scan == c ? 1u << r : 0;
            check(t, 1u << scan, expected);
        }
        check(t, 0, 0);
    }
    clear(t); key(t,0x04); key(t,0x0f); // A and L, separate columns
    check(t,0x22,0x04); check(t,0xff,0x04); check(t,0x01,0);
    clear(t); key(t,0x50); // Left is Shift+Right
    check(t,0x01,0x04); check(t,0x02,0x80);
    clear(t); key(t,0x3b); // F2 is Shift+F1
    check(t,0x01,0x10); check(t,0x02,0x80);
    clear(t); key(t,0xe4); key(t,0xe6);
    check(t,0x80,0x24);
    clear(t); check(t,0xff,0);
    std::puts("c64 keyboard: all 64 native switches, scan isolation and aliases passed");
}
