// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vmemory_machine_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <fstream>
#include <iterator>
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    require(argc==3,"ROM argument required",0);
    std::ifstream file(argv[1],std::ios::binary);
    std::vector<uint8_t> rom{std::istreambuf_iterator<char>(file),{}};
    require(rom.size()==65536,"diagnostic ROM size",0);
    Vmemory_machine_top d;
    const unsigned delay=std::stoul(argv[2]);
    Sdram<Vmemory_machine_top> ram(delay);
    uint64_t cycles=0;
    auto step=[&] {
        d.clk=0; d.eval(); d.dq_sample=ram.tick(d); d.eval();
        d.clk=1; d.eval(); ++cycles;
    };
    auto byte=[&](unsigned a) {return (ram.words[a/2]>>((a&1)*8))&255;};
    d.reset=1; d.cold_reset=1; d.firmware_valid=0;
    d.cga_font_data=d.mda_font_data=0x81;
    for(unsigned i=0;i<8;++i) step();
    d.cold_reset=0;
    for(unsigned i=0;!d.initialized && i<20000;++i) step();
    require(d.initialized,"SDRAM initialization timeout",cycles);
    // Every byte of the ROM travels through the synthesizable loader and
    // masked SDRAM writes. There is no testbench ROM read or CPU intercept.
    for(unsigned a=0;a<rom.size();++a) {
        unsigned timeout=0;
        while(!d.firmware_ready && timeout++<1000) step();
        require(d.firmware_ready,"firmware loader admission",cycles);
        d.firmware_valid=1; d.firmware_address=a; d.firmware_data=rom[a]; step();
        d.firmware_valid=0; timeout=0;
        while(!d.firmware_done && timeout++<1000) step();
        require(d.firmware_done,"firmware loader completion",cycles); step();
    }
    for(unsigned a=0;a<rom.size();++a) require(byte(0xf0000+a)==rom[a],"loaded ROM differs",cycles);
    auto modes=ram.mode_sets;
    for(unsigned boot=0;boot<2;++boot) {
        d.reset=1;
        for(unsigned i=0;i<32;++i) step();
        d.reset=0;
        std::array<unsigned,2> dots{},lines{},frames{};
        std::array<bool,2> started{};
        bool success=false;
        size_t writes_seen=ram.ram_writes.size();
        unsigned ticks=0;
        for(;ticks<40000000;++ticks) {
            step(); require(!d.fault,"CPU/DMA/ISA fault",cycles);
            while(writes_seen<ram.ram_writes.size()) {
                const auto &w=ram.ram_writes[writes_seen++];
                if(w.address==0x300 && (w.lanes&1)) {
                    require((w.data&255)==0xaa,"8088 ROM failure",cycles);
                    success=true;
                }
            }
            if(!success) continue;
            for(unsigned c=0;c<2;++c) {
                unsigned mask=1u<<c;
                if(!(d.pixel_ce&mask)) continue;
                if(d.sof&mask) {
                    if(started[c]) {
                        require(lines[c]==(c?370u:262u) && dots[c]==0,"frame geometry",cycles);
                        ++frames[c];
                    }
                    started[c]=true; dots[c]=lines[c]=0;
                }
                if(!started[c]) continue;
                unsigned x=dots[c],y=lines[c],want=0;
                if(c && x<720 && y<350) want=(x%9==0 || x%9>=7)?15:0;
                if(!c && x<640 && y<200) want=((x%16<8)?!(x&1):bool(x&1))?15:0;
                require(((d.rgbi>>(4*c))&15)==want,"8088 ISA-programmed pixel",cycles);
                if(d.eol&mask) {dots[c]=0; ++lines[c];} else ++dots[c];
            }
            if(frames[0] && frames[1]) break;
        }
        require(success && frames[0] && frames[1],"SDRAM display boot timeout",cycles);
        require(ram.mode_sets==modes,"warm boot reinitialized SDRAM",cycles);
        std::cout<<"PASS SDRAM mask-delay="<<delay<<" 8088 CGA/MDA boot "<<boot<<": "<<ticks<<" fabric clocks, full frames verified\n";
    }
    require(ram.max_refresh_gap<=850,"refresh under firmware/CPU load",cycles);
    std::cout<<"Physical command model: "<<ram.reads<<" reads, "<<ram.writes<<" writes, "<<ram.refreshes<<" refreshes\n";
}
