// SPDX-License-Identifier: GPL-2.0-or-later
// A retained Run stays fenced. A fresh grant and the normal machine hold
// hand off to the ATA verification path before execution starts.
#include "Vpc386_reset_hold_tb.h"
#include <cstdio>
#include <cstdint>

static Vpc386_reset_hold_tb *t;
static int failures = 0;

static void tick() {
    t->clk = 0; t->eval();
    t->clk = 1; t->eval();
}

static void expect(bool condition, const char *message) {
    if (!condition) {
        std::printf("FAIL %s (held=%u reset=%u request=%u fault=%u/%u/%u)\n", message,
                    unsigned(t->reset_hold), unsigned(t->system_reset), unsigned(t->port_request),
                    unsigned(t->fault_port), unsigned(t->fault_no_grant), unsigned(t->fault_outside));
        ++failures;
    }
}

int main() {
    t = new Vpc386_reset_hold_tb;
    t->clk = 0;
    t->machine_reset_n = 1;
    t->mailbox_exec_reset = 1;
    t->machine_ddr_hold = 1;
    t->grant_valid = 1;
    t->cpu_request = 1;
    t->verify_request = 0;
    for (int i = 0; i < 8; ++i) tick();
    expect(!t->reset_hold && !t->port_request,
           "a fresh granted machine hands off while execution stays held");
    t->mailbox_exec_reset = 0;
    t->machine_ddr_hold = 0;
    for (int i = 0; i < 8; ++i) tick();
    expect(!t->reset_hold && t->port_request,
           "ordinary execution without HPS reset remains released");
    expect(!t->port_drained, "an accepted read is still waiting for its reply");

    // A running CPU is reset while its old grant and an accepted DDR read are
    // present. The new HPS owner has not published a grant yet.
    t->grant_valid = 0;
    t->machine_reset_n = 0;
    t->eval();
    expect(t->reset_hold && t->system_reset && !t->port_request,
           "HPS reset asynchronously holds execution and DDR requests");
    // The PLL can stop for the whole reset pulse. Resume the clock only
    // after the reset input has deasserted, with no old read replies.
    t->machine_reset_n = 1;
    t->eval();
    for (int i = 0; i < 8; ++i) tick();
    expect(t->reset_hold && t->system_reset && !t->port_request,
           "released HPS reset leaves the formerly running machine held");
    expect(t->port_drained && t->fault_port == 0 && !t->fault_no_grant && !t->fault_outside,
           "held reset drains the ports without latching a no-grant fault");

    t->mailbox_exec_reset = 1;
    t->machine_ddr_hold = 1;
    tick();
    expect(t->reset_hold, "a held mailbox cannot hand off before a fresh grant");
    t->mailbox_exec_reset = 0;
    t->machine_ddr_hold = 0;

    // Grant alone cannot start a mailbox that retained its previous Run.
    t->grant_valid = 1;
    for (int i = 0; i < 5; ++i) tick();
    expect(t->reset_hold && !t->port_request, "a fresh grant cannot restart a running mailbox");

    t->mailbox_exec_reset = 1;
    for (int i = 0; i < 5; ++i) tick();
    expect(t->reset_hold && !t->port_request, "wait for the machine's own port hold");

    t->machine_ddr_hold = 1;
    for (int i = 0; i < 5; ++i) tick();
    expect(!t->reset_hold && !t->port_request, "hand off to the active machine hold");

    t->machine_ddr_hold = 0;
    t->verify_request = 1;
    bool issued = false;
    for (int i = 0; i < 8; ++i) {
        tick();
        issued |= t->verify_issued;
    }
    expect(issued && !t->port_request,
           "port 2 can verify the ATA image before execution release");

    t->verify_request = 0;
    t->mailbox_exec_reset = 0;
    for (int i = 0; i < 5; ++i) tick();
    expect(!t->reset_hold && t->port_request,
           "a fresh Run releases the CPU after grant and verification");
    expect(!t->fault_port && !t->fault_no_grant && !t->fault_outside,
           "the first post-launch request does not fault with the published grant");

    std::printf(failures ? "FAIL\n" : "PASS\n");
    delete t;
    return failures ? 1 : 0;
}
