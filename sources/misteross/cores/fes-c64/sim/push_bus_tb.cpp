// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "verilated.h"
#include "push_program.h"
#include "push_vectors.h"
#include <array>
#include <cstdio>
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);unsigned cases=0;
    for(unsigned scenario=0;scenario<PUSH_CASES;++scenario)
    for(unsigned gaps=0;gaps<3;++gaps) {
        Vcpu6502 top;std::array<uint8_t,65536> ram;push_bus_program(ram.data(),scenario);
        top.clk=0;top.reset=1;top.IRQ=0;top.NMI=0;top.SO=1;top.RDY=0;top.DI=0;top.eval();
        bool started=false,done=false;unsigned count=0;
        for(unsigned tick=0;tick<10000&&!done;++tick) {
            top.clk=0;top.RDY=gaps==0||tick%17==0||(gaps==2&&tick%17==8);top.eval();
            unsigned address=top.AB,data=top.WE?top.DO:ram[address];bool write=top.WE;
            if(tick==100)top.reset=0;
            if(top.RDY&&!started&&address==push_cases[scenario].pc)started=true;
            top.clk=1;top.eval();
            if(top.RDY) {
                if(started&&count<4) {
                    const auto &expected=push_bus_expected[scenario][count];
                    if(address!=expected.address||data!=expected.data||write!=expected.write) {
                        std::printf("PUSH case%u gaps%u bus%u got %04x/%02x/%u, transistor %04x/%02x/%u\n",
                            scenario,gaps,count,address,data,write,expected.address,expected.data,expected.write);return 1;
                    }
                    ++count;
                }
                if(write)ram[address]=data;
                top.DI=ram[address];top.eval();
                if(started&&count==4&&push_cases[scenario].pc==0x01fc)done=true;
                else if(started&&write&&address==0x0200) {
                    for(unsigned i=0;i<3;++i)if(ram[0x0200+i]!=push_state_expected[scenario][i]) {
                        std::printf("PUSH case%u gaps%u S/A/P marker%u got%02x transistor%02x\n",
                            scenario,gaps,i,ram[0x0200+i],push_state_expected[scenario][i]);return 1;
                    }
                    done=true;
                }
            }
        }
        if(count!=4||!done)return 1;
        ++cases;
    }
    std::printf("6502 PHP/PHA PASS: %u transistor-reference bus/state cases, stack/page wrap, opcode alias, SP decrement, A/P preservation and enable gaps\n",cases);
}
