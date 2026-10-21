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
        top.tod_tick = 0;
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

// Independent register-level TOD expectations. No private RTL state is read.
static void time_of_day() {
    Bench b;
    auto pulses = [&](int n) {
        for (int i=0;i<n;++i) {
            b.top.tod_tick=1; b.top.phi=1; b.tick();
            b.top.tod_tick=0; b.top.phi=0; b.tick();
        }
    };
    auto set = [&](int hour,int minute,int second,int tenth) {
        b.write(11,hour); b.write(10,minute); b.write(9,second); b.write(8,tenth);
    };
    b.expect("TOD reset hour",b.read(11),1);
    pulses(60); b.expect("TOD reset stopped",b.read(8),0);
    b.write(14,0x80); // Divide the 50 Hz input by five.
    set(1,0,0,0);
    pulses(4); b.expect("50 Hz before fifth input",b.read(8),0);
    pulses(1); b.expect("50 Hz fifth input",b.read(8),1);
    b.write(14,0); // Same pulse input, divide by six.
    pulses(5); b.expect("60 Hz before sixth input",b.read(8),1);
    pulses(1); b.expect("60 Hz sixth input",b.read(8),2);
    b.write(14,0x80);
    set(1,0,0,0); pulses(250);
    b.expect("PAL five-second game wait",b.read(9),5);
    set(0x11,0x59,0x59,9); pulses(5);
    b.expect("noon toggles PM",b.read(11),0x92);
    b.expect("minute BCD carry",b.read(10),0);
    b.expect("second BCD carry",b.read(9),0);
    b.expect("tenth BCD carry",b.read(8),0);
    set(0x91,0x59,0x59,9); pulses(5);
    b.expect("midnight toggles AM",b.read(11),0x12);
    set(0x12,0x59,0x59,9); pulses(5);
    b.expect("12 write flips PM and wraps to one",b.read(11),0x81);
    set(0x09,0x59,0x59,9); pulses(5);
    b.expect("nine to ten BCD",b.read(11),0x10);
    set(1,0,0,9);
    b.expect("hour read latches",b.read(11,true),1);
    pulses(5);
    b.expect("latched seconds remain old",b.read(9,true),0);
    b.expect("repeated hour read retains latch",b.read(11,true),1);
    b.expect("tenths read returns latch then releases",b.read(8,true),9);
    b.expect("live seconds after release",b.read(9,true),1);
    set(1,0,0,0); pulses(4); b.write(11,2); pulses(50);
    b.expect("hour write stops clock",b.read(8),0);
    b.write(8,0); pulses(4);
    b.expect("restart clears prescaler",b.read(8),0);
    pulses(1); b.expect("restart fifth pulse",b.read(8),1);
    b.write(15,0x80); // Alarm writes neither stop nor restart the clock.
    b.write(11,1); b.write(10,0); b.write(9,1); b.write(8,0);
    b.write(15,0); set(1,0,0,9);
    b.write(13,0x84); pulses(5); b.phi(2);
    b.expect("TOD alarm flag and IRQ",b.read(13,true),0x84);
    b.expect("TOD IRQ read clears pin",b.top.irq,0);
    b.phi(3); b.expect("TOD alarm not level retriggered",b.read(13,true),0);
    // The alarm remains readable only through the clock bank; masked writes
    // use the same BCD masks as the chip, including AM/PM on time-only writes.
    b.write(15,0x80); b.write(11,0x12); b.write(15,0);
    b.expect("alarm hour does not change clock",b.read(11),1);
    b.write(11,0xff); b.expect("hour write masks bits 6/5",b.read(11),0x9f);
    b.write(10,0xff); b.expect("minute mask",b.read(10),0x7f);
    b.write(9,0xff); b.expect("second mask",b.read(9),0x7f);
    b.write(8,0xff); b.expect("tenth mask",b.read(8),0x0f);
}

int main() {
    try {
        time_of_day();
        ports();
        timer_a();
        timer_b();
        linked_timers(0x40);
        linked_timers(0x60); // The unconnected CNT pin is held high.
    } catch (const std::exception&) {
        return 1;
    }
    std::puts("c64 CIA TOD, timer and interrupt register tests passed");
    return 0;
}
