// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vbyte_bench.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>

static void require(bool ok, const char* message) {
    if (!ok) { std::cerr << message << '\n'; std::exit(1); }
}
static void tick(Vbyte_bench& top) {
    top.clk = 1; top.eval(); top.clk = 0; top.eval();
}
static void init(Vbyte_bench& top, unsigned rate) {
    top.clk = 0; top.rate = rate; top.reset = 1; top.stop = 0; top.stall_ack = 0;
    top.eval(); tick(top); tick(top); top.reset = 0;
}
int main() {
    for (unsigned rate = 0; rate < 3; ++rate) {
        Vbyte_bench top; init(top, rate);
        for (int i = 0; i < 1200000 && !(top.pass || top.fail); ++i) tick(top);
        if (top.fail) std::cerr << "rate=" << rate << " step=" << unsigned(top.fault_step)
            << " expected=" << std::hex << top.fault_expect << " got=" << top.fault_got << '\n';
        require(top.pass && !top.fail, "controller byte lane did not pass at every rate");
        require(top.completed == 128 && top.checks == 512, "incomplete byte coverage");
        require(top.refresh_masked_writes != 0, "no masked write after refresh");
        std::cout << "PASS: byte controller rate " << rate << " and refresh adjacency\n";
    }
    { Vbyte_bench top; init(top, 0); top.stall_ack = 1;
      for (int i = 0; i < 21000 && !top.fail; ++i) tick(top);
      require(top.fail && top.timed_out && !top.pass, "missing ACK did not fail closed");
      require(top.fault_addr == 0 && top.fault_step == 0 && top.fault_be == 3
          && top.fault_payload == 0xa55a, "timeout lost its request record"); }
    for (unsigned delay : {1u, 14000u, 17000u}) {
        Vbyte_bench top; init(top, 0);
        for (unsigned i = 0; i < delay; ++i) tick(top);
        top.stop = 1;
        for (int i = 0; i < 21000 && !top.stopped; ++i) tick(top);
        require(top.stopped && !top.pass && !top.fail, "stop failed during byte preflight");
    }
    std::cout << "PASS: timeout receipt and preflight stops\n";
}
