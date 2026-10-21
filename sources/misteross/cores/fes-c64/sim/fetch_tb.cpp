// SPDX-License-Identifier: MIT
#include "Vc64_vic_fetch.h"
#include "verilated.h"
#include <array>
#include <cstdio>
#include <stdexcept>
static void require(bool ok,const char *message){if(!ok)throw std::runtime_error(message);}
static unsigned code(unsigned cell){return 1+(cell*19)%127;}
static unsigned color(unsigned cell){return (cell*7)&15;}
static unsigned glyph(unsigned address){return (address*37+(address>>8)*11)&255;}
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    for(unsigned scroll=0;scroll<8;++scroll)for(unsigned bank=0;bank<4;++bank)for(unsigned mode=0;mode<3;++mode) {
        Vc64_vic_fetch t;
        t.clk=0;t.reset=1;t.first_phase=0;t.second_phase=0;t.sample=0;
        t.raster=0;t.cycle=1;t.d011=0x10|scroll|(mode==1?0x20:mode==2?0x40:0);
        t.d018=0x18;t.bank=bank;t.cpu_data=0x0a;t.ram_data=0;t.color_data=0;
        auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};tick();t.reset=0;
        unsigned matrix_reads=0,active_glyphs=0,ba_cycles=0,owned_cycles=0;
        for(unsigned line=0;line<312;++line)for(unsigned cycle=1;cycle<=63;++cycle) {
            t.raster=line;t.cycle=cycle;t.first_phase=1;t.second_phase=0;t.sample=0;tick();
            bool bad=line>=48&&line<=247&&(line&7)==scroll;
            require(bool(t.badline)==bad,"badline mismatch");
            ba_cycles+=!t.ba_n;owned_cycles+=!t.aec_n;
            bool active=line>=48+scroll&&line<248+scroll;
            if(cycle>=16&&cycle<=55) {
                unsigned col=cycle-16, row=(line-48-scroll)&7;
                unsigned cell=((line-48-scroll)/8)*40+col;
                unsigned local=0x3fff;
                if(active)local=mode==1?0x2000+cell*8+row:0x2000+code(cell)*8+row;
                if(mode==2)local&=0x39ff;
                require(t.request&&!t.matrix_request,"missing first-phase graphics read");
                if(t.address!=bank*0x4000+local) {
                    std::printf("scroll%u bank%u mode%u line%u cycle%u address%04x expected%04x vc%u rc%u\n",scroll,bank,mode,line,cycle,t.address,bank*0x4000+local,t.vc,t.rc);return 1;
                }
                t.first_phase=0;t.sample=1;t.ram_data=glyph(t.address);tick();
                require(t.graphics_valid&&t.graphics_data==glyph(bank*0x4000+local),"graphics sample mismatch");
                require(t.graphics_matrix==(active?code(cell):0),"matrix latch not reused across native lines");
                require(t.graphics_color==(active?color(cell):0),"color latch mismatch");
                active_glyphs+=active;
            }
            t.first_phase=0;t.sample=0;t.second_phase=1;tick();
            if(bad&&cycle>=15&&cycle<=54) {
                unsigned cell=((line-48-scroll)/8)*40+cycle-15;
                require(t.request&&t.matrix_request&&!t.aec_n,"missing owned second-phase matrix read");
                require(t.address==bank*0x4000+0x400+cell&&t.color_address==cell,"matrix address mismatch");
                t.second_phase=0;t.sample=1;t.ram_data=code(cell);t.color_data=color(cell);tick();++matrix_reads;
            } else require(!t.request,"unexpected second-phase read");
            t.second_phase=0;t.sample=0;
        }
        require(matrix_reads==1000&&active_glyphs==8000&&ba_cycles==1075&&owned_cycles==1000,"native frame fetch totals");
    }
    std::puts("Native PAL fetch sequencer: 96 complete frames, all banks/scrolls, text/bitmap/ECM addresses, 1000 matrix and 8000 active glyph reads, 43-cycle BA and 40-cycle owned windows match");
}
