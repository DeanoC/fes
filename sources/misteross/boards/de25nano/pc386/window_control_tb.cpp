// fes_de25_hps_window_control: the contract's acceptance and refusal rules.
#include "Vfes_de25_hps_window_control.h"
#include "verilated.h"
#include <cstdint>
#include <cstdio>
#include <set>
#include "Vfes_de25_hps_window_control___024root.h"

static Vfes_de25_hps_window_control *t;
static int failures = 0;
static void tick() {  // clk_sys and machine_clk together; the rules do not depend on the ratio
    t->clk_sys = 1; t->machine_clk = 1; t->eval(); t->clk_sys = 0; t->machine_clk = 0; t->eval();
}
static int write(int a, uint32_t v) {
    t->address = a; t->writedata = v; t->byteenable = 15; t->write = 1; tick(); t->write = 0;
    int r = t->response; tick(); return r;
}
static uint32_t read(int a) { t->address = a; t->read = 1; tick(); t->read = 0; return t->readdata; }
static void expect(bool ok, const char *what) { if (!ok) { std::printf("FAIL %s\n", what); ++failures; } }
static void settle() { for (int i = 0; i < 8; ++i) tick(); }
static uint32_t record() { return t->fault_port | t->fault_no_grant << 3 | t->fault_outside << 4; }
// The control block's copy of the record, read through the FAULT register's layout.
static uint32_t fault_copy() {
    uint32_t word = t->rootp->fes_de25_hps_window_control__DOT__machine_fault;
    return word;
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    t = new Vfes_de25_hps_window_control;
    t->reset_n = 0; t->machine_reset_n = 0; tick();
    t->reset_n = 1; t->machine_reset_n = 1; tick();
    expect(read(0x00) == 0x44325748, "identity");
    expect(read(0x04) == 0x00010000, "version");
    t->machine_drained = 1; t->machine_held = 1; settle();
    // Refusals: no generation, wrong span, misaligned, above 4 GiB, crossing 4 GiB, not held, not drained.
    write(0x10, 0x40000000); write(0x14, 0); write(0x18, 0x10000000); write(0x1c, 0); write(0x20, 0);
    expect(write(0x0c, 1) == 2, "refuse a zero generation");
    write(0x1c, 7);
    write(0x18, 0x08000000); expect(write(0x0c, 1) == 2, "refuse a short span");
    write(0x18, 0x10000000); write(0x10, 0x40000800); expect(write(0x0c, 1) == 2, "refuse a misaligned base");
    write(0x10, 0x40000000); write(0x14, 1); expect(write(0x0c, 1) == 2, "refuse a base above 4 GiB");
    write(0x14, 0);
    // The whole span lies below 4 GiB: a base whose span crosses it is refused.
    write(0x10, 0xf0001000); expect(write(0x0c, 1) == 2, "refuse a span that crosses 4 GiB");
    write(0x10, 0xfffff000); expect(write(0x0c, 1) == 2, "refuse a span from the last page");
    write(0x10, 0x40000000);
    t->machine_held = 0; settle(); expect(write(0x0c, 1) == 2, "refuse while not held");
    t->machine_held = 1; t->machine_drained = 0; settle(); expect(write(0x0c, 1) == 2, "refuse while not drained");
    expect(read(0x24) & 0x400, "a refused command sets FaultCommand");
    expect(!t->grant_valid, "no grant reaches the machine");
    // Acceptance.
    t->machine_drained = 1; settle();
    expect(write(0x0c, 1) == 0, "accept a valid grant");
    settle();
    expect(t->grant_valid && t->grant_base == 0x40000000, "the grant reaches the machine");
    expect((read(0x08) & 1) == 1, "status granted");
    expect(write(0x10, 0x50000000) == 2 && t->grant_base == 0x40000000, "grant registers are fixed while granted");
    // A machine fault is reported, and Clear clears it and the command fault.
    t->fault_port = 2; t->fault_outside = 1; settle();
    expect((read(0x24) & 0x207) == 0x202, "fault: port 1, outside");
    expect(read(0x08) & 8, "status fault");
    t->machine_drained = 0; settle(); expect(write(0x0c, 2) == 2, "refuse Clear while not drained");
    t->machine_drained = 1; settle();
    bool pulse = false;
    t->address = 0x0c; t->writedata = 2; t->byteenable = 15; t->write = 1; tick(); t->write = 0;
    for (int i = 0; i < 8; ++i) { tick(); if (t->fault_clear) pulse = true; }
    expect(pulse, "Clear pulses fault_clear in the machine clock");
    t->fault_port = 0; t->fault_outside = 0; settle();
    expect(!t->grant_valid, "Clear withdraws the grant");
    expect((read(0x24) & 0x707) == 0, "Clear clears the fault record");
    // An HPS reset can stop both clocks with a live grant. Neither the
    // stale control registers nor the machine-clock copy may reopen a window.
    expect(write(0x0c, 1) == 0, "grant a live window before HPS reset");
    settle();
    expect(t->grant_valid, "live grant before reset");
    t->fault_port = 1; t->fault_no_grant = 1;
    settle();
    expect(read(0x08) & 8, "live fault before reset");
    t->fault_port = 4; t->fault_no_grant = 0; t->fault_outside = 1;
    tick(); // Start the next fault transfer before resetting both sides.
    t->reset_n = 0;
    t->machine_reset_n = 0;
    t->eval();
    expect(!t->grant_valid, "reset removes the live grant without a machine clock");
    expect(fault_copy() == 0, "reset clears the receiving fault record");
    t->fault_port = 0; t->fault_outside = 0; // The window's machine-domain reset clears its record.
    t->reset_n = 1;
    settle();
    expect((read(0x08) & 1) == 0 && read(0x10) == 0 && read(0x1c) == 0,
           "reset clears the published grant, base and generation");
    t->machine_reset_n = 1;
    settle();
    expect(!t->grant_valid, "clock restart cannot restore the old grant");
    expect(read(0x08) == 6 && read(0x24) == 0,
           "both fault handshake sides restart clean before another grant");
    expect(write(0x0c, 1) == 2, "grant requires fresh owner parameters after reset");
    write(0x18, 0x10000000);
    write(0x1c, 8);
    // A span that ends exactly at 4 GiB is accepted.
    write(0x10, 0xf0000000); expect(write(0x0c, 1) == 0, "accept a span that ends at 4 GiB");
    settle(); expect(t->grant_valid && t->grant_base == 0xf0000000, "the top grant reaches the machine");
    expect(write(0x0c, 2) == 0, "Clear withdraws the top grant"); settle();
    expect(!t->grant_valid, "the top grant is withdrawn");
    expect(write(0x0c, 5) == 2, "an unknown command is refused");
    expect(write(0x3c, 1) == 2, "an unknown register is refused");
    t->address = 0x10; t->writedata = 1; t->byteenable = 3; t->write = 1; tick(); t->write = 0;
    expect(t->response == 2, "a partial write is refused"); tick();
    // The record crosses whole with the clocks at unrelated rates, changing
    // faster than the handshake can follow: every value clk_sys holds is one
    // the machine's record has had, and it settles on the last.
    {
        std::set<uint32_t> seen = {record()};
        uint32_t lcg = 12345, faulted = 0, torn = 0;
        int machine_phase = 0, sys_phase = 0;
        for (int step = 0; step < 200000; ++step) {
            bool m_edge = (machine_phase += 5) >= 7, s_edge = (sys_phase += 3) >= 4;
            if (m_edge) machine_phase -= 7;
            if (s_edge) sys_phase -= 4;
            if (!m_edge && !s_edge) continue;
            if (m_edge && step < 190000) {
                lcg = lcg * 1103515245u + 12345u;
                if ((lcg >> 16) % 3 == 0) {
                    uint32_t value = (lcg >> 20) & 31;
                    t->fault_port = value & 7; t->fault_no_grant = (value >> 3) & 1; t->fault_outside = value >> 4;
                    seen.insert(value);
                }
            }
            auto *r = t->rootp;
            uint32_t launch = r->fes_de25_hps_window_control__DOT__fault_launch;
            bool open = r->fes_de25_hps_window_control__DOT__fault_request != r->fes_de25_hps_window_control__DOT__fault_ack;
            t->machine_clk = m_edge; t->clk_sys = s_edge; t->eval();
            t->machine_clk = 0; t->clk_sys = 0; t->eval();
            if (s_edge && !seen.count(fault_copy())) ++faulted;
            // The launched copy changes only once clk_sys has acknowledged the last request.
            if (open && r->fes_de25_hps_window_control__DOT__fault_launch != launch) ++torn;
        }
        expect(faulted == 0, "every copied record is one the machine had");
        expect(torn == 0, "the held copy is still while a request is open");
        expect(fault_copy() == record(), "the copy settles on the last record");
    }
    std::printf("%s: %d failures\n", failures ? "FAIL" : "PASS", failures);
    delete t;
    return failures ? 1 : 0;
}
