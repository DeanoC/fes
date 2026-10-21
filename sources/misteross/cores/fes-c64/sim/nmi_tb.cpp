// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "verilated.h"
#include "nmi_program.h"
#include "nmi_vectors.h"
#include <array>
#include <cstdio>
struct Push { unsigned address,data; };
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);unsigned cases=0,failures=0;
    for(unsigned scenario=0;scenario<NMI_SCENARIOS;++scenario)
    for(unsigned phase=1;phase<=40;++phase)
    for(unsigned gaps=0;gaps<3;++gaps)
    for(unsigned stimulus=0;stimulus<5;++stimulus) {
        if(stimulus==4&&gaps==0)continue;
        bool pulse_sent=false;unsigned pulse_end=0,enabled_nmi_high=0;
        Vcpu6502 top;std::array<uint8_t,65536> ram;nmi_program(ram.data(),scenario);
        top.clk=0;top.reset=1;top.IRQ=0;top.NMI=0;top.SO=1;top.RDY=0;top.DI=0;top.eval();
        int origin=-1,handler=-1;unsigned enabled=0,count=0,post_handler_pushes=0,skip=nmi_program_pushes[scenario];std::array<Push,3> pushed{};
        for(unsigned tick=0;tick<4000;++tick) {
            top.clk=0;top.RDY=gaps==0||tick%17==0||(gaps==2&&tick%17==8);top.eval();
            unsigned address=top.AB,data=top.DO;bool write=top.WE;
            if(tick==100)top.reset=0;
            if(top.RDY&&origin<0&&address==0x1100)origin=enabled;
            if(stimulus<4&&top.RDY&&origin>=0&&enabled-origin==phase) {
                top.NMI=1;
                if(stimulus==2)top.IRQ=1; // NMI has priority over simultaneous IRQ.
            }
            if(top.RDY&&origin>=0&&enabled-origin==phase+1&&stimulus==1)top.NMI=0;
            // A one-SYS-clock pulse wholly between guest enables must survive
            // until the next enabled poll. It has the same pending-arrival
            // point as the oracle's one-guest-cycle pulse at that enable.
            if(stimulus==4&&!top.RDY&&origin>=0&&enabled-origin==phase&&!pulse_sent) {
                top.NMI=1;pulse_sent=true;pulse_end=tick+1;
            }
            if(stimulus==4&&pulse_sent&&tick==pulse_end)top.NMI=0;
            if(top.RDY&&top.NMI)++enabled_nmi_high;
            top.clk=1;top.eval();
            if(top.RDY) {
                if(handler>=0&&write&&address>=0x100&&address<0x200)++post_handler_pushes;
                if(origin>=0&&handler<0) {
                    if(write&&address>=0x100&&address<0x200) {
                        if(skip)--skip;
                        else {
                            if(count>=3)return 1;
                            pushed[count++]={address,data};
                            if(stimulus==3)top.NMI=0; // B must remember hardware entry.
                        }
                    }
                    if(address==0x2000)handler=enabled-origin;
                }
                if(write)ram[address]=data;
                top.DI=ram[address];top.eval();++enabled;
                if(origin>=0&&enabled-origin>100)break;
            }
        }
        const auto &expected=nmi_expected[scenario][stimulus==4?1:stimulus][phase-1];
        unsigned pc=count>=2?(pushed[0].data<<8)|pushed[1].data:0;
        unsigned status=count==3?pushed[2].data:0;
        const unsigned stack_top=0x1fd-nmi_program_pushes[scenario];
        bool addresses=count==0||(count==3&&pushed[0].address==stack_top&&pushed[1].address==stack_top-1&&pushed[2].address==stack_top-2);
        if((stimulus==4&&(!pulse_sent||enabled_nmi_high))||post_handler_pushes||origin<0||handler!=expected.cycle||pc!=expected.pc||status!=expected.status||!addresses||count!=(expected.cycle<0?0u:3u)) {
            std::printf("NMI %s phase=%u gaps=%u stimulus=%u got cycle=%d PC=%04x P=%02x pushes=%u; reference %d/%04x/%02x\n",
                nmi_names[scenario],phase,gaps,stimulus,handler,pc,status,count,expected.cycle,expected.pc,expected.status);++failures;
        }
        ++cases;
    }
    std::printf("6502 NMI: %u cases, 40 arrival phases, instruction/mask/branch/RMW/stack/store cases; three enable schedules, held/one-cycle/simultaneous IRQ/withdrawn/inter-enable pulse NMI; failures=%u\n",cases,failures);
    return failures?1:0;
}
