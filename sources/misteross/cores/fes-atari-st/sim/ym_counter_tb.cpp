// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vst_ym_counter_sim_top.h"
#include "verilated.h"
#include "st_ym_counter_primary.hpp"
#include <cstdlib>
#include <iostream>
#include <string>
using namespace st_ym_counter_primary;
struct Test {
    Vst_ym_counter_sim_top dut;
    std::string name;
    unsigned tick = 0, cases = 0, total = 0;
    void edge() { dut.clk=0; dut.eval(); dut.clk=1; dut.eval(); }
    void write(unsigned reg, unsigned value) {
        // Bus writes occur strictly BETWEEN /8 source counters, on idle edges.
        dut.chip_ce=0; dut.address_write=1; dut.data_write=0; dut.data=reg; edge();
        dut.address_write=0; dut.data_write=1; dut.data=value; edge();
        dut.data_write=0; Sound_WriteReg(reg, uint8_t(value));
    }
    void begin(const std::string &label) {
        name=label; tick=0; ++cases;
        dut.reset_n=0; dut.chip_ce=0; dut.address_write=dut.data_write=0;
        edge(); edge(); dut.reset_n=1; primary_reset();
        // Equivalent generator state, declared analytically, without modifying
        // either production reset: Fibonacci seed1/output1 maps to Galois
        // RndRack0x10000/current Noise_val0xffff. Full-period test proves it.
        RndRack=0x10000; Noise_val=0xffff;
        write(7,55); write(8,15); // A noise only; B/C silent.
    }
    void counter() {
        // Primary counter units are CHIP/8. Reset prescale/divider phase is0.
        // Sample immediately after eighth enabled fabric edge and compare the
        // actual primary DoSamples(1) result at the same counter edge.
        for(unsigned chip=0;chip<8;++chip) {
            dut.chip_ce=0; edge(); edge(); edge(); dut.chip_ce=1; edge();
        }
        dut.chip_ce=0;
        unsigned expected=primary_tick();
        if(unsigned(dut.levels)!=expected) {
            std::cerr<<"YM counter "<<name<<" first divergence tick="<<tick
                     <<" expected="<<expected<<" actual="<<dut.levels
                     <<" primary_noise_count="<<Noise_count<<'\n';
            std::exit(1);
        }
        ++tick; ++total;
    }
    void many(unsigned n) { while(n--) counter(); }
    void run() {
        for(unsigned period=0;period<32;++period) {
            begin("static-noise-"+std::to_string(period)); write(6,period);
            many(period==1?262144:4096);
        }
        // All period writes on both halves, with nonzero accumulated count.
        // Includes positive-period shrink, zero entry/exit and equal-period
        // writes. Counts keep running across writes, as in primary Sound_WriteReg.
        for(unsigned from=0;from<32;++from) for(unsigned to=0;to<32;++to)
            for(unsigned phase=0;phase<2;++phase) {
                begin("transition-"+std::to_string(from)+"-"+std::to_string(to)+"-phase"+std::to_string(phase));
                write(6,from); many((from?2*from-1:17)+phase);
                write(6,to); many(2*to+37);
            }
        begin("0-1-25-0-31");
        write(6,0); many(17); write(6,1); many(19); write(6,25); many(101);
        write(6,0); many(9); write(6,31); many(128);
        begin("25-3-shrink"); write(6,25); many(8); write(6,3); many(40);
        write(6,31); many(80); write(6,2); many(80);
        begin("mixed-gates-and-quiet-level");
        write(0,3); write(2,7); write(4,11);
        write(8,1); write(9,16); write(10,15); write(11,2); write(13,10);
        unsigned masks[]={0,7,56,63,21,42},periods[]={25,0,1,31,0,25};
        for(unsigned i=0;i<6;++i) { write(6,periods[i]); write(7,masks[i]); many(65); }
        std::cout<<"YM primary counters: "<<cases<<" cases, "<<total
                 <<" exact gated-level samples; full LFSR cycle and period transitions PASS\n";
        dut.final();
    }
};
int main(int argc,char **argv) { Verilated::commandArgs(argc,argv); Test t; t.run(); }
