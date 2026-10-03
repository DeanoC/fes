// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vspectrum_benchmark_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

static void require(bool condition, const std::string& text) {
    if (!condition) { std::cerr << "Spectrum benchmark: " << text << '\n'; std::exit(1); }
}
struct Machine {
    Vspectrum_benchmark_top dut;
    uint64_t clocks = 0, ticks = 0;
    Machine() {
        dut.clk_sys=0; dut.reset=1; dut.hold_wait=0; dut.nmi=0;
        for (int i=0;i<32;++i) step();
        dut.reset=0; clocks=0; ticks=0;
    }
    void step() {
        dut.clk_sys=1; dut.eval(); dut.clk_sys=0; dut.eval();
        if (!dut.reset) { ++clocks; ticks += dut.peripheral_tick; }
        require(!dut.illegal, "documented fixture trapped at PC " + std::to_string(dut.pc));
    }
    void until_signature(uint8_t expected) {
        while (dut.sig8000!=expected && clocks<3000000) step();
        require(dut.sig8000==expected,"fixture signature timeout");
    }
    void until_write(uint16_t addr, bool io) {
        for (unsigned i=0;i<100000;++i) {
            step();
            const uint32_t r=dut.request;
            if ((r&0xffff)==addr && (r&(1u<<27)) && (r&(1u<<29)) &&
                (r&(1u<<(io?25:24)))) return;
        }
        require(false,"write request timeout");
    }
};
int main(int argc,char**argv) {
    Verilated::commandArgs(argc,argv);
    require(argc==3,"expected variant and scenario");
    const bool fast=std::string(argv[1])=="fast";
    Machine m;
    if (std::string(argv[2])=="wait") {
        m.dut.hold_wait=1;
        m.until_write(0x00e1,true);
        // NMOS sampling needs a full T-state; both paths must then stay put.
        for(int i=0;i<100;++i) m.step();
        const uint32_t held=m.dut.request & ~(1u<<29);
        const uint16_t pc=m.dut.pc;
        for(int i=0;i<100;++i) {
            m.step();
            require((m.dut.request & ~(1u<<29))==held,"WAIT changed address/control/data");
            require(m.dut.pc==pc,"WAIT advanced the CPU");
        }
        require(m.dut.card_writes==1,"WAIT repeated the expansion write");
        // The NMI edge must remain pending throughout the stalled write.
        m.dut.nmi=1;
        for(int i=0;i<100;++i) m.step();
        m.dut.nmi=0;
        require(m.dut.sig8004==0,"NMI crossed an unaccepted write");
        m.dut.hold_wait=0;
        m.until_write(0x8100,false);
        m.dut.hold_wait=1;
        for(int i=0;i<200;++i) m.step();
        require(m.dut.sig8000==0,"RAM WAIT completed the program early");
        if(fast) require(m.dut.ram8100==0 && m.dut.ram_writes==0,
                         "fast RAM write committed before WAIT cleared");
        m.dut.hold_wait=0;
        m.until_signature(0x5a);
        require(m.dut.sig8001==0xc9,"registered ROMCS read failed");
        require(m.dut.sig8002==0x5a,"RAM write/read after WAIT failed");
        require(m.dut.ram_writes==1,"WAIT repeated the internal RAM write");
        require(m.dut.sig8004==0x77,"NMI edge was lost during WAIT");
        require(m.dut.card_writes==1,"extra expansion writes after WAIT");
        std::cout<<"Spectrum "<<argv[1]<<" WAIT/ROMCS: ok\n";
        return 0;
    }
    uint64_t marks[4];
    for (unsigned i=0;i<4;++i) { m.until_signature(i+1); marks[i]=m.clocks; }
    unsigned expected=0;
    for(unsigned i=0;i<128;++i) expected=((expected+1+3)&255)^5;
    require(m.dut.sig8001==expected,"compute result mismatch");
    require(m.dut.sig8002==128,"memory result mismatch");
    require(m.dut.sig8003==128 && m.dut.card_writes==128,"I/O result or single-commit mismatch");
    std::cout<<"Spectrum "<<argv[1]<<" clocks compute="<<marks[1]-marks[0]
             <<" memory="<<marks[2]-marks[1]<<" io="<<marks[3]-marks[2]<<'\n';
    // A registered expansion NMI wakes HALT and executes the open vector.
    for(int i=0;i<500;++i) m.step();
    require(m.dut.halted,"HALT was not reached");
    m.dut.nmi=1;
    for(int i=0;i<1000;++i) m.step();
    m.dut.nmi=0;
    for(int i=0;i<10000 && m.dut.sig8004!=0x77;++i) m.step();
    require(m.dut.sig8004==0x77,"registered NMI did not execute its vector");
    // Frame duration and interrupt width use peripheral ticks, independent of
    // CPU throughput. Check two complete pulses rather than startup phase.
    unsigned widths=0, periods=0, low_ticks=0;
    uint64_t falling=0; bool previous=m.dut.frame_int_n;
    for(unsigned i=0;i<3500000 && periods<2;++i) {
        m.step();
        if (!m.dut.peripheral_tick) continue;
        const bool level=m.dut.frame_int_n;
        if (previous && !level) {
            if(falling) {require(m.ticks-falling==69888,"frame period changed");++periods;}
            falling=m.ticks; low_ticks=0;
        }
        if (!level) ++low_ticks;
        if (!previous && level && falling) {require(low_ticks==32,"frame IRQ width changed");++widths;}
        previous=level;
    }
    require(periods==2 && widths>=2,"insufficient frame pulses");
    if(fast) require(m.clocks/16==m.ticks,"fast peripheral divider is not exactly 16");
    else require(m.ticks==(m.clocks*875)/13056,"normal fractional timebase changed");
    std::cout<<"Spectrum "<<argv[1]<<" NMI/frame timebase: ok\n";
}
