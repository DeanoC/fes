// SPDX-License-Identifier: MIT
#include "Vclock_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdio>
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Vclock_sim_top top;
    constexpr std::array<unsigned,5> system={52224000,100,10,10,10};
    constexpr std::array<unsigned,5> cpu={985248,17,5,6,10};
    std::array<uint64_t,5> phase{},count{};
    top.clk=0;top.eval();
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
    std::puts("C64 clock PASS: 52224000 motherboard ticks match independent fractional reference, PAL/boundary/adjacent/full-rate enables");
}
