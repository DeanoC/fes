// SPDX-License-Identifier: MIT
#include "Vready_sim_top.h"
#include "Vready_sim_top___024root.h"
#include "verilated.h"
#include "jsr_program.h"
#include "jsr_vectors.h"
#include <array>
#include <cstdio>
#include <fstream>
#include <string>
int main(int argc,char **argv) {
    if(argc!=2)return 2;
    std::array<uint8_t,20480> firmware{};firmware[16381]=0x0f;
    std::ofstream file(argv[1]);
    for(unsigned byte:firmware) {
        char text[4];std::snprintf(text,sizeof(text),"%02x\n",byte);file<<text;
    }
    file.close();if(!file)return 2;
    std::string option=std::string("+C64_FIRMWARE_HEX=")+argv[1];
    char *args[]={argv[0],option.data()};Verilated::commandArgs(2,args);
    unsigned failures=0,cases=0;
    for(const auto &expected:jsr_expected) {
        const unsigned phase=expected.phase;
        Vready_sim_top top;top.clk=0;top.reset=1;top.eval();auto *r=top.rootp;
        auto &ram=r->ready_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
        for(unsigned i=0;i<sizeof(jsr_setup);++i)ram[0x0f00+i]=jsr_setup[i];
        for(unsigned i=0;i<sizeof(jsr_code);++i)ram[0x1000+i]=jsr_code[i];
        for(unsigned i=0;i<sizeof(jsr_target);++i)ram[0x1010+i]=jsr_target[i];
        bool aligned=false,started=false,done=false;int cycle=-1;
        for(unsigned sys=0;sys<20000;++sys) {
            top.clk=0;top.eval();if(sys==400)top.reset=0;
            if(!aligned&&top.cpu_enable&&top.debug_pc==0x1000) {
                aligned=true;
                // Establish a PAL badline around the first opcode. From here
                // the real motherboard, VIC BA and CPU stack cycles run freely.
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__phi_div=(phase+61)%63;
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__raster=phase==1?50:51;
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__regs[0x11]=0x1b;
                r->ready_sim_top__DOT__machine__DOT__vic__DOT__fetch__DOT__den_seen=1;
                top.eval();
            }
            // At BA release the prepared opcode can wait one extra enable.
            // Measure the instruction from its accepted bus cycle, not from
            // the earlier fixture-alignment request.
            if(aligned&&!started&&top.cpu_enable&&top.debug_pc==0x1000&&
               r->ready_sim_top__DOT__machine__DOT__cpu_run_ce)started=true;
            if(started&&top.cpu_enable)++cycle;
            bool marker=started&&r->ready_sim_top__DOT__machine__DOT__cycle_clock==26&&
                r->ready_sim_top__DOT__machine__DOT__bus_we&&
                r->ready_sim_top__DOT__machine__DOT__bus_addr==0xd020;
            unsigned data=r->ready_sim_top__DOT__machine__DOT__bus_wdata;
            top.clk=1;top.eval();
            if(marker) {
                if(unsigned(cycle)!=expected.cycle||data!=13) {
                    std::printf("JSR BA phase%u got%d/%02x, transistor %u/0d\n",phase,cycle,data,expected.cycle);
                    ++failures;
                }
                done=true;break;
            }
        }
        if(!done){std::printf("JSR BA phase%u no marker PC%04x\n",phase,top.debug_pc);++failures;}
        ++cases;
    }
    std::printf("C64 JSR/badline: %u failures across %u transistor-reference call/write timings\n",failures,cases);
    return failures?1:0;
}
