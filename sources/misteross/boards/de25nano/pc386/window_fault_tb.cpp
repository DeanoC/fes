// The window's fault record (fes_de25_hps_window): the first fault only.
// Port 0 faults with no grant; then, with a grant, port 1 faults outside the
// span. The record keeps port 0 and "no grant". After fault_clear it takes
// port 1's fault, still presented, with "outside".
#include "Vfes_de25_hps_window.h"
#include <cstdio>
#include <cstdint>

static Vfes_de25_hps_window *t;
static int failures = 0;

static void tick() {
    t->p0_clk = t->p1_clk = t->p2_clk = 0; t->eval();
    t->p0_clk = t->p1_clk = t->p2_clk = 1; t->eval();
}
static uint32_t record() { return t->fault_port | t->fault_no_grant << 3 | t->fault_outside << 4; }
static void expect(bool ok, const char *what) {
    if (!ok) { std::printf("FAIL %s (record %02x)\n", what, record()); ++failures; }
}

int main() {
    t = new Vfes_de25_hps_window;
    t->reset_n = 1; t->hold = 0; t->grant_valid = 0; t->grant_base = 0; t->fault_clear = 0;
    t->m0_waitrequest = t->m1_waitrequest = t->m2_waitrequest = 0;
    t->p0_burstcount = t->p1_burstcount = t->p2_burstcount = 1;
    for (int i = 0; i < 4; ++i) tick();
    expect(record() == 0, "no fault at rest");

    // Port 0 reads the window's first word with no grant.
    t->p0_address = 0x30000000u >> 4; t->p0_read = 1;
    for (int i = 0; i < 4 && !t->fault_port; ++i) { tick(); if (!t->p0_waitrequest) t->p0_read = 0; }
    for (int i = 0; i < 4; ++i) tick();
    expect(record() == 0x09, "port 0, no grant");

    // A grant; port 1 reads one word past its span.
    t->grant_base = 0x40000000u; t->grant_valid = 1;
    t->p1_address = (0x30000000u + 0x10000000u) >> 3; t->p1_read = 1;
    for (int i = 0; i < 8; ++i) { tick(); if (!t->p1_waitrequest) t->p1_read = 0; }
    expect(record() == 0x09, "a later fault leaves the first: port 0, no grant");

    // Clear: port 1's command is still presented, so its fault is now the first.
    t->fault_clear = 1; tick(); t->fault_clear = 0;
    for (int i = 0; i < 4; ++i) tick();
    expect(record() == 0x12, "after clear: port 1, outside");

    // A bridge reset discards the faulting command, even before hold arrives.
    t->reset_n = 0; t->grant_valid = 0; t->eval();
    expect(record() == 0, "reset clears the sticky fault without a clock");
    for (int i = 0; i < 4; ++i) tick();
    expect(record() == 0 && t->p0_drained && t->p1_drained && t->p2_drained,
           "queued commands are discarded without a reset-time fault");

    t->reset_n = 1; t->grant_valid = 1;
    for (int i = 0; i < 4; ++i) tick();
    t->p0_address = 0x30000000u >> 4;
    t->p1_address = t->p2_address = 0x30000000u >> 3;
    t->p0_read = t->p1_read = t->p2_read = 1;
    tick();
    t->p0_read = t->p1_read = t->p2_read = 0;
    expect(t->m0_read && t->m1_read && t->m2_read, "all three reads reach the bridge");
    tick();
    expect(!t->p0_drained && !t->p1_drained && !t->p2_drained, "accepted reads are still owed");

    // The bridge loses its replies on reset. No fake responses are supplied.
    t->reset_n = 0; t->grant_valid = 0; t->eval();
    expect(!t->m0_read && !t->m1_read && !t->m2_read, "reset fences further reads");
    for (int i = 0; i < 4; ++i) tick();
    t->hold = 1; t->reset_n = 1;
    for (int i = 0; i < 4; ++i) tick();
    expect(record() == 0 && t->p0_drained && t->p1_drained && t->p2_drained,
           "bridge reset drains locally without responses from the old grant");
    expect(t->p0_reset && t->p1_reset && t->p2_reset, "ports stay held for the fresh grant");

    std::printf(failures ? "FAIL\n" : "PASS\n");
    delete t;
    return failures ? 1 : 0;
}
