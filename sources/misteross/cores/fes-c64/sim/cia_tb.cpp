// SPDX-License-Identifier: MIT
// MOS 6526 register semantics, driven through the same CIA pins as the CPU.
#include "Vc64_cia.h"
#include "verilated.h"

#include <cstdio>
#include <stdexcept>

struct Bench {
    Vc64_cia top;

    Bench() {
        top.clk = 0;
        top.phi = 0;
        top.reset = 1;
        top.cs = 0;
        top.we = 0;
        top.reading = 0;
        top.read_sample = 0;
        top.rs = 0;
        top.din = 0;
        top.pa_pin = 0xff;
        top.pb_pin = 0xff;
        tick();
        top.reset = 0;
    }

    void tick() {
        top.clk = 0;
        top.eval();
        top.clk = 1;
        top.eval();
    }

    void phi(int clocks = 1) {
        for (int i = 0; i < clocks; ++i) {
            top.phi = 1;
            tick();
            top.phi = 0;
            tick();
        }
    }

    void write(int reg, int data, bool with_phi = false) {
        top.cs = 1;
        top.we = 1;
        top.rs = reg;
        top.din = data;
        top.phi = with_phi;
        tick();
        top.phi = 0;
        top.we = 0;
        top.cs = 0;
        tick();
    }

    int read(int reg, bool acknowledge = false) {
        top.cs = 1;
        top.rs = reg;
        top.reading = 1;
        top.read_sample = 0;
        top.eval();
        int result = top.dout;
        if (acknowledge) {
            top.read_sample = 1;
            tick();
        }
        top.read_sample = 0;
        top.reading = 0;
        top.cs = 0;
        top.eval();
        return result;
    }

    int count(int low_reg) { return read(low_reg) | (read(low_reg + 1) << 8); }

    void expect(const char* what, int actual, int expected) {
        if (actual == expected) return;
        std::fprintf(stderr, "%s: got %04x, expected %04x\n", what, actual, expected);
        throw std::runtime_error(what);
    }
};

static void timer_a() {
    Bench b;
    b.write(4, 3);
    b.expect("TA low write changes only latch", b.count(4), 0xffff);
    b.write(5, 0);
    b.expect("TA stopped high write loads full counter", b.count(4), 3);
    b.write(14, 0x11);
    b.expect("CRA force load reads zero", b.read(14), 1);
    b.phi(2);
    b.expect("TA phi counter", b.count(4), 1);
    b.write(5, 1);
    b.expect("TA running high write leaves counter", b.count(4), 1);
    b.phi(2);
    b.expect("TA underflow reloads changed latch", b.count(4), 0x103);
    b.expect("TA unmasked underflow flag", b.read(13, true), 1);
    b.expect("TA unmasked underflow does not interrupt", b.top.irq, 0);
    b.phi();
    b.write(14, 0x11, true);
    b.expect("TA force load takes priority over counting", b.count(4), 0x103);
    b.write(14, 0x21);
    b.phi(4);
    b.expect("TA CNT mode receives no external edges", b.count(4), 0x103);
    b.write(14, 0);
    b.write(4, 0);
    b.write(5, 0);
    b.write(14, 9);
    b.phi();
    b.expect("TA zero one-shot underflow", b.read(13, true), 1);
    b.expect("TA one-shot clears START", b.read(14), 8);
    b.phi(3);
    b.expect("TA one-shot remains stopped", b.read(13, true), 0);
}

static void timer_b() {
    Bench b;
    b.write(6, 2);
    b.expect("TB low write changes only latch", b.count(6), 0xffff);
    b.write(7, 0);
    b.expect("TB stopped high write loads full counter", b.count(6), 2);
    b.write(15, 0x11);
    b.expect("CRB force load reads zero", b.read(15), 1);
    b.phi();
    b.expect("TB reads live counter", b.count(6), 1);
    b.write(7, 1);
    b.expect("TB running high write leaves counter", b.count(6), 1);
    b.write(13, 0x82);
    b.phi(2);
    b.expect("TB continuous reload", b.count(6), 0x102);
    b.expect("TB IRQ mask", b.top.irq, 1);
    b.expect("TB ICR flags remain before CPU sample", b.read(13), 0x82);
    b.expect("TB ICR read returns pending flags", b.read(13, true), 0x82);
    b.expect("TB ICR clears on CPU sample", b.top.irq, 0);
    b.expect("TB ICR second read", b.read(13, true), 0);
    b.write(15, 0x11, true);
    b.expect("TB force load takes priority over counting", b.count(6), 0x102);
    b.write(15, 0x21);
    b.phi(4);
    b.expect("TB CNT mode receives no external edges", b.count(6), 0x102);
    b.write(15, 0);
    b.write(6, 0);
    b.write(7, 0);
    b.write(15, 9);
    b.phi();
    b.expect("TB zero one-shot underflow", b.read(13, true), 0x82);
    b.expect("TB one-shot clears START", b.read(15), 8);
    b.phi(3);
    b.expect("TB one-shot remains stopped", b.read(13, true), 0);
}

static void linked_timers(int mode) {
    Bench b;
    b.write(4, 1);
    b.write(5, 0);
    b.write(6, 1);
    b.write(7, 0);
    b.write(14, 1);
    b.write(15, mode | 9);
    b.write(13, 0x83);
    b.phi();
    b.expect("TB chain waits for TA underflow", b.count(6), 1);
    b.phi();
    b.expect("TA underflow clocks TB", b.count(6), 0);
    b.expect("TA underflow flag", b.read(13, true), 0x81);
    b.phi();
    b.expect("TB chain skips ordinary phi", b.count(6), 0);
    b.phi();
    b.expect("TB chain reloads on underflow", b.count(6), 1);
    b.expect("both underflows retained", b.read(13, true), 0x83);
    b.expect("TB chain one-shot clears START", b.read(15), mode | 8);
    b.phi(2);
    b.expect("stopped TB ignores later TA underflows", b.count(6), 1);
    b.expect("only TA remains active", b.read(13, true), 0x81);
}

int main() {
    try {
        timer_a();
        timer_b();
        linked_timers(0x40);
        linked_timers(0x60); // The unconnected CNT pin is held high.
    } catch (const std::exception&) {
        return 1;
    }
    std::puts("c64 CIA timer and interrupt register tests passed");
    return 0;
}
