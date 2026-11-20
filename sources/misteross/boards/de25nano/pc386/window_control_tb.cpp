// fes_de25_hps_window_control: the contract's acceptance and refusal rules.
#include "Vfes_de25_hps_window_control.h"
#include "verilated.h"
#include <cstdint>
#include <cstdio>

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
static void settle() { for (int i = 0; i < 6; ++i) tick(); }

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    t = new Vfes_de25_hps_window_control;
    t->reset_n = 0; tick(); t->reset_n = 1; tick();
    expect(read(0x00) == 0x44325748, "identity");
    expect(read(0x04) == 0x00010000, "version");
    t->machine_drained = 1; t->machine_held = 1; settle();
    // Refusals: no generation, wrong span, misaligned, above 4 GiB, not held, not drained.
    write(0x10, 0x40000000); write(0x14, 0); write(0x18, 0x10000000); write(0x1c, 0); write(0x20, 0);
    expect(write(0x0c, 1) == 2, "refuse a zero generation");
    write(0x1c, 7);
    write(0x18, 0x08000000); expect(write(0x0c, 1) == 2, "refuse a short span");
    write(0x18, 0x10000000); write(0x10, 0x40000800); expect(write(0x0c, 1) == 2, "refuse a misaligned base");
    write(0x10, 0x40000000); write(0x14, 1); expect(write(0x0c, 1) == 2, "refuse a base above 4 GiB");
    write(0x14, 0);
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
    expect(write(0x0c, 5) == 2, "an unknown command is refused");
    expect(write(0x3c, 1) == 2, "an unknown register is refused");
    t->address = 0x10; t->writedata = 1; t->byteenable = 3; t->write = 1; tick(); t->write = 0;
    expect(t->response == 2, "a partial write is refused"); tick();
    std::printf("%s: %d failures\n", failures ? "FAIL" : "PASS", failures);
    delete t;
    return failures ? 1 : 0;
}
