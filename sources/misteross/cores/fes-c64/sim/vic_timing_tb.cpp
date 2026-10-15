// SPDX-License-Identifier: MIT
#include "Vvic_timing_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdio>
#include <stdexcept>

static void require(bool ok, const char *message) {
    if (!ok) throw std::runtime_error(message);
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vvic_timing_sim_top t;
    t.clk_sys=t.pixel_clk=t.phi=t.we=t.reading=t.read_sample=t.mem_we=t.color_we=0;
    t.addr=t.din=t.bank=t.mem_addr=t.mem_data=t.color_addr=t.color_data=0;
    t.cycle_clock=63;t.reset=1;
    auto sys=[&](){t.clk_sys=0;t.eval();t.clk_sys=1;t.eval();};
    auto pixel=[&](){t.pixel_clk=0;t.eval();t.pixel_clk=1;t.eval();};
    sys();pixel();
    for(unsigned a=0;a<65536;++a){
        t.mem_we=1;t.mem_addr=a;t.mem_data=((a*73)^(a>>3)^0xa5)&255;sys();
    }
    auto memory=[&](unsigned a,unsigned v){t.mem_addr=a;t.mem_data=v;sys();};
    for(unsigned a=0;a<1000;++a)memory(0x400+a,0);
    for(unsigned a=0;a<8;++a)memory(0x2000+a,0xf0);
    for(unsigned sprite=0;sprite<8;++sprite){
        memory(0x7f8+sprite,0xc0+sprite);
        for(unsigned b=0;b<63;++b)memory(0x3000+sprite*64+b,(b&1)?0xff:0xe4);
    }
    t.mem_we=0;
    for(unsigned a=0;a<1024;++a){t.color_we=1;t.color_addr=a;t.color_data=1;sys();}
    t.color_we=0;t.reset=0;sys();
    auto write=[&](unsigned a,unsigned v){t.addr=a;t.din=v;t.we=1;sys();t.we=0;sys();};
    write(0x11,0x1b);write(0x16,8);write(0x18,0x18);write(0x20,2);write(0x21,6);
    write(0x15,255);write(0x17,0x55);write(0x1a,7);write(0x1b,0x55);
    write(0x1c,0xaa);write(0x1d,0xcc);write(0x25,13);write(0x26,14);
    for(unsigned i=0;i<8;++i){write(i*2,32+3*i);write(i*2+1,80);write(0x27+i,1+i);}
    write(0x10,0);

    std::array<std::array<unsigned,8>,63> dots{};
    std::array<unsigned,54> writes{};
    std::array<std::array<unsigned,8>,8> pointer_dots{},row_dots{};
    unsigned pending_dots=0,reset_phases=0;
    const std::array<unsigned,8> strobe={1,8,14,21,27,34,40,47};
    auto check_events=[&](unsigned cycle,unsigned dot,bool count){
        static const std::array<unsigned,8> pointer_cycles={58,60,62,1,3,5,7,9};
        static const std::array<unsigned,8> row_cycles={59,61,63,2,4,6,8,10};
        unsigned pointer=0,row=0;
        for(unsigned n=0;n<8;++n){
            if(cycle==pointer_cycles[n]){pointer|=1u<<n;if(count)++pointer_dots[n][dot];}
            if(cycle==row_cycles[n]){row|=1u<<n;if(count)++row_dots[n][dot];}
        }
#ifndef SKIP_PREPARED_EVENTS
        require(t.prepared_pointer==pointer,"sprite pointer event has wrong cycle/dot");
        require(t.prepared_row==row,"sprite row event has wrong cycle/dot");
        require(bool(t.prepared_pending)==(cycle==58),"sprite pending update has wrong cycle/dot");
#endif
        if(count&&cycle==58)++pending_dots;
    };
    uint64_t accumulator=0,pixel_accumulator=0;
    unsigned cycle_clock=63,last_phi=0,period53=0,period54=0,wraps=0;
    std::array<unsigned,8> prepares{};
    unsigned phase26_pairs=0,reset_discards=0,checked_pending=0;
    bool reset_done=false;
    for(unsigned edge=0;edge<5000000;++edge){
        t.cycle_clock=cycle_clock;
        accumulator+=985248;
        t.phi=accumulator>=52224000;
        if(t.phi){
            accumulator-=52224000;
            if(last_phi){
                const unsigned period=edge-last_phi;
                require(period==53||period==54,"fractional enable interval changed");
                if(period==53)++period53;else ++period54;
            }
            last_phi=edge;
        }
        t.we=t.reading=t.read_sample=0;t.reset=0;
        const unsigned line=t.raster,cycle=t.native_cycle;
        if(edge>4900000&&!reset_done&&t.write_pending){t.reset=1;reset_done=true;++reset_discards;}
        else if(line>=16&&line<=30&&cycle==20&&cycle_clock>=25&&cycle_clock<=28){
            // Border resolves three dots after source selection. Writes through
            // phase28 must affect dot4 when it resolves at dot7/phase47;
            // the completed write packet must then stay fixed.
            t.we=1;t.addr=0x20;t.din=3+2*(cycle_clock-25);
        }else if(line>=72&&line<=140&&cycle>=18&&cycle<=22){
            bool adjacent=false;
            for(unsigned phase:strobe)if(cycle_clock+1==phase||cycle_clock==phase||cycle_clock==phase+1)adjacent=true;
            if(adjacent){
                static const unsigned registers[]={0x21,0x25,0x26,0x27,0x1b,0x1c,0x1d};
                t.we=1;t.addr=registers[(line+cycle+cycle_clock)%7];
                t.din=(line*17+cycle*29+cycle_clock*7)&255;
            }
        }else if(line>80&&cycle==30&&cycle_clock==43){
            t.addr=(line&1)?0x1e:0x1f;t.reading=t.read_sample=1;
        }else if(line>80&&cycle==32&&cycle_clock==26){
            t.we=1;t.addr=0x19;t.din=6;
        }
        if(t.we&&cycle_clock<54)++writes[cycle_clock];
        t.clk_sys=0;t.eval();
        if(!t.reset && cycle_clock<48){
            bool expected_prepare=false,expected_dot=false;
            for(unsigned i=0;i<strobe.size();++i){
                if(cycle_clock+1==strobe[i]){expected_prepare=true;++prepares[i];}
                if(cycle_clock==strobe[i])expected_dot=true;
            }
            require(bool(t.prep_tick)==expected_prepare,"beam preparation SYS phase changed");
            require(bool(t.dot_tick)==expected_dot,"native dot SYS phase changed");
        }
        const bool dot=t.dot_tick&&!t.reset;
        if(dot){
            require(cycle>=1&&cycle<=63,"invalid PAL cycle");
            ++dots[cycle-1][t.dot];
            check_events(cycle,t.dot,true);
            const unsigned x=((cycle>=14?(cycle-14)*8:400+(cycle-1)*8)+t.dot)&511;
            require(t.prepared_x==x,"prepared beam differs from PAL coordinate");
        }
        if(cycle_clock==0&&cycle==1&&!t.reset)++wraps;
        const bool palette_dot=line>=16&&line<=30&&cycle==20&&cycle_clock==47&&!t.reset;
        if(line>=16&&line<=30&&cycle==20&&cycle_clock==48&&!t.reset){
            require(t.write_pending&&t.saved_ink==9,"post-dot write altered saved framebuffer pixel");
            ++checked_pending;
        }
        t.clk_sys=1;t.eval();
        if(t.reset)require(!t.write_pending,"reset failed to discard pending framebuffer write");
        if(palette_dot){
            require(t.write_pending&&t.saved_ink==9,"late border color did not observe the phase28 write");
            ++phase26_pairs;
        }
        if(t.phi)cycle_clock=0;else if(cycle_clock<63)++cycle_clock;
        pixel_accumulator+=74250000;
        while(pixel_accumulator>=52224000){pixel_accumulator-=52224000;pixel();}
    }
    // Reset can arrive on any SYS phase. The beam resets to native cycle 1;
    // its pointer event must be valid even if reset releases on a dot edge.
    for(unsigned reset_phase=0;reset_phase<54;++reset_phase){
        t.we=t.reading=t.read_sample=t.phi=0;t.reset=1;t.cycle_clock=reset_phase;
        sys();pixel();
        require(t.prepared_pointer==8&&t.prepared_row==0&&!t.prepared_pending,
                "reset did not prepare native cycle 1 events");
        ++reset_phases;t.reset=0;
        unsigned phase=(reset_phase+1)%54;
        for(unsigned edge=0;edge<162;++edge){
            t.cycle_clock=phase;t.phi=phase==53;t.clk_sys=0;t.eval();
            if(t.dot_tick)check_events(t.native_cycle,t.dot,false);
            t.clk_sys=1;t.eval();pixel();phase=(phase+1)%54;
        }
    }
    for(const auto &sprite:pointer_dots)for(unsigned count:sprite)
        require(count>0,"uncovered sprite pointer dot");
    for(const auto &sprite:row_dots)for(unsigned count:sprite)
        require(count>0,"uncovered sprite row dot");
    require(pending_dots>0&&reset_phases==54,"missing pending event or reset phase");
    std::printf("Sprite events PASS: all 64 pointer and64 row/dot pairs, %u pending dots, %u reset phases\n",pending_dots,reset_phases);
    for(unsigned count:prepares)require(count>0,"uncovered beam preparation phase");
    for(const auto &cycle:dots)for(unsigned count:cycle)require(count>0,"uncovered cycle/dot pair");
    for(unsigned phase:strobe){
        require(writes[phase-1]&&writes[phase]&&writes[phase+1],"missing adjacent-dot register writes");
    }
    require(period53&&period54&&wraps>312,"missing fractional cadence or line wrap");
    require(phase26_pairs&&checked_pending&&reset_discards==1,"missing write/reset corner");
    require(t.checked_writes>300000&&t.publications>=3,"insufficient complete-frame transactions");
    std::fprintf(stderr,"Coverage: %u collision-bearing SYS samples, %u displayed pixels\n",t.collision_sys_samples,t.displayed_pixels);
    require(t.collision_sys_samples>100&&t.displayed_pixels>1000000,"missing collisions or completed HDMI frames");
    std::printf("VIC timing PASS: %u SYS checks, %u dots, %u three-dot plus one-SYS-delayed writes, %u publications, %u collision-bearing SYS samples, %u displayed pixels; %u/%u periods53/54; %u phase26 palette pairs and %u pending reset discard\n",
        t.checked_clocks,t.checked_pixels,t.checked_writes,t.publications,t.collision_sys_samples,t.displayed_pixels,
        period53,period54,phase26_pairs,reset_discards);
}
