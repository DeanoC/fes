// SPDX-License-Identifier: MIT
#include "Vready_sim_top.h"
#include "Vready_sim_top___024root.h"
#include "verilated.h"
#include "ready_program.h"
#include "ready_vectors.h"
#include <array>
#include <cstdio>
#include <fstream>
#include <string>

int main(int argc,char **argv) {
    if(argc!=2)return 2;
    // Own firmware supplies only a reset vector into the open RAM program.
    std::array<unsigned char,36864> firmware{};
    firmware[16381]=0x10;
    std::ofstream file(argv[1]);
    for(unsigned byte:firmware) {
        char text[4];std::snprintf(text,sizeof(text),"%02x\n",byte);file<<text;
    }
    file.close();if(!file)return 2;
    std::string option=std::string("+C64_FIRMWARE_HEX=")+argv[1];
    char *args[]={argv[0],option.data()};Verilated::commandArgs(2,args);
    static_assert(sizeof(ready_expected)/sizeof(*ready_expected)==READY_LAST_PHASE-READY_FIRST_PHASE+1);
    unsigned failures=0;
    for(unsigned phase=READY_FIRST_PHASE;phase<=READY_LAST_PHASE;++phase) {
        Vready_sim_top top;top.clk=0;top.reset=1;top.eval();auto *r=top.rootp;
        auto &ram=r->ready_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
        for(unsigned i=0;i<sizeof(ready_code);++i)ram[0x1000+i]=ready_code[i];
        bool started=false,done=false;int cycle=-1;
        for(unsigned sys=0;sys<20000;++sys) {
            top.clk=0;top.eval();if(sys==400)top.reset=0;
            if(!started&&top.cpu_enable&&top.debug_pc==0x1000) {
                started=true;
                // Unit-fixture setup: disable matrix DMA and establish the
                // same four-sprite BA window as the independent pin model.
                // After this point the real VIC runs without intervention.
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__phi_div=phase-2;
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__raster=50;
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__regs[0x11]=0;
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__fetch__DOT__den_seen=0;
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__regs[0x15]=0xf0;
                for(unsigned i=0;i<8;++i)
                    r->ready_sim_top__DOT__machine__DOT__vic__DOT__regs[i*2+1]=240;
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__sprites__DOT__dma=0xf0;
                top.eval();
            }
            if(started&&top.cpu_enable)++cycle;
            bool marker=started&&r->ready_sim_top__DOT__machine__DOT__cycle_clock==26&&
                r->ready_sim_top__DOT__machine__DOT__bus_we&&
                r->ready_sim_top__DOT__machine__DOT__bus_addr==0xd011;
            unsigned data=r->ready_sim_top__DOT__machine__DOT__bus_wdata;
            top.clk=1;top.eval();
            if(marker) {
                unsigned expected=ready_expected[phase-READY_FIRST_PHASE];
                if(unsigned(cycle)!=expected||data!=13) {
                    std::printf("BA phase%u got%d/%02x, transistor reference%u/0d\n",phase,cycle,data,expected);
                    ++failures;
                }
                done=true;break;
            }
        }
        if(!done){std::printf("BA phase%u no marker PC%04x\n",phase,top.debug_pc);++failures;}
    }
    std::printf("C64 motherboard BA: %u failures across 51 transistor-reference read-stabilizer/write timings\n",failures);
    return failures?1:0;
}
