// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "Vcpu6502___024root.h"
#include "verilated.h"
#include "axs_oracle_program.h"
#include "axs_oracle_vectors.h"
#include <array>
#include <cstdio>
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    unsigned cases=0;
    for(unsigned scenario=0;scenario<AXS_ORACLE_CASES;++scenario)
    for(unsigned gaps=0;gaps<3;++gaps) {
        Vcpu6502 top;std::array<uint8_t,65536> ram;axs_transistor_program(ram.data(),scenario);
        top.clk=0;top.reset=1;top.IRQ=0;top.NMI=0;top.SO=1;top.RDY=0;top.DI=0;top.eval();
        unsigned count=0;bool done=false;auto c=axs_transistor_case(scenario);
        for(unsigned tick=0;tick<10000&&!done;++tick) {
            top.clk=0;top.RDY=gaps==0||tick%17==0||(gaps==2&&tick%17==8);top.eval();
            unsigned address=top.AB,data=top.WE?top.DO:ram[address];bool write=top.WE;
            if(tick==100)top.reset=0;
            bool record=top.RDY&&(count||address==c.pc)&&count<3;
            top.clk=1;top.eval();
            if(top.RDY) {
                if(record) {
                    const auto &expected=axs_golden[scenario].bus[count];
                    if(address!=expected[0]||data!=expected[1]||write!=expected[2]) {
                        std::printf("AXS source case%u gaps%u bus%u %04x/%02x/%u expected%04x/%02x/%u\n",scenario,gaps,count,address,data,write,expected[0],expected[1],expected[2]);return 1;
                    }
                    ++count;
                }
                if(write)ram[address]=data;
                top.DI=ram[address];top.eval();
                if(count==3&&write&&address==0x205) {
                    for(unsigned i=0;i<6;++i)if(ram[0x200+i]!=axs_golden[scenario].state[i]) {
                        std::printf("AXS source case%u gaps%u state%u %02x expected%02x\n",scenario,gaps,i,ram[0x200+i],axs_golden[scenario].state[i]);return 1;
                    }
                    done=true;
                }
            }
        }
        if(!done){std::printf("AXS source case%u gaps%u did not finish\n",scenario,gaps);return 1;}
        ++cases;
    }
    unsigned interrupts=0;
    for(unsigned mode=0;mode<4;++mode)
    for(unsigned phase=1;phase<=AXS_INTERRUPT_PHASES;++phase)
    for(unsigned gaps=0;gaps<3;++gaps) {
        Vcpu6502 top;std::array<uint8_t,65536> ram;axs_interrupt_program(ram.data());
        top.clk=0;top.reset=1;top.IRQ=0;top.NMI=0;top.SO=1;top.RDY=0;top.DI=0;top.eval();
        int origin=-1;unsigned enabled=0,count=0,pushed[3]={0},handler=0,when=0,x=0;
        for(unsigned tick=0;tick<10000&&!handler;++tick) {
            top.clk=0;top.RDY=gaps==0||tick%17==0||(gaps==2&&tick%17==8);top.eval();
            unsigned address=top.AB,data=top.DO;bool write=top.WE;
            if(tick==100)top.reset=0;
            if(top.RDY&&origin<0&&address==0x1100)origin=enabled;
            if(top.RDY&&origin>=0&&enabled-origin==phase) {if(mode!=1&&mode!=2)top.IRQ=1;if(mode!=0)top.NMI=1;}
            if(top.RDY&&origin>=0&&enabled-origin==phase+1&&mode==2)top.NMI=0;
            top.clk=1;top.eval();
            if(top.RDY) {
                if(origin>=0&&write&&address>=0x100&&address<0x200) {
                    if(count>=3||address!=0x1fd-count)return 2;
                    pushed[count++]=data;
                }
                if(origin>=0&&(address==0x2000||address==0x2100)) {handler=address;when=enabled-origin;x=top.rootp->cpu6502__DOT__AXYS[2];}
                if(write)ram[address]=data;
                top.DI=ram[address];top.eval();++enabled;
            }
        }
        const auto &e=axs_interrupt_golden[mode][phase-1];
        unsigned pc=(pushed[0]<<8)|pushed[1];
        if(count!=3||handler!=e.handler||when!=e.cycle||pc!=e.pc||pushed[2]!=e.status||x!=e.x) {
            std::printf("AXS interrupt mode%u phase%u gaps%u got%04x/%u/%04x/%02x/X%02x expected%04x/%u/%04x/%02x/X%02x\n",mode,phase,gaps,handler,when,pc,pushed[2],x,e.handler,e.cycle,e.pc,e.status,e.x);return 1;
        }
        ++interrupts;
    }
    std::printf("AXS transistor PASS: %u bus/state cases, %u IRQ/NMI held/pulse/priority arrival cases, three RDY cadences, actual PLA/branch fragment and page/PC wrap\n",cases,interrupts);
}
