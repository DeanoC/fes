// SPDX-License-Identifier: MIT
#include "Vclock_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdio>
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Vclock_sim_top top;
    constexpr std::array<unsigned,6> system={52224000,100,10,10,10,5};
    constexpr std::array<unsigned,6> cpu={985248,17,5,6,10,2};
    std::array<uint64_t,6> phase{},count{};
    top.clk=0;top.eval();
    if(top.prepared!=0b100111 || top.envelope_prepared!=0b100111) {
        std::printf("SID preparation guard masks oscillator=%x envelope=%x expected27\n",top.prepared,top.envelope_prepared);
        return 1;
    }
    for(unsigned tick=0;tick<52224000;++tick) {
        for(unsigned i=0;i<system.size();++i) {
            phase[i]+=cpu[i];
            bool expected=phase[i]>=system[i];
            bool actual=(top.enables>>i)&1;
            if(actual!=expected) {
                std::printf("clock %u tick %u enable %u expected %u\n",i,tick,actual,expected);
                return 1;
            }
            if(expected){phase[i]-=system[i];++count[i];}
        }
        top.clk=1;top.eval();top.clk=0;top.eval();
    }
    if(count[0]!=985248)return 1;
    std::puts("C64 clock PASS: 52224000 motherboard ticks match independent fractional reference, PAL/boundary/fractional2-3/adjacent/full-rate enables and SID preparation guard");
}
