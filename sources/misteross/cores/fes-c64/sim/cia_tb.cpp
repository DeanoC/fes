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

static void ports() {
    Bench b;
    b.write(0, 0x5a);
    b.write(1, 0xa5);
    b.write(2, 0xf0);
    b.write(3, 0x0f);
    b.expect("port A output latch", b.top.pa_reg, 0x5a);
    b.expect("port B output latch", b.top.pb_reg, 0xa5);
    b.expect("port A DDR", b.read(2), 0xf0);
    b.expect("port B DDR", b.read(3), 0x0f);
    b.top.pa_pin = 0x3c;
    b.top.pb_pin = 0xc3;
    b.expect("port A reads external resolved pins", b.read(0), 0x3c);
    b.expect("port B reads external resolved pins", b.read(1), 0xc3);
}

static void timer_a() {
    Bench b;
    b.write(4, 3);
    b.expect("TA low write changes only latch", b.count(4), 0xffff);
    b.write(5, 0);
    b.expect("TA stopped high write loads full counter", b.count(4), 3);
    b.write(14, 0x11);
    b.expect("CRA force load reads zero", b.read(14), 1);
    b.phi(5);
    b.expect("TA delayed force/start counter", b.count(4), 1);
    b.write(5, 1);
    b.expect("TA running high write leaves counter", b.count(4), 1);
    b.phi(2);
    b.expect("TA underflow reloads changed latch", b.count(4), 0x103);
    b.expect("TA unmasked underflow flag", b.read(13, true), 1);
    b.expect("TA unmasked underflow does not interrupt", b.top.irq, 0);
    b.phi();
    b.write(14, 0x11, true);
    b.expect("TA force write leaves current decrement visible", b.count(4), 0x101);
    b.phi(2);
    b.expect("TA delayed force load", b.count(4), 0x103);
    b.write(14, 0x21);
    b.phi(4);
    b.expect("TA CNT mode drains pending count then receives no edges", b.count(4), 0x102);
    b.write(14, 0);
    b.write(4, 0);
    b.write(5, 0);
    b.write(14, 9);
    b.phi(2);
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
    b.phi(4);
    b.expect("TB delayed force/start reads live counter", b.count(6), 1);
    b.write(7, 1);
    b.expect("TB running high write leaves counter", b.count(6), 1);
    b.write(13, 0x82);
    b.phi(2);
    b.expect("TB continuous reload", b.count(6), 0x102);
    b.expect("TB IRQ mask", b.top.irq, 1);
    b.expect("TB ICR flags remain before CPU sample", b.read(13), 0x82);
    b.expect("TB ICR read returns pending flags", b.read(13, true), 0x82);
    b.expect("TB ICR clears on CPU sample", b.top.irq, 0);
    b.expect("old CIA ICR retains bit7 after pin clears", b.read(13, true), 0x80);
    b.phi(2);
    b.expect("old CIA delayed ICR bit7 clear", b.read(13, true), 0);
    b.write(15, 0x11, true);
    b.expect("TB force write leaves current decrement visible", b.count(6), 0xff);
    b.phi(2);
    b.expect("TB delayed force load", b.count(6), 0x102);
    b.write(15, 0x21);
    b.phi(4);
    b.expect("TB CNT mode drains pending count then receives no edges", b.count(6), 0x101);
    b.write(15, 0);
    b.write(6, 0);
    b.write(7, 0);
    b.write(15, 9);
    b.phi(2);
    b.expect("TB zero one-shot sets source before IRQ", b.read(13), 2);
    b.expect("old CIA one-shot IRQ is delayed", b.top.irq, 0);
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
    b.phi(2);
    b.expect("TB chain waits for TA pipeline", b.count(6), 1);
    b.expect("TA has not underflowed during start delay", b.read(13), 0);
    b.phi();
    b.expect("TA underflow queues B step", b.count(6), 1);
    b.expect("TA source precedes old CIA IRQ", b.read(13), 1);
    b.expect("TA IRQ delay", b.top.irq, 0);
    b.phi();
    b.expect("TB queued step enters count pipeline", b.count(6), 1);
    b.expect("TA delayed IRQ", b.top.irq, 1);
    b.phi();
    b.expect("TB queued step decrements count", b.count(6), 0);
    b.expect("TA underflow flag", b.read(13), 0x81);
    b.phi();
    b.expect("TB next queued step reloads zero counter", b.count(6), 1);
    b.expect("both underflows retained", b.read(13, true), 0x83);
    b.expect("TB chain one-shot clears START", b.read(15), mode | 8);
    b.phi(2);
    b.expect("stopped TB ignores later TA underflows", b.count(6), 1);
    b.expect("only TA remains active", b.read(13, true), 0x81);
}

int main() {
    try {
        ports();
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
