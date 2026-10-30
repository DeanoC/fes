#ifndef HALF_RATE_BOARD
#define HALF_RATE_BOARD 0
#endif
// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vsystem_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <deque>
int main(int argc,char**argv) {
    Verilated::commandArgs(argc,argv);
    require(argc>=2,"DQM delay required",0);
    bool mda=argc==3;
    Vsystem_top d;
    Sdram<Vsystem_top> ram(std::stoul(argv[1]));
    uint64_t cycles=0, pixels=0;
    unsigned pixel_phase=0, x=0, y=0, frames=0, effect_line=0, effects=0;
    bool check_video=false, effect_known=false, cold_dma_checkpoint=false;
    std::deque<uint32_t> pipe;
    auto edge=[&](unsigned level) {
        d.clk=level; pixel_phase+=1485; bool rising=false;
        if(pixel_phase>=2000) {pixel_phase-=2000; d.clk_pixel=!d.clk_pixel; rising=d.clk_pixel;}
        d.eval();
        if(!rising || d.reset_pixel) return;
        // Independently observe the DMA target and CPU source in physical
        // SDRAM before permitting a cold-start picture. The final success
        // marker separately verifies both card transfers/checks returned.
        // Warm HOLD may repeat the preceding complete frame.
        if (!cold_dma_checkpoint) {
            bool target=true;
            for(unsigned a=0x3000;a<0x3008;++a)
                target &= ((ram.words[a/2]>>((a&1)*8))&255)==0x6d;
            cold_dma_checkpoint=target && ram.words[0x2000]==0x3231 && ram.words[0x2001]==0x3433;
        }
        require(cold_dma_checkpoint || !(d.video_response & 0xffffff),
                "cold-start picture before DMA SDRAM activity",cycles);
        uint32_t r=d.video_request;
        pipe.push_back(r);
        if(pipe.size()>2) {
            uint32_t old=pipe.front();pipe.pop_front();
            uint32_t want=old&0x0fffffff;
            if((old&(1u<<30)) || !(old&(1u<<24))) want&=0x0f000000;
            if(old&(1u<<28)) {effect_line=0;effect_known=true;}
            if(mda && (effect_line&1)) want=(want&0x0f000000)|((want&0xfefefe)>>1);
            if(effect_known) {require(d.video_response==want,"shared video effect response",cycles);++effects;}
            if(old&(1u<<29)) ++effect_line;
        }
        if(!check_video) return;
        if(r&(1u<<28)) {if(frames) require(x==0 && y==750,"720p total geometry",cycles);x=y=0;++frames;}
        if(!frames) return;
        if(frames>3) {
            unsigned want=0;
            if(x>=160 && x<1120 && y<720) {
                unsigned sx=(x-160)*(mda?720u:640u)/960;
                bool ink=mda ? sx%9==0 || sx%9>=7 :
                    ((sx<16 && y*200/720<8) || (sx%16<8 ? !(sx&1):bool(sx&1)));
                want=ink?0xffffff:0;
            }
            require((r&0xffffff)==want,"SDRAM CPU/ISA-to-720p pixel",cycles);++pixels;
        }
        if(++x==1650) {x=0;++y;}
    };
    auto step=[&] {
        if constexpr (HALF_RATE_BOARD) {
            edge(0); d.clk_board=(cycles & 1)==0; edge(1);
            // Model the phase-18 ns chip-select capture after the phase-zero
            // board edge. This registered output feeds the SDRAM model.
            d.clk_cs=d.clk_board; d.eval();
            if (!d.clk_board) { d.dq_sample=ram.tick(d); d.eval(); }
            ++cycles;
        } else {
            edge(0); d.dq_sample=ram.tick(d); d.eval(); edge(1); ++cycles;
        }
        require(d.sdram_clk==(HALF_RATE_BOARD ? !d.clk_board : 0),"inverted SDRAM clock rising phase",cycles);
    };
    auto cold=[&] {
        check_video=false; pipe.clear();effect_known=false;
        d.cold_reset=d.reset_pixel=1;d.warm_reset=d.hold=0;d.load_enable=0;
        for(unsigned i=0;i<16;++i)step();
        ram.cold_reset();d.cold_reset=d.reset_pixel=0;
    };
    cold();
    // Reset a partially loaded image; completion of the previous image cannot
    // release the CPU, and cold start must reload from offset zero.
    d.load_enable=1;
    while(d.firmware_address<128 && cycles<200000)step();
    require(d.machine_reset && !d.firmware_loaded,"partial firmware gate",cycles);
    cold();d.load_enable=1;
    unsigned wait=0;
    while(d.firmware_address<0xff00 && wait++<5000000)step();
    require(d.firmware_address>=0xff00,"automatic loader timeout",cycles);
    d.load_enable=0;
    for(unsigned i=0;i<2000;++i) {step();require(d.machine_reset && !d.firmware_loaded,"early CPU release",cycles);}
    d.load_enable=1;wait=0;
    while(!d.firmware_loaded && wait++<100000)step();
    require(d.firmware_loaded && d.initialized,"full firmware completion",cycles);
    // Cancel a real issued CPU SDRAM read, rather than resetting only at HLT.
    // The physical read drains while the motherboard/CPU cancel their request.
    wait=0;
    while(!(!d.sdram_ncs && d.sdram_nras && !d.sdram_ncas && d.sdram_nwe) && wait++<10000)step();
    require(wait<10000,"CPU read before warm reset",cycles);
    d.warm_reset=1;
    for(unsigned i=0;i<32;++i)step();
    require(d.firmware_loaded && d.initialized,"warm reset lost firmware/storage",cycles);
    d.warm_reset=0;
    const auto modes=ram.mode_sets, writes=ram.writes;
    size_t seen=ram.ram_writes.size();
    for(unsigned boot=0;boot<2;++boot) {
        bool success=false;check_video=false;frames=0;pixels=0;
        unsigned dma_cycles=0, refresh=0, ticks=0;
        for(;ticks<45000000 && pixels<2475000;++ticks) {
            step();require(!d.fault,"CPU/DMA/ISA fault",cycles);
            dma_cycles+=d.dma_owner;refresh+=d.refresh_done;
            while(seen<ram.ram_writes.size()) {
                const auto&w=ram.ram_writes[seen++];
                if(w.address==0x300 && (w.lanes&1)) {
                    require((w.data&255)==0xaa,"original diagnostic firmware failed",cycles);
                    success=true;check_video=true;
                }
            }
        }
        require(success && pixels>=2475000 && effects>2000000,"system boot/video timeout",cycles);
        require(dma_cycles>100 && refresh>10,"DMA/guest refresh did not reach SDRAM",cycles);
        for(unsigned a=0x3000;a<0x3008;++a)
            require(((ram.words[a/2]>>((a&1)*8))&255)==0x6d,"ISA DMA write to real SDRAM",cycles);
        require(ram.words[0x2000]==0x3231 && ram.words[0x2001]==0x3433,"CPU SDRAM DMA source",cycles);
        require(ram.mode_sets==modes,"warm boot restarted SDRAM",cycles);
        std::cout<<"PASS system "<<(mda?"MDA Scanlines":"CGA Direct")
                 <<(HALF_RATE_BOARD?" external-CS-register":"")<<" mask-delay="<<argv[1]<<" boot="<<boot
                 <<" clocks="<<ticks<<" pixels="<<pixels<<" DMA="<<dma_cycles<<" refresh="<<refresh<<'\n';
        check_video=false;
        d.hold=1;
        for(unsigned i=0;i<2000;++i)step();
        require((d.video_response&0xffffff)==0 && d.machine_reset,"HOLD/mute",cycles);
        d.hold=0;d.warm_reset=1;
        for(unsigned i=0;i<32;++i)step();
        d.warm_reset=0;
    }
    require(ram.writes>writes && ram.max_refresh_gap<=(HALF_RATE_BOARD ? 390 : 850),"storage and physical refresh",cycles);
}
