// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "verilated.h"
#include "jsr_program.h"
#include "jsr_vectors.h"
#include <array>
#include <cstdio>
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);unsigned cases=0;
    for(unsigned scenario=0;scenario<sizeof(jsr_cases)/sizeof(*jsr_cases);++scenario)
    for(unsigned gaps=0;gaps<3;++gaps) {
        Vcpu6502 top;std::array<uint8_t,65536> ram;jsr_bus_program(ram.data(),scenario);
        top.clk=0;top.reset=1;top.IRQ=0;top.NMI=0;top.SO=1;top.RDY=0;top.DI=0;top.eval();
        bool started=false;unsigned count=0;
        for(unsigned tick=0;tick<10000&&count<7;++tick) {
            top.clk=0;top.RDY=gaps==0||tick%17==0||(gaps==2&&tick%17==8);top.eval();
            unsigned address=top.AB,data=top.WE?top.DO:ram[address];bool write=top.WE;
            if(tick==100)top.reset=0;
            if(top.RDY&&!started&&address==jsr_cases[scenario].pc)started=true;
            top.clk=1;top.eval();
            if(top.RDY) {
                if(started) {
                    const auto &expected=jsr_bus_expected[scenario][count];
                    // Reset stack scratch differs between the two harnesses;
                    // dummy-read data has no architectural effect. Compare all
                    // addresses/directions and every pushed data byte.
                    if(address!=expected.address||write!=expected.write||(write&&data!=expected.data)) {
                        std::printf("JSR case%u gaps%u bus%u got %04x/%02x/%u, transistor %04x/%02x/%u\n",
                            scenario,gaps,count,address,data,write,expected.address,expected.data,expected.write);return 1;
                    }
                    ++count;
                }
                if(write)ram[address]=data;
                top.DI=ram[address];top.eval();
            }
        }
        if(count!=7)return 1;
        ++cases;
    }
    std::printf("6502 JSR PASS: %u transistor-reference bus cases, dummy read, pushes, page/stack wrap, operand alias and enable gaps\n",cases);
}
