// SPDX-License-Identifier: MIT
#include "Vntsc_timing_sim_top.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>

static void check(bool passed, const char *name, unsigned line, unsigned cycle, unsigned phase) {
    if (!passed) {
        std::fprintf(stderr, "6567R8 %s at raster %u cycle %u SYS phase %u\n", name, line, cycle, phase);
        std::exit(1);
    }
}

// Reference: VICE viciisc/vicii-chip-model.c, cycle_tab_ntsc at
// 08b38de806e309b7f0a2a27043c896cf7c259061. Both phases of cycle 65
// belong to sprite 3; the other two bytes of that row arrive on cycle 1.
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vntsc_timing_sim_top t;
    t.phase = 63;
    t.clk = t.we = t.phi = t.addr = t.din = 0;
    t.reset = 1;
    auto edge = [&]() { t.clk = 0; t.eval(); t.clk = 1; t.eval(); };
    edge();
    auto read_raster = [&]() {
        t.addr = 0x11; t.eval();
        const unsigned high = (t.dout & 0x80) << 1;
        t.addr = 0x12; t.eval();
        return high | t.dout;
    };
    check(read_raster() == 0, "guest reset raster", 0, 1, 63);
    t.reset = 0;
    auto write = [&](unsigned addr, unsigned value) {
        t.we = 1; t.addr = addr; t.din = value; edge(); t.we = 0;
    };
    write(0x11, 0x1b);  // First badline at $33.
    write(0x18, 0x14);  // Matrix at $0400.
    write(0x1a, 1);     // Observe raster interrupts on the CPU's IRQ line.
    for (unsigned sprite = 0; sprite < 8; ++sprite) write(2*sprite+1, 80);
    write(0x15, 0xff);

    constexpr unsigned frame_cycles = 263 * 65;
    unsigned phase = 0, accumulator = 0, tick = 0;
    unsigned fetches = 0, matrix_reads = 0, late_dots = 0, single_sprite_halfcycles = 0;
    while (tick < frame_cycles + 2) {
        const unsigned line = tick / 65 % 263, cycle = tick % 65 + 1;
        t.phase = phase;
        accumulator += 1022730;
        t.phi = accumulator >= 52224000;
        if (t.phi) accumulator -= 52224000;
        t.clk = 0; t.eval();
        check(t.raster == line && t.cycle == cycle, "beam progression", line, cycle, phase);
        if (line == 80 && (cycle == 62 || cycle == 63) && t.dot_tick) {
            check(t.sprite_x == 384 + t.dot, "repeated sprite X group", line, cycle, phase);
            ++late_dots;
        }

        // x64sc keeps $D011/$D012 at 262 through cycle 1 of the new frame.
        if (phase == 43 && tick >= frame_cycles - 1)
            check(read_raster() == (tick <= frame_cycles ? 262u : 0u),
                  "guest raster at frame wrap", line, cycle, phase);
        t.we = 0;
        if (line == 110 && cycle == 20 && phase == 26) {
            t.we = 1; t.addr = 0x15; t.din = 0x08;
        } else if (line == 110 && cycle == 21 && phase == 26) {
            t.we = 1; t.addr = 7; t.din = 120;  // Y for the isolated sprite 3.
        } else if (line == 262 && cycle == 63 && phase == 26) {
            t.we = 1; t.addr = 0x12; t.din = 1;
        } else if (tick == frame_cycles && phase == 26) {
            t.we = 1; t.addr = 0x12; t.din = 0;  // Compare 0 before guest raster 0.
        } else if (line == 262 && cycle == 64 && phase == 26) {
            t.we = 1; t.addr = 0x19; t.din = 1;
        }
        t.clk = 1; t.eval();
        if (tick == frame_cycles && phase == 26)
            check(!t.irq, "premature compare-write IRQ for raster 0", line, cycle, phase);
        if (phase == 0 || phase == 26) {
            // With only sprite 3 active its five BA cycles span the line wrap.
            if ((line == 120 && cycle >= 60) || (line == 121 && cycle <= 3)) {
                const bool owned = line == 120 ? cycle >= 62 : cycle == 1;
                check(bool(t.ba_n) == !owned, "isolated sprite 3 BA", line, cycle, phase);
                ++single_sprite_halfcycles;
            }
            if (line == 51) {
                check(bool(t.ba_n) == (cycle < 12 || cycle > 54), "badline BA", line, cycle, phase);
                const bool fetch = phase == 26 && cycle >= 15 && cycle <= 54;
                check(bool(t.matrix_fetch) == fetch, "matrix slot", line, cycle, phase);
                if (fetch) {
                    check(t.address == 0x400 + cycle - 15, "first matrix row", line, cycle, phase);
                    ++matrix_reads;
                }
            }
            if ((line == 80 && cycle >= 55) || (line == 81 && cycle <= 10)) {
                const unsigned relative = line == 80 ? cycle - 55 : 10 + cycle;
                check(bool(t.ba_n) == (relative == 0 || relative == 20), "sprite BA", line, cycle, phase);
                const bool fetch = relative >= 4 && relative < 20;
                check(bool(t.sprite_fetch) == fetch, "sprite slot", line, cycle, phase);
                if (fetch) {
                    const unsigned half = 2*(relative-4) + (phase == 26);
                    const unsigned sprite = half / 4, lane = half % 4;
                    const unsigned expected = lane ? 0x3000 + 64*sprite + lane - 1 : 0x7f8 + sprite;
                    check(t.address == expected, "sprite pointer/data address", line, cycle, phase);
                    ++fetches;
                }
            }
        }
        if (t.phi) {
            ++tick;
            if (tick >= frame_cycles - 1)
                check(bool(t.irq) == (tick > frame_cycles), "line-zero IRQ", line, cycle, phase);
            phase = 0;
        } else {
            ++phase;
        }
    }
    check(matrix_reads == 40 && fetches == 32 && late_dots == 16 && single_sprite_halfcycles == 18, "coverage", 0, 0, 0);
    // For the opposite compare-write case, arm low byte 6 first. Raising the
    // high bit at line 0 cycle 1 must still match guest raster $106.
    t.phi = t.we = 0; t.phase = 63; t.reset = 1; edge(); t.reset = 0;
    write(0x12, 6);
    write(0x1a, 1);
    t.phi = 1;
    for (unsigned n = 0; n < frame_cycles - 1; ++n) edge();
    t.phi = 0; write(0x19, 1);  // Clear the IRQ previously raised on line 6.
    t.phi = 1; edge(); t.phi = 0;
    check(read_raster() == 262 && !t.irq, "armed last-line compare", 0, 1, 63);
    write(0x11, 0x9b);
    check(t.irq, "compare write must match raster 262 on cycle 1", 0, 1, 63);
    std::puts("6567R8 PASS: 263 x 65 raster, guest readback and compare writes across frame wrap, "
              "delayed line-zero IRQ, 40 matrix reads, "
              "32 sprite transfers with BA across wrap, repeated X group on cycles 62/63");
}
