// SPDX-License-Identifier: MIT
#include "Vc64_vic_fetch.h"
#include "verilated.h"
#include <cstdio>
#include <initializer_list>
#include <stdexcept>
static void require(bool value,const char *message){if(!value)throw std::runtime_error(message);}
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    for(unsigned trigger : {15u,20u,40u}) for(unsigned bitmap=0;bitmap<2;++bitmap) {
        Vc64_vic_fetch t;
        t.clk=0;t.reset=1;t.first_phase=0;t.second_phase=0;t.sample=0;
        t.raster=0;t.cycle=1;t.d011=0x17|(bitmap?0x20:0);t.d018=0x18;
        t.bank=1;t.cpu_data=0x0a;t.ram_data=0;t.color_data=0;
        auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};tick();t.reset=0;
        unsigned c_reads=0,g_reads=0,invalid=0;
        for(unsigned line=0;line<=52;++line)for(unsigned cycle=1;cycle<=63;++cycle) {
            if(line==52&&cycle==trigger)t.d011=0x14|(bitmap?0x20:0);
            t.raster=line;t.cycle=cycle;t.sample=0;t.second_phase=0;t.first_phase=1;tick();
            if(cycle>=16&&cycle<=55) {
                bool active=line==52&&cycle>trigger;
                unsigned cell=cycle-trigger-1;
                unsigned code=cell<3?0xff:0x40+cell;
                unsigned local=active?(bitmap?0x2000+cell*8+7:0x2000+code*8+7):0x3fff;
                require(t.request&&!t.matrix_request,"missing dynamic glyph request");
                if(t.address!=0x4000+local){std::printf("trigger%u bitmap%u cycle%u address%04x expected%04x\n",trigger,bitmap,cycle,t.address,0x4000+local);return 1;}
                t.first_phase=0;t.sample=1;t.ram_data=0x5a;tick();
                require(t.graphics_data==0x5a,"dynamic RAM sample");
                require(t.graphics_matrix==(active?code:0),"late DMA matrix data or FF prefix");
                require(t.graphics_color==(active?(cell<3?0xa:cell&15):0),"late DMA color bus address");
                g_reads+=active;
            }
            t.first_phase=0;t.sample=0;t.second_phase=1;tick();
            if(line==52&&cycle>=trigger&&cycle<=54) {
                unsigned cell=cycle-trigger;
                require(t.request&&t.matrix_request,"late matrix request missing");
                require(t.address==0x4400+cell,"late VC address");
                bool owned=cell>=3;
                require(bool(t.aec_n)==!owned,"AEC must wait three complete BA cycles");
                require(t.color_address==cell,"color RAM address must follow VC");
                t.second_phase=0;t.sample=1;t.ram_data=0x40+cell;t.color_data=cell&15;tick();
                ++c_reads;invalid+=!owned;
            }
            t.second_phase=0;t.sample=0;
        }
        require(c_reads==55-trigger&&g_reads==55-trigger&&invalid==3,"late DMA length/invalid prefix");
        require(t.vcbase==55-trigger&&t.rc==0&&t.display,"late DMA leaves persistent non-40-aligned VCBASE");
    }
    std::puts("Native late DMA sequencer: starts15/20/40 in text and bitmap preserve RC, three FF/color-bus prefix cells, shortened fetches and persistent VCBASE displacement");
}
