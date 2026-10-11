// SPDX-License-Identifier: GPL-3.0-or-later
// A 79-word last PAL row followed by ordinary opened-bottom rows. Physical
// RAM contains plane 0 and plane 3 bits (index 9): a one-word earlier row
// becomes index3, independently of the cache/address-generation RTL.
#include "Vst_shifter_output_sim_top.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>

struct Test {
    Vst_shifter_output_sim_top d;
    unsigned assertions=0, pixels=0, requests=0;
    bool early_next_row=false;
    bool second_short;
    explicit Test(bool second):second_short(second){}
    void require(bool good,const char *message) {
        ++assertions;
        if(!good){std::cerr<<"Short-row capture FAIL: "<<message
            <<" line="<<d.native_line<<" cycle="<<d.native_cycle
            <<" row="<<d.row<<" x="<<d.x<<" rgb="<<d.rgb<<'\n';std::exit(1);}
    }
    unsigned expected(unsigned row,unsigned x) const {
        if(second_short && row>=238)return 0x0b7; // Two earlier words produce index6.
        if(second_short && row==237 && x>=316)return 0;
        if(row>=200)return 0x1a2; // Previous plane3 and current plane0 become planes0/1.
        if(row==199 && x>=316)return 0; // Shortened DE ends four pixels early.
        if(row==199 && x>=304)return 0x040; // The missing final plane3 leaves index1.
        return 0x12a;
    }
    void fabric() {
        d.eval();
        if(d.pixel && !d.reset_sys) {
            require(d.rgb==expected(d.row,d.x),"physical bitplane row displacement");
            ++pixels;
        }
        if(d.memory_req && !d.memory_ready)++requests;
        const unsigned relative=d.memory_addr-0x10000/2;
        if(d.memory_req && relative/80==200 && d.native_line==262 && d.native_cycle<300)
            early_next_row=true;
        d.memory_ready=d.memory_req;
        d.memory_data=(relative%4==0 || relative%4==3)?0xffff:0;
        d.clk_sys=1;d.eval();d.clk_sys=0;d.eval();
    }
    void pulse(unsigned line,unsigned cycle) {
        d.native_line=line;d.native_cycle=cycle;
        d.native_vblank=line==0 && cycle==0;
        const bool shortened=line==262 || (second_short && line==300);
        d.native_short_line=shortened && cycle>=300;
        d.native_word_loss=unsigned(line>262 || (line==262 && cycle>=383))+
            unsigned(second_short && (line>300 || (line==300 && cycle>=383)));
        d.native_display=line>=63 && line<310 && cycle>=56 && cycle<(shortened?372u:376u);
        d.native_pixel_ce=1;d.native_lookup_ce=0;
        fabric();
        d.native_pixel_ce=d.native_vblank=0;
        for(unsigned i=1;i<7;++i){d.native_lookup_ce=i==6;fabric();}
    }
    void pixel_clock(){d.clk_pixel=1;d.eval();d.clk_pixel=0;d.eval();}
    unsigned read(unsigned address){d.output_address=address;for(unsigned i=0;i<4;++i)pixel_clock();return d.output_rgb;}
    void run() {
        d.pal=1;d.hold=d.commit=d.native_pixel_ce=d.native_lookup_ce=d.native_vblank=0;
        d.native_short_line=d.native_word_loss=0;
        d.output_sof=d.border_start=d.border_ce=0;
        d.reset_sys=d.reset_pixel=1;fabric();pixel_clock();
        d.reset_sys=d.reset_pixel=0;
        d.commit=1;d.commit_index=9;d.commit_color=0x12a;fabric();
        d.commit_index=3;d.commit_color=0x1a2;fabric();d.commit=0;
        d.commit=1;d.commit_index=6;d.commit_color=0x0b7;fabric();d.commit=0;
        for(unsigned line=0;line<313;++line)for(unsigned cycle=0;cycle<512;++cycle)pulse(line,cycle);
        require(early_next_row,"next row was already prefetched before the frequency switch");
        require(d.frames==1,"shortened row publishes a complete frame");
        require(d.underruns==0,"late displacement needs no missing row");
        require(pixels==320*247,"fixed-width frame includes shortened-row border pixels");
        // Let the publication toggle cross into the independent consumer
        // before asking SOF to select it, as the live HDMI clock does.
        for(unsigned i=0;i<5;++i)pixel_clock();
        d.output_sof=1;pixel_clock();d.output_sof=0;
        for(unsigned i=0;i<5;++i)pixel_clock();
        require(d.front_valid && d.front_height==247 && d.front_pal,"published geometry");
        for(unsigned row:{0u,198u,199u,200u,201u,237u,238u,246u})
            for(unsigned x:{0u,15u,16u,303u,304u,315u,316u,319u})
                require(read(row*320+x)==expected(row,x),"immutable replay retains the displaced bitplanes");
        std::cout<<(second_short?"Two short PAL rows":"Short PAL row")<<" capture PASS: "<<assertions<<" assertions, "
            <<pixels<<" pixels; prefetched row survives late one-word displacement\n";
    }
};
int main(int argc,char **argv){Verilated::commandArgs(argc,argv);Test one(false);one.run();Test two(true);two.run();}
