// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vreset_domains_model.h"
#include <cstdio>
#include <cstdlib>
static Vreset_domains_model d;
static unsigned cycles, asynchronous_assertions;
static bool monitoring, previous_clock;
static void need(bool value,const char* why) {
    if (!value) {std::fprintf(stderr,"FAIL %s at cycle %u\n",why,cycles);std::exit(1);}
}
static void eval() {
    const bool was=d.device_n;
    const bool memory_was=d.memory_n, request_was=d.request;
    const bool rising=!previous_clock && d.clk;
    d.eval();
    previous_clock=d.clk;
    if(!monitoring)return;
    need(!d.ninit_done && d.pll_locked || !d.device_n,
         "stopped-clock reset request did not contain device owner");
    need(!d.device_n || d.memory_n && !d.request,"device escaped memory reset/request ownership");
    need(!(request_was && !d.request) || !d.memory_n,
         "early request cleared before memory containment");
    need(!(memory_was && !d.memory_n) || request_was && d.request,
         "memory reset assertion lacked a stable early request");
    need(!d.device_n || was || rising,"device reset released without a clock edge");
}
static void tick() {d.clk=0;eval();d.clk=1;eval();++cycles;}
static void release() {
    d.ninit_done=0;d.pll_locked=1;eval();
    need(!d.device_n,"stopped clock released a reset epoch");
    for (unsigned n=0;n<32;++n) eval();
    unsigned edges=0;
    while (!d.device_n && edges++<64) tick();
    need(d.device_n && d.memory_n && !d.request && edges>=2 && edges<=64,
         "qualified memory release did not complete after clock restart");
    for(unsigned n=0;n<32;++n)tick();
}
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    d.clk=0;d.ninit_done=1;d.pll_locked=0;eval();
    for(unsigned n=0;n<32;++n)tick();
    monitoring=true;eval();
    release();
    for(unsigned trial=0;trial<128;++trial) {
        // Alternate stopped-low and stopped-high clocks, configuration reset
        // and PLL loss, and sub-cycle / multi-cycle reset pulses.
        d.clk=trial&1;eval();
        if(trial&2)d.ninit_done=1;else d.pll_locked=0;
        eval();++asynchronous_assertions;
        need(!d.device_n && d.request,"early request was not asynchronous");
        for(unsigned n=0;n<(trial%4)*3;++n)tick();
        release();
    }
    std::printf("PASS actual vendor memory sequencer/device bridge: asynchronous=%u cycles=%u\n",
        asynchronous_assertions,cycles);
}
