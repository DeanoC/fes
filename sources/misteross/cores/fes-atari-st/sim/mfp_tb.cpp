// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vst_mfp.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

// These tests use the Motorola register contract, not the internal RTL state.
class Simulation {
public:
    Vst_mfp dut;
    uint64_t cycles = 0;
    unsigned assertions = 0;

    void expect(unsigned actual, unsigned expected, const char *what) {
        ++assertions;
        if (actual != expected) {
            std::cerr << what << " at cycle " << cycles << ": got 0x"
                      << std::hex << actual << " expected 0x" << expected << '\n';
            std::exit(1);
        }
    }

    void step(bool timer = false) {
        dut.timer_ce = timer;
        dut.clk = 0;
        dut.eval();
        dut.clk = 1;
        dut.eval();
        ++cycles;
    }

    void clocks(unsigned count) {
        for (unsigned i = 0; i < count; ++i) step(true);
    }

    void reset() {
        dut.req = 0;
        dut.iack = 0;
        dut.write = 0;
        dut.addr = 0;
        dut.wdata = 0;
        dut.gpip = 0xff;
        dut.timer_a = 1;
        dut.timer_b = 1;
        dut.reset = 1;
        step();
        dut.reset = 0;
        step();
        expect(dut.ack, 0, "reset acknowledge");
        expect(dut.irq, 0, "reset interrupt");
    }

    unsigned access(unsigned address, bool writing, unsigned value = 0, unsigned held = 3) {
        dut.addr = address;
        dut.write = writing;
        dut.wdata = value;
        dut.req = 1;
        step();
        expect(dut.ack, 1, "register acknowledge");
        const unsigned result = dut.rdata;
        for (unsigned i = 1; i < held; ++i) {
            step();
            expect(dut.ack, 1, "stretched register acknowledge");
            expect(dut.rdata, result, "latched register read");
        }
        dut.req = 0;
        step();
        expect(dut.ack, 0, "register acknowledge release");
        return result;
    }

    unsigned read(unsigned address) { return access(address, false); }
    void write(unsigned address, unsigned value) { access(address, true, value); }

    unsigned acknowledge(unsigned held = 3) {
        expect(dut.irq, 1, "interrupt before acknowledge");
        const unsigned vector = dut.irq_vector;
        dut.iack = 1;
        step();
        expect(dut.irq_vector, vector, "acknowledged vector");
        for (unsigned i = 1; i < held; ++i) {
            step();
            expect(dut.irq_vector, vector, "stretched acknowledge vector");
        }
        dut.iack = 0;
        step();
        return vector;
    }

    void enable(unsigned a, unsigned b, bool software_eoi = false) {
        write(0x17, software_eoi ? 0x48 : 0x40);
        write(0x07, a);
        write(0x09, b);
        write(0x13, a);
        write(0x15, b);
    }

    void gpio() {
        reset();
        for (unsigned a = 3; a <= 0x2f; a += 2)
            expect(read(a), 0, "reset register contents");
        expect(read(1), 0xff, "GPIP external pins");
        dut.gpip = 0xdf;
        step();
        write(1, 0x5a);
        write(5, 0xf0);
        expect(read(1), 0x5f, "GPIP mixes output latch and input pins");
        write(1, 0xa5);
        expect(read(1), 0xaf, "GPIP output latch write");
        write(0x17, 0xff);
        expect(read(0x17), 0xf8, "VR reserved bits read zero");
        write(0x1d, 0xff);
        expect(read(0x1d), 0x77, "timer C/D reserved bits read zero");
        write(0x1d, 0);
        expect(read(2), 0, "unmapped even address");
        write(2, 0xff);
        expect(read(3), 0, "even write does not alias AER");

        reset();
        enable(0xff, 0xff);
        dut.gpip = 0;
        step();
        expect(read(0x0b), 0xc0, "GPIP6/7 interrupt mapping");
        expect(read(0x0d), 0xcf, "GPIP0-5 interrupt mapping");
        const unsigned priority[] = {0x4f, 0x4e, 0x47, 0x46, 0x43, 0x42, 0x41, 0x40};
        for (unsigned vector : priority)
            expect(acknowledge(7), vector, "GPIP descending priority and single IACK");
        expect(dut.irq, 0, "all GPIP interrupts consumed");
        expect(read(0x0f), 0, "automatic EOI ISR A");
        expect(read(0x11), 0, "automatic EOI ISR B");

        reset();
        enable(0, 0x04);
        write(0x15, 0);
        dut.gpip = 0xfb;
        step();
        expect(read(0x0d), 4, "masked source still sets pending");
        expect(dut.irq, 0, "mask suppresses interrupt request");
        write(0x15, 4);
        expect(dut.irq, 1, "unmask releases pending interrupt");
        write(0x0d, 0xff);
        expect(read(0x0d), 4, "ones cannot set or clear pending");
        write(0x09, 0);
        expect(read(0x0d), 0, "disable clears pending");
        dut.gpip = 0xff;
        step();
        dut.gpip = 0xfb;
        step();
        expect(read(0x0d), 0, "disabled source ignores edges");

        reset();
        enable(0, 0x04);
        write(3, 4); // AER is an XOR input: changing it can itself create an edge.
        expect(read(0x0d), 4, "AER transition can set pending");
        write(0x0d, 0xfb);
        dut.gpip = 0xfb;
        step();
        expect(dut.irq, 0, "rising AER rejects falling pin edge");
        dut.gpip = 0xff;
        step();
        expect(acknowledge(), 0x42, "rising AER accepts rising pin edge");
        write(5, 4);
        dut.gpip = 0xfb;
        step();
        dut.gpip = 0xff;
        step();
        expect(dut.irq, 0, "DDR output suppresses external pin interrupt");
    }

    void software_eoi() {
        reset();
        enable(0x80, 0x88, true);
        dut.gpip = 0xd7; // GPIP5 and GPIP3 fall, pending channels 7 and 3.
        step();
        expect(acknowledge(8), 0x47, "software EOI priority");
        expect(read(0x11), 0x80, "software EOI marks serviced channel");
        expect(read(0x0d), 8, "stretched IACK preserves lower pending source");
        expect(dut.irq, 0, "in-service channel blocks lower priority");
        dut.gpip = 0x57; // GPIP7 is higher than serviced GPIP5.
        step();
        expect(acknowledge(), 0x4f, "higher priority can nest");
        expect(read(0x0f), 0x80, "nested ISR A");
        write(0x07, 0);
        expect(read(0x0f), 0x80, "disabling source preserves in-service bit");
        write(0x0f, 0xff);
        expect(read(0x0f), 0x80, "ones preserve in-service bits");
        write(0x0f, 0x7f);
        expect(dut.irq, 0, "remaining lower ISR still blocks");
        dut.gpip = 0x77;
        step();
        dut.gpip = 0x57;
        step();
        expect(read(0x0d), 0x88, "in-service channel can become pending again");
        write(0x11, 0x7f);
        expect(acknowledge(), 0x47, "EOI releases same-channel recurrence");
        write(0x17, 0x40);
        expect(read(0x11), 0, "automatic EOI clears prior software ISR");
        expect(acknowledge(), 0x43, "automatic EOI releases lower pending channel");

        reset();
        enable(0, 1);
        dut.req = 1;
        dut.write = 1;
        dut.addr = 0x0d;
        dut.wdata = 0;
        dut.gpip = 0xfe;
        step();
        dut.req = 0;
        step();
        expect(read(0x0d), 1, "new edge survives simultaneous pending clear");
    }

    void delay_timers() {
        const unsigned divisors[] = {0, 4, 10, 16, 50, 64, 100, 200};
        for (unsigned mode = 1; mode <= 7; ++mode) {
            reset();
            enable(0x20, 0);
            write(0x1f, 2);
            write(0x19, mode);
            clocks(divisors[mode] - 1);
            expect(read(0x1f), 2, "prescaler has not expired");
            clocks(1);
            expect(read(0x1f), 1, "prescaler decrements counter");
            clocks(divisors[mode] - 1);
            expect(dut.irq, 0, "timer has not expired early");
            clocks(1);
            expect(read(0x1f), 2, "timer reloads on count through one");
            expect(acknowledge(), 0x4d, "timer A vector");
        }

        reset();
        enable(0x20, 0);
        write(0x1f, 2);
        write(0x19, 1);
        clocks(4);
        write(0x1f, 3);
        expect(read(0x1f), 1, "running data write preserves current countdown");
        clocks(4);
        expect(read(0x1f), 3, "running data write changes next reload");
        acknowledge();
        clocks(4);
        write(0x19, 0);
        clocks(100);
        expect(read(0x1f), 2, "stop preserves counter");
        write(0x19, 1);
        clocks(3);
        expect(read(0x1f), 2, "restart resets residual prescaler");
        clocks(1);
        expect(read(0x1f), 1, "restart resumes previous counter");

        reset();
        enable(0, 0x10);
        write(0x25, 0);
        write(0x1d, 1);
        clocks(4 * 255);
        expect(read(0x25), 1, "zero reload means 256 decrements");
        expect(dut.irq, 0, "zero reload does not expire after 255");
        clocks(4);
        expect(acknowledge(), 0x44, "timer D vector");
        expect(read(0x25), 0, "zero timer reload preserved");

        reset();
        enable(0, 0x20, true);
        write(0x23, 192);
        write(0x1d, 0x50);
        // Exact production ratio: 52.224 MHz / 2.4576 MHz = 21.25.
        unsigned phase = 0;
        for (unsigned period = 0; period < 2; ++period) {
            for (unsigned cycle = 1; cycle <= 261120; ++cycle) {
                phase += 2457600;
                const bool ce = phase >= 52224000;
                if (ce) phase -= 52224000;
                step(ce);
                if (cycle != 261120) expect(dut.irq, 0, "200 Hz timer C period");
            }
            expect(acknowledge(), 0x45, "EmuTOS 200 Hz vector");
            expect(read(0x11), 0x20, "EmuTOS software EOI channel");
            write(0x11, 0xdf);
        }

        reset();
        write(0x1f, 20);
        write(0x19, 1);
        dut.req = 1;
        dut.write = 0;
        dut.addr = 0x1f;
        step();
        expect(dut.rdata, 20, "timer read snapshot");
        clocks(12);
        expect(dut.rdata, 20, "stretched timer read remains captured");
        dut.req = 0;
        step();
        expect(read(0x1f), 17, "next timer read sees current countdown");

        reset();
        enable(0x20, 0);
        write(0x1f, 2);
        write(0x19, 1);
        dut.req = 1;
        dut.write = 1;
        dut.addr = 0x1f;
        dut.wdata = 3;
        step();
        dut.wdata = 7; // A held request must not overwrite the reload again.
        clocks(8);
        expect(dut.irq, 1, "timer expires with held data-write request");
        dut.req = 0;
        step();
        expect(read(0x1f), 3, "data register write happens once per request");
    }

    void event_and_pulse_timers() {
        reset();
        enable(1, 0);
        write(0x21, 2);
        write(0x1b, 8);
        clocks(100);
        expect(read(0x21), 2, "event timer ignores XTAL clocks");
        dut.timer_b = 0;
        step();
        expect(read(0x21), 1, "event timer counts falling TBI");
        clocks(100);
        expect(dut.irq, 0, "held event input counts once");
        dut.timer_b = 1;
        step();
        dut.timer_b = 0;
        step();
        expect(acknowledge(), 0x48, "timer B event vector");

        reset();
        enable(0x20, 0);
        write(3, 0x10);
        dut.timer_a = 0;
        step();
        write(0x1f, 1);
        write(0x19, 8);
        dut.timer_a = 1;
        step();
        expect(acknowledge(), 0x4d, "AER4 selects rising TAI event");

        reset();
        enable(0x20, 0x40);
        write(0x1f, 20);
        write(0x19, 9); // Active-low pulse width, prescaler 4.
        clocks(20);
        expect(read(0x1f), 20, "inactive pulse timer is stopped");
        dut.gpip = 0xef;
        step();
        expect(dut.irq, 0, "pulse timer replaces GPIP4 interrupt source");
        dut.timer_a = 0;
        step();
        clocks(12);
        expect(read(0x1f), 17, "pulse timer measures active width");
        dut.timer_a = 1;
        step();
        expect(acknowledge(), 0x46, "pulse termination uses GPIP4 channel");
        clocks(100);
        expect(read(0x1f), 17, "pulse timer stops when pulse ends");
        write(0x1f, 15);
        expect(read(0x1f), 15, "inactive pulse timer accepts counter reload");
        dut.timer_a = 0;
        step();
        clocks(60);
        expect(acknowledge(), 0x4d, "pulse timer timeout uses timer A channel");

        reset();
        enable(1, 8);
        write(0x21, 10);
        write(0x1b, 9);
        dut.timer_b = 0;
        step();
        clocks(8);
        expect(read(0x21), 8, "timer B pulse countdown");
        dut.timer_b = 1;
        step();
        expect(acknowledge(), 0x43, "pulse timer B termination uses GPIP3 channel");
    }

    void uart_and_reset() {
        reset();
        enable(0x16, 0);
        write(0x27, 0x55);
        expect(read(0x27), 0x55, "synchronous character register");
        write(0x29, 0x88); // Divide-by-16, 8 bits, asynchronous one stop bit.
        expect(read(0x29), 0x88, "USART control register");
        write(0x2b, 0xff);
        expect(read(0x2b), 3, "RX status flags cannot be written into existence");
        write(0x2d, 1);
        expect(read(0x2d), 0x81, "enabled transmitter is ready");
        write(0x2f, 0x52);
        expect(read(0x2d), 1, "UDR write occupies transmit buffer");
        expect(read(0x2f), 0, "disconnected RX does not echo transmit data");
        write(0x25, 1);
        write(0x1d, 1);
        clocks(4);
        expect(read(0x2d), 0x81, "timer D clock transfers transmit buffer");
        expect(acknowledge(), 0x4a, "transmit buffer empty vector");
        clocks(8 * 16 * 10);
        expect(read(0x2d), 0xc1, "completed unrefilled transmitter reports underrun");
        expect(acknowledge(), 0x49, "transmitter underrun vector");
        expect(read(0x2d), 0x81, "TSR read clears underrun");
        expect(read(0x2b), 3, "disconnected receiver stays idle");
        expect(read(0x0b), 0, "disconnected receiver produces no interrupt");
        write(0x2d, 0);
        expect(read(0x2d), 0x90, "disabled transmitter reports END");
        expect(acknowledge(), 0x49, "transmitter END vector");

        // A held TSR read must clear its old flag only once, so an underrun
        // appearing later in that same request remains visible on the next read.
        write(0x2d, 1);
        write(0x2f, 0xa5);
        clocks(8);
        dut.req = 1;
        dut.write = 0;
        dut.addr = 0x2d;
        step();
        clocks(8 * 16 * 10);
        expect(dut.rdata, 0x81, "held TSR read preserves data");
        dut.req = 0;
        step();
        expect(read(0x2d), 0xc1, "held TSR read does not consume future underrun");

        dut.req = 1;
        dut.write = 1;
        dut.addr = 0x07;
        dut.wdata = 0xff;
        dut.iack = 1;
        dut.reset = 1;
        step();
        expect(dut.ack, 0, "reset suppresses bus acknowledge");
        expect(dut.irq, 0, "reset suppresses interrupt");
        dut.req = 0;
        dut.iack = 0;
        dut.reset = 0;
        step();
        expect(read(0x07), 0, "reset discards held write");
        expect(read(0x0b), 0, "reset clears pending interrupts");
        expect(read(0x0f), 0, "reset clears in-service interrupts");
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Simulation sim;
    sim.gpio();
    sim.software_eoi();
    sim.delay_timers();
    sim.event_and_pulse_timers();
    sim.uart_and_reset();
    std::cout << "st_mfp: " << sim.assertions << " assertions passed over "
              << sim.cycles << " cycles\n";
}
