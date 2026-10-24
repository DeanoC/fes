// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vmemory_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <random>

struct Simulation {
    Vmemory_top d;
    Sdram<Vmemory_top> ram;
    uint64_t cycles=0;
    explicit Simulation(unsigned delay) : ram(delay) {
        d.memory_valid=0; d.firmware_valid=0; d.cold_reset=1; d.reset=1;
        idle(8); d.cold_reset=0;
        for(unsigned i=0; !d.initialized && i<20000; ++i) step();
        require(d.initialized,"initialization timeout",cycles);
    }
    void step() {
        d.clk=0; d.eval(); d.dq_sample=ram.tick(d); d.eval();
        d.clk=1; d.eval(); ++cycles;
    }
    void idle(unsigned n) { while(n--) step(); }
    uint8_t request(unsigned a, bool write=false, unsigned data=0, bool firmware=false, unsigned hold=0) {
        auto ready=[&] {return firmware?d.firmware_ready:d.memory_ready;};
        auto done=[&] {return firmware?d.firmware_done:d.memory_done;};
        for(unsigned i=0; !ready() && i<1000; ++i) step();
        require(ready(),"admission timeout",cycles);
        d.address=a; d.wdata=data; d.write_cycle=write;
        d.firmware_address=a; d.firmware_data=data;
        d.memory_valid=!firmware; d.firmware_valid=firmware;
        step();
        // Payload is owned by the backend once accepted.
        d.address=0xabcde; d.wdata=~data; d.write_cycle=!write;
        d.firmware_address=0x1234; d.firmware_data=~data;
        if(!hold) {d.memory_valid=0; d.firmware_valid=0;}
        unsigned n=0;
        while(!done() && n++<1000) step();
        require(done(),"completion timeout",cycles);
        uint8_t result=d.memory_rdata;
        for(unsigned i=0;i<hold;++i) {
            step(); require(!done() && !ready(),"held VALID repeated request",cycles);
        }
        d.memory_valid=0; d.firmware_valid=0; step();
        return result;
    }
};
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    const unsigned delay=argc>1?std::stoul(argv[1]):0;
    Simulation s(delay);
    auto &d=s.d;
    d.reset=0; s.step();
    std::vector<uint8_t> reference(0xa0000,0);
    // Boundaries, both byte lanes, all banks and upper RAM row bits.
    for(unsigned a: {0u,1u,7u,8u,15u,16u,0xffffu,0x10000u,0x7ffffu,0x80000u,0x9fffeu,0x9ffffu}) {
        s.request(a,true,0xa5, false,35); reference[a]=0xa5;
        require(s.request(a)==0xa5,"RAM address/byte mapping",s.cycles);
    }
    std::mt19937 rng(0x8088);
    for(unsigned i=0;i<4000;++i) {
        unsigned a=(rng()%0x50000)*2, lo=rng()&255, hi=rng()&255;
        s.request(a,true,lo); reference[a]=lo;
        s.request(a+1,true,hi); reference[a+1]=hi;
        require(s.request(a)==lo && s.request(a+1)==hi,"neighbor byte clobbered",s.cycles);
    }
    for(unsigned i=0;i<2000;++i) {
        unsigned a=rng()%reference.size();
        require(s.request(a)==reference[a],"random RAM scoreboard",s.cycles);
    }
    // Loader is blocked while CPU is running, even with a held request.
    d.firmware_valid=1; d.firmware_address=0xffff; d.firmware_data=0x31;
    s.idle(100); require(!d.firmware_ready && !d.firmware_done,"live firmware admission",s.cycles);
    d.firmware_valid=0; d.reset=1; s.step();
    for(unsigned a: {0u,1u,0x7fffu,0x8000u,0xfffeu,0xffffu}) s.request(a,true,(a^0x5a)&255,true,40);
    d.reset=0; s.step();
    for(unsigned a: {0u,1u,0x7fffu,0x8000u,0xfffeu,0xffffu}) {
        unsigned expected=(a^0x5a)&255;
        require(s.request(0xf0000+a)==expected,"firmware mapping",s.cycles);
        s.request(0xf0000+a,true,~expected);
        require(s.request(0xf0000+a)==expected,"ROM write protection",s.cycles);
    }
    auto reads=s.ram.reads, writes=s.ram.writes;
    for(unsigned a: {0xa0000u,0xb8000u,0xc8000u,0xeffffu}) {
        s.request(a,true,0x33); require(s.request(a)==255,"ISA hole read",s.cycles);
    }
    require(s.ram.reads==reads && s.ram.writes==writes,"ISA hole reached SDRAM",s.cycles);
    // Reset sweep covers admission, ACTIVATE/CAS, recovery and completion.
    // Issued writes may commit; they must never be retried or acknowledged to
    // the reset CPU. The following request must wait for physical retirement.
    for(unsigned offset=0;offset<80;++offset) {
        unsigned a=0x4000+2*offset;
        while(!d.memory_ready) s.step();
        d.address=a; d.wdata=offset; d.write_cycle=1; d.memory_valid=1; s.step();
        d.memory_valid=0; s.idle(offset); d.reset=1; s.step();
        auto mode_sets=s.ram.mode_sets;
        s.idle(2); d.reset=0;
        for(unsigned i=0;i<150;++i) {
            s.step(); require(!d.memory_done,"completion escaped warm reset",s.cycles);
        }
        require(s.ram.mode_sets==mode_sets,"warm reset reinitialized SDRAM",s.cycles);
        require(s.request(a)==offset,"warm reset lost issued write",s.cycles);
        s.request(a,true,0xc3); require(s.request(a)==0xc3,"post-reset progress",s.cycles);
    }
    auto refreshes=s.ram.refreshes;
    d.reset=1; s.idle(10000);
    require(s.ram.refreshes>refreshes+10,"warm reset stopped physical refresh",s.cycles);
    d.reset=0; s.step(); require(s.request(0xfffff)==0xa5,"reset lost firmware",s.cycles);
    require(s.ram.write_masks[1]>0 && s.ram.write_masks[2]>0,"both write byte lanes",s.cycles);
    require(s.ram.max_refresh_gap<=850,"refresh interval",s.cycles);
    for(unsigned offset=0;offset<40;++offset) {
        while(!d.memory_ready) s.step();
        d.address=0x7000; d.wdata=offset; d.write_cycle=1; d.memory_valid=1; s.step();
        d.memory_valid=0; s.idle(offset);
        d.cold_reset=1; d.reset=1; s.step(); s.ram.cold_reset(); s.idle(7);
        require(!d.initialized && !d.memory_done && !d.firmware_done &&
                !s.ram.restart_pending,"cold reset cancellation/CKE boundary",s.cycles);
        auto modes=s.ram.mode_sets;
        d.cold_reset=0;
        for(unsigned i=0;!d.initialized && i<20000;++i) {
            s.step(); require(!d.memory_done && !d.firmware_done,"cold-reset phantom completion",s.cycles);
        }
        require(d.initialized && s.ram.mode_sets==modes+1,"cold reset did not initialize",s.cycles);
        d.reset=0; s.step();
        s.request(0x7000,true,0x69); require(s.request(0x7000)==0x69,"cold reset progress",s.cycles);
        require(s.request(0xfffff)==0xa5,"model firmware retention",s.cycles);
    }
    std::cout<<"PASS memory mask-delay="<<delay<<": "<<s.ram.reads<<" reads, "<<s.ram.writes<<" writes, "<<s.ram.refreshes
             <<" refreshes, 80 warm/40 cold-reset phases, "<<s.cycles<<" clocks\n";
}
