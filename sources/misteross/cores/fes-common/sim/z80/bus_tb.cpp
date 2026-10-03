// SPDX-License-Identifier: GPL-2.0-or-later
// Independent bus expectations from Zilog UM0080 figures 5--10.
#include "Vfes_z80_bus.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

class BusTest {
public:
    Vfes_z80_bus dut;
    const bool fast;
    unsigned checks = 0;

    explicit BusTest(bool mode) : fast(mode) { reset(); }

    void check(bool condition, const char *message) {
        ++checks;
        if (!condition) {
            std::cerr << (fast ? "fast" : "NMOS") << " bus: " << message
                      << " (check " << checks << ")\n";
            std::exit(1);
        }
    }

    void eval() { dut.clk = 0; dut.eval(); }

    bool edge(bool positive, bool negative) {
        dut.clk = 0;
        dut.ce_p = positive;
        dut.ce_n = negative;
        dut.eval();
        const bool completed = dut.ready;
        if (completed) check(positive, "ready must qualify a positive enable");
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.ce_p = 0;
        dut.ce_n = 0;
        dut.eval();
        check(!dut.ready, "ready must be low outside ce_p");
        return completed;
    }

    bool p() { return edge(true, false); }
    void n() { check(!edge(false, true), "negative edge cannot complete"); }
    void idle() { check(!edge(false, false), "disabled edge cannot complete"); }

    void reset() {
        dut.req = 0;
        dut.kind = 0;
        dut.addr = 0;
        dut.wdata = 0;
        dut.refresh_addr = 0;
        dut.extra_t = 0;
        dut.internal_t = 0;
        dut.din = 0;
        dut.wait_n = 1;
        dut.busrq_n = 1;
        dut.reset = 1;
        idle();
        check(inactive() && dut.busak_n, "reset must release bus controls");
        check(!dut.interrupt_blocked && !dut.resume, "reset must clear interrupt coordination");
        dut.reset = 0;
        eval();
    }

    bool inactive() const {
        return dut.m1_n && dut.mreq_n && dut.iorq_n && dut.rd_n && dut.wr_n &&
               dut.rfsh_n;
    }

    void request(unsigned k, uint16_t address = 0x4567) {
        dut.req = 1;
        dut.kind = k;
        dut.addr = address;
        dut.wdata = 0x9b;
        dut.refresh_addr = 0xab73;
        dut.extra_t = k == 5 ? 1 : 0;
        dut.internal_t = address & 31;
        dut.din = 0x56;
        eval();
    }

    void stop() { dut.req = 0; eval(); }

    void enabled_hold() {
        const uint16_t address = dut.a;
        const uint8_t data = dut.dout;
        const unsigned signals = pins();
        for (unsigned i = 0; i < 3; ++i) idle();
        check(dut.a == address && dut.dout == data && pins() == signals,
              "disabled clocks must preserve phase, address and data");
    }

    unsigned pins() const {
        return (dut.m1_n << 0) | (dut.mreq_n << 1) | (dut.iorq_n << 2) |
               (dut.rd_n << 3) | (dut.wr_n << 4) | (dut.rfsh_n << 5) |
               (dut.busak_n << 6);
    }

    void lengths_and_samples() {
        const std::array<unsigned, 8> lengths = {4, 3, 3, 4, 4, 7, 5, 9};
        for (unsigned k = 0; k < 8; ++k) {
            request(k, k == 7 ? 9 : 0x4500 + k);
            if (!fast) n(); // Falling T1: first machine-cycle metadata latch.
            const unsigned ticks = fast ? 1 : lengths[k];
            for (unsigned tick = 1; tick <= ticks; ++tick) {
                dut.din = 0x56;
                check(p() == (tick == ticks), "wrong unstretched cycle length");
                if (tick == ticks) break;
                // Opcode and acknowledge data is already captured before
                // refresh. Later din changes must not replace that sample.
                if ((k == 0 || k == 6) && tick >= 2) dut.din = 0xe1;
                if (k == 5 && tick >= 4) dut.din = 0xe1;
                n();
                enabled_hold();
            }
            stop();
            if (k == 0 || k == 1 || k == 3 || k == 5 || k == 6)
                check(dut.rdata == 0x56, "read byte must survive cycle completion");
        }
        request(7, 0);
        if (!fast) n();
        check(p(), "zero internal delay must terminate after one T state");
        stop();
    }

    void continuous_requests() {
        const std::array<unsigned, 4> kinds = {2, 1, 0, 7};
        const std::array<unsigned, 4> ticks = {3, 3, 4, 2};
        for (unsigned cycle = 0; cycle < kinds.size(); ++cycle) {
            // Update the metadata immediately after completion, keeping req
            // HIGH throughout every clk and enable of the entire sequence.
            request(kinds[cycle], kinds[cycle] == 7 ? 2 : 0x7800 + cycle);
            if (kinds[cycle] != 7)
                check(dut.a == 0x7800 + cycle, "next T1 address must be immediate");
            if (!fast) n();
            const unsigned count = fast ? 1 : ticks[cycle];
            for (unsigned tick = 1; tick <= count; ++tick) {
                check(dut.req, "back-to-back test must never lower req");
                check(p() == (tick == count),
                      "continuous req must not create an extra machine-cycle bubble");
                if (tick != count) n();
            }
        }
        stop();
    }

    void internal_address() {
        request(7,0x8ace);
        dut.internal_t=5;
        if (!fast) n();
        dut.internal_t=1; // Length was latched independently of the address.
        const unsigned ticks=fast ? 1 : 5;
        for (unsigned t=1;t<=ticks;++t) {
            check(dut.a==0x8ace && inactive(),
                  "internal delay must preserve its independently supplied address");
            check(p()==(t==ticks),"internal delay length must be latched");
            if (t!=ticks) n();
        }
        stop();
    }

    void extensions() {
        struct Example { unsigned kind, extra, base; uint16_t address; uint8_t data; };
        const std::array<Example, 7> cases = {{{0,2,4,0x1234,0x03},
            {0,7,4,0x4567,0x09}, {1,1,3,0x2345,0x6c},
            {1,5,3,0x3456,0xd2}, {2,2,3,0x5678,0x00},
            {5,0,6,0x6789,0x00}, {7,15,31,31,0x00}}};
        for (const auto& c : cases) {
            request(c.kind, c.address);
            dut.din = c.data;
            // Fetch extensions start at zero and are selected from the sampled
            // opcode during refresh, just as the instruction engine does.
            dut.extra_t = c.kind == 0 || c.kind == 1 ? 0 : c.extra;
            if (fast) {
                dut.extra_t = 15;
                check(p(), "fast mode must ignore even maximum extension");
                stop();
                continue;
            }
            n();
            const unsigned total = c.base + c.extra;
            for (unsigned t = 1; t <= total; ++t) {
                check(p() == (t == total), "wrong extended cycle completion edge");
                if (t == total) break;
                if (c.kind == 0 && t == 2) {
                    check(dut.rdata == c.data, "extension selection needs sampled opcode");
                    dut.extra_t = dut.rdata == 0x03 ? 2 : 7;
                    dut.din = 0xff;
                }
                n();
                if (c.kind == 1 && t == 2) {
                    check(dut.rdata == c.data, "read extension must retain sampled operand");
                    dut.extra_t = c.extra;
                    dut.din = 0x00;
                }
                if (t >= c.base) {
                    dut.wait_n = 0; // Extensions must not sample WAIT again.
                    check(inactive(), "extension states must not repeat transfer strobes");
                    check(dut.a == (c.kind == 0 ? 0xab73 : c.address) && dut.dout == 0x9b,
                          "extended transfer must retain original metadata");
                    dut.addr = 0xbadc;
                    dut.wdata = 0x13;
                    dut.kind = 7;
                }
            }
            stop();
            dut.wait_n = 1;
            if (c.kind == 0 || c.kind == 1)
                check(dut.rdata == c.data, "extension must preserve original fetched data");
        }
        if (fast) return;
        request(1); dut.extra_t = 5; n();
        for (unsigned t = 1; t <= 8; ++t) {
            if (t == 4) dut.busrq_n = 0;
            check(p() == (t == 8), "extended read must finish before DMA grant");
            if (t == 7) dut.busrq_n = 1; // Released after final-T sample.
            if (t != 8) {
                check(dut.busak_n, "BUSRQ cannot grant at unextended base boundary");
                n();
            }
        }
        check(!dut.busak_n, "extended final-T BUSRQ sample must control boundary grant");
        stop();
        check(!p() && dut.busak_n, "extended DMA grant must release normally");
    }

    void faithful_pin_timing() {
        request(0);
        check(!dut.m1_n && dut.mreq_n && dut.rd_n,
              "fetch T1 must present address before memory strobes");
        n();
        check(!dut.mreq_n && !dut.rd_n && !dut.m1_n && dut.a == 0x4567,
              "fetch falling T1 must enable memory read");
        check(!p(), "fetch T2 cannot complete");
        n();
        dut.din = 0xa5;
        check(!p(), "fetch T3 cannot complete");
        check(dut.m1_n && dut.mreq_n && dut.rd_n && !dut.rfsh_n &&
              dut.a == 0xab73 && dut.rdata == 0xa5,
              "fetch T3 must latch opcode and present refresh address");
        dut.din = 0x7e;
        n();
        check(!dut.mreq_n && dut.rd_n, "refresh MREQ begins falling T3");
        check(!p(), "fetch T4 cannot complete");
        check(!dut.mreq_n && !dut.rfsh_n, "refresh spans rising T4");
        n();
        check(dut.mreq_n && !dut.rfsh_n, "refresh MREQ ends falling T4");
        check(p(), "fetch ends at the next machine-cycle boundary");
        stop();
        check(dut.rdata == 0xa5, "refresh input data must not overwrite opcode");

        request(2);
        n();
        check(!dut.mreq_n && dut.wr_n && dut.dout == 0x9b,
              "write data must settle before WR");
        check(!p(), "write T2 cannot complete");
        check(dut.wr_n, "WR must remain inactive until falling T2");
        n();
        check(!dut.wr_n, "WR must begin at falling T2");
        check(!p(), "write T3 cannot complete");
        n();
        check(dut.wr_n && dut.mreq_n && dut.a == 0x4567 && dut.dout == 0x9b,
              "WR must finish half a cycle before address/data change");
        check(p(), "write must finish after three T states");
        stop();

        request(1); n();
        check(!p(), "memory read T2 cannot complete"); n();
        dut.din = 0x38;
        check(!p(), "memory read T3 cannot complete");
        check(!dut.rd_n, "memory read must remain active in rising T3");
        dut.din = 0xc2;
        n();
        check(dut.rd_n && dut.mreq_n && dut.rdata == 0xc2,
              "memory operand must latch at falling T3");
        dut.din = 0x00;
        check(p(), "memory read must finish after three T states");
        stop();
        check(dut.rdata == 0xc2, "memory data must survive strobe release");

        request(3); n();
        check(dut.iorq_n && dut.rd_n, "I/O T1 must present the address alone");
        check(!p(), "I/O T2 cannot complete");
        check(!dut.iorq_n && !dut.rd_n && dut.mreq_n,
              "I/O read strobes must begin in rising T2");
        n();
        check(!p(), "I/O mandatory Tw cannot complete"); n();
        check(!p(), "I/O T3 cannot complete");
        dut.din = 0x91;
        n();
        check(dut.iorq_n && dut.rd_n && dut.rdata == 0x91,
              "I/O byte and strobe release belong to falling T3");
        dut.din = 0xff;
        check(p(), "I/O read must finish after four T states");
        stop();
        check(dut.rdata == 0x91, "I/O data must survive strobe release");

        request(5);
        n();
        check(!dut.m1_n && dut.iorq_n && dut.rd_n && dut.mreq_n,
              "IRQ T1 must assert M1 without an ordinary read");
        check(!p(), "IRQ T2 cannot complete"); n();
        check(!p(), "IRQ first automatic Tw cannot complete");
        check(dut.iorq_n, "IRQ IORQ must wait until first Tw falling edge");
        n();
        check(!dut.iorq_n && !dut.m1_n && dut.rd_n,
              "IRQ acknowledges with M1 and IORQ, never RD");
        check(!p(), "IRQ second automatic Tw cannot complete"); n();
        dut.din = 0xd7;
        check(!p(), "IRQ refresh begins before final completion");
        check(dut.iorq_n && dut.m1_n && !dut.rfsh_n && dut.rdata == 0xd7,
              "IRQ vector must latch before refresh");
        n(); check(!dut.mreq_n, "IRQ must emit refresh MREQ");
        check(!p(), "IRQ T4 cannot complete"); n();
        check(!p(), "IRQ seventh T state must run"); n();
        check(p(), "IRQ acknowledge must take seven T states");
        stop();

        request(6); n();
        check(!dut.mreq_n && !dut.rd_n && !dut.m1_n,
              "faithful NMI must perform the documented discarded read");
        for (unsigned t = 1; t <= 5; ++t) {
            check(p() == (t == 5), "NMI acknowledge must take five T states");
            if(t==4) check(dut.a==0xab73 && dut.rfsh_n && dut.mreq_n && dut.rd_n,
                "quiet NMI fifth state must retain the refresh address");
            if (t != 5) n();
        }
        stop();
    }

    void late_wait_is_ignored() {
        // Zilog samples WAIT in T2/Tw, not during refresh. A later low level
        // cannot lengthen refresh or corrupt the byte already sampled.
        request(0); n();
        check(!p(), "fetch T2 cannot complete"); n();
        check(!p(), "fetch T3 cannot complete");
        dut.wait_n = 0;
        n(); check(!p(), "fetch T4 cannot complete"); n();
        check(p(), "late WAIT cannot stretch the refresh portion of M1");
        stop();
        dut.wait_n = 1;
    }

    void wait_states() {
        for (unsigned k : {0u, 1u, 2u, 3u, 4u, 5u, 6u}) {
            request(k);
            if (fast) {
                dut.wait_n = 0;
                check(!p(), "fast WAIT must suppress completion");
            } else {
                n();
                const unsigned sampling_phase = k == 5 ? 4 :
                                                (k == 3 || k == 4 ? 3 : 2);
                for (unsigned t = 2; t <= sampling_phase; ++t) {
                    check(!p(), "cycle cannot complete before WAIT sampling");
                    if (t != sampling_phase) n();
                }
                dut.wait_n = 0;
                n();
            }
            const unsigned held_pins = pins();
            const uint16_t held_a = dut.a;
            const uint8_t held_dout = dut.dout;
            // Once launched, request metadata cannot alter the held transfer.
            dut.addr = 0xbadc;
            dut.wdata = 0x11;
            dut.kind = 7;
            dut.refresh_addr = 0x0000;
            for (unsigned extra = 0; extra < 3; ++extra) {
                check(!p(), "WAIT must add whole T states");
                check(pins() == held_pins && dut.a == held_a &&
                      dut.dout == held_dout,
                      "WAIT must hold all transfer strobes and metadata");
                n();
            }
            dut.wait_n = 1;
            if (!fast) n(); // Release is sampled on the falling edge.
            unsigned remaining = 0;
            while (!p()) {
                n();
                check(++remaining < 8, "WAIT release must make progress");
            }
            stop();
            check(fast || remaining == (k == 5 ? 3u : k == 6 ? 3u :
                                        k == 0 ? 2u : 1u),
                  "automatic and external WAIT counts must remain distinct");
        }
    }

    void bus_request() {
        request(1);
        if (!fast) {
            n();
            dut.busrq_n = 0;
            for (unsigned t = 1; t <= 3; ++t) {
                check(dut.busak_n, "BUSRQ cannot cut a machine cycle short");
                check(p() == (t == 3), "BUSRQ must allow the current read to finish");
                if (t==2) check(dut.interrupt_blocked,
                    "sampled final-T BUSRQ must inhibit boundary interrupt recognition");
                if (t != 3) n();
            }
        } else {
            dut.wait_n = 0;
            check(!p(), "fast request must start before arbitration test");
            dut.busrq_n = 0;
            dut.wait_n = 1;
            check(p(), "fast BUSRQ must finish the active transfer");
        }
        check(!dut.busak_n && inactive(), "BUSRQ grants only at cycle boundary");
        check(dut.interrupt_blocked && !dut.resume,
              "DMA grant inhibits interrupts until an enabled release edge");
        request(2, 0x1234);
        for (unsigned t = 0; t < 3; ++t) {
            n();
            check(!p() && !dut.busak_n && inactive(),
                  "granted bus must suppress every new request");
        }
        dut.busrq_n = 1;
        idle();
        check(!dut.busak_n, "bus grant release needs an enabled positive edge");
        dut.ce_p=1; eval();
        check(dut.resume && dut.interrupt_blocked && !dut.busak_n,
              "resume must be visible before the release positive edge");
        dut.ce_p=0; eval();
        check(!p(), "bus release cannot also consume a request");
        check(dut.busak_n, "bus must release when BUSRQ is inactive");
        check(!dut.resume && !dut.interrupt_blocked,
              "interrupt coordination clears after bus release");
        if (!fast) n();
        for (unsigned t = 1; t <= (fast ? 1u : 3u); ++t) {
            check(p() == (t == (fast ? 1u : 3u)),
                  "request pending during DMA must resume normally");
            if (!fast && t != 3) n();
        }
        stop();
        dut.busrq_n = 0;
        check(!p() && !dut.busak_n && inactive(),
              "idle BUSRQ must grant before accepting a new request");
        dut.busrq_n = 1;
        check(!p(), "idle bus release cannot complete anything");
    }

    void late_bus_request() {
        request(1); n();
        check(!p(), "first read T2 cannot complete"); n();
        check(!p(), "first read T3 samples inactive BUSRQ");
        dut.busrq_n = 0; // Too late for this cycle's BUSRQ sampling edge.
        eval();
        check(!dut.interrupt_blocked,
              "late BUSRQ must not retroactively inhibit the finishing boundary");
        n();
        check(p() && dut.busak_n, "late BUSRQ must wait for another machine cycle");
        request(2, 0x1234);
        check(dut.a == 0x1234, "sampled permission must allow the next T1");
        n();
        for (unsigned t = 1; t <= 3; ++t) {
            check(p() == (t == 3), "next cycle must run before late DMA request");
            if (t != 3) n();
        }
        check(!dut.busak_n && inactive(), "late BUSRQ grants at following boundary");
        stop();
        dut.busrq_n = 1;
        check(!p(), "late DMA release cannot complete a request");
    }

    void run() {
        lengths_and_samples();
        continuous_requests();
        internal_address();
        extensions();
        if (!fast) {
            faithful_pin_timing();
            late_wait_is_ignored();
        }
        wait_states();
        bus_request();
        if (!fast) late_bus_request();
        reset();
        std::cout << (fast ? "fast" : "NMOS") << " Z80 bus: " << checks
                  << " checks passed\n";
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    const bool fast = argc > 1 && std::string(argv[1]) == "--fast";
    BusTest test(fast);
    test.run();
    return 0;
}
