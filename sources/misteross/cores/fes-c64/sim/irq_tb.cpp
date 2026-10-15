// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "verilated.h"
#include "irq_program.h"
#include "irq_vectors.h"
#include <array>
#include <cstdio>
struct Push { unsigned address,data; };
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);unsigned cases=0;
    for(unsigned scenario=0;scenario<IRQ_SCENARIOS;++scenario)
    for(unsigned phase=1;phase<=40;++phase)
    for(unsigned gaps=0;gaps<3;++gaps)
    for(unsigned release=0;release<2;++release) {
        Vcpu6502 top;std::array<uint8_t,65536> ram;irq_program(ram.data(),scenario);
        top.clk=0;top.reset=1;top.IRQ=0;top.NMI=0;top.SO=1;top.RDY=0;top.DI=0;top.eval();
        int origin=-1,handler=-1;unsigned enabled=0,count=0,skip=irq_program_pushes[scenario];std::array<Push,3> pushed{};
        for(unsigned tick=0;tick<4000;++tick) {
            top.clk=0;top.RDY=gaps==0||tick%17==0||(gaps==2&&tick%17==8);top.eval();
            unsigned address=top.AB,data=top.DO;bool write=top.WE;
            if(tick==100)top.reset=0;
            if(top.RDY&&origin<0&&address==0x1100)origin=enabled;
            if(top.RDY&&origin>=0&&enabled-origin==phase)top.IRQ=1;
            top.clk=1;top.eval();
            if(top.RDY) {
                if(origin>=0&&handler<0) {
                    if(write&&address>=0x100&&address<0x200) {
                        if(skip)--skip;
                        else {
                            if(count>=3)return 1;
                            pushed[count++]={address,data};
                            if(release)top.IRQ=0; // B must remember hardware entry.
                        }
                    }
                    if(address==0x2000)handler=enabled-origin;
                }
                if(write)ram[address]=data;
                top.DI=ram[address];top.eval();++enabled;
                if(origin>=0&&enabled-origin>100)break;
            }
        }
        const auto &expected=irq_expected[scenario][phase-1];
        unsigned pc=count>=2?(pushed[0].data<<8)|pushed[1].data:0;
        unsigned status=count==3?pushed[2].data:0;
        const unsigned stack_top=0x1fd-irq_program_pushes[scenario];
        bool addresses=count==0||(count==3&&pushed[0].address==stack_top&&pushed[1].address==stack_top-1&&pushed[2].address==stack_top-2);
        if(origin<0||handler!=expected.cycle||pc!=expected.pc||status!=expected.status||!addresses||count!=(expected.cycle<0?0u:3u)) {
            std::printf("IRQ %s phase=%u gaps=%u release=%u got cycle=%d PC=%04x P=%02x pushes=%u; netlist %d/%04x/%02x\n",
                irq_names[scenario],phase,gaps,release,handler,pc,status,count,expected.cycle,expected.pc,expected.status);return 1;
        }
        ++cases;
    }
    std::printf("6502 IRQ PASS: %u transistor-reference entry/stack cases, 40 arrival phases, CLI/SEI/PLP/RTI, PHP/PHA, branches, RMW, enable gaps and withdrawn IRQ\n",cases);
}
