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
        d.memory_valid=0; d.firmware_valid=0; d.disk_load_valid=0; d.media_read_valid=0;
        d.cold_reset=1; d.reset=1;
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
    void disk_load(unsigned a, unsigned data, unsigned hold=0) {
        d.disk_load_address=a; d.disk_load_data=data; d.disk_load_valid=1; d.eval();
        for(unsigned i=0; !d.disk_load_ready && i<1000; ++i) step();
        require(d.disk_load_ready,"disk upload admission timeout",cycles); step();
        d.disk_load_address=0; d.disk_load_data=~data; // accepted payload is retained
        if(!hold) d.disk_load_valid=0;
        unsigned n=0;
        while(!d.disk_load_done && n++<1000) step();
        require(d.disk_load_done,"disk upload completion timeout",cycles);
        for(unsigned i=0;i<hold;++i) {
            step(); require(!d.disk_load_done && !d.disk_load_ready,"held disk upload repeated",cycles);
        }
        d.disk_load_valid=0; step();
    }
    uint8_t disk_read(unsigned a, unsigned hold=0) {
        d.media_read_addr=a; d.media_read_valid=1;
        unsigned n=0;
        while(!d.media_read_ready && n++<1000) step();
        require(d.media_read_ready,"disk read completion timeout",cycles);
        uint8_t result=d.media_read_data;
        for(unsigned i=0;i<hold;++i) {
            step(); require(!d.media_read_ready,"held media request repeated",cycles);
        }
        d.media_read_valid=0; step();
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
    // Upload shares the physical device but occupies bytes 100000–159fff,
    // outside all guest RAM and firmware. Both byte lanes and both endpoints
    // are covered; a held VALID must not duplicate a write.
    s.request(0,true,0x63); s.request(1,true,0x95);
    s.request(0x80000,true,0xc1); s.request(0x80001,true,0xc2);
    s.request(0x9fffe,true,0xc3); s.request(0x9ffff,true,0xc4);
    d.reset=1; s.step();
    for(unsigned a: {0u,1u,2u,3u,0x1ffffu,0x20000u,0x3ffffu,0x40000u,0x59ffeu,0x59fffu}) {
        s.disk_load(a,(a^0x5b)&255,35);
        const auto physical=s.ram.words.at(0x80000+(a>>1));
        require(((physical>>(8*(a&1)))&255)==((a^0x5b)&255),
                "disk write has wrong physical SDRAM address",s.cycles);
    }
    d.disk_load_address=0x5a000; d.disk_load_data=0x4d; d.disk_load_valid=1;
    writes=s.ram.writes; s.idle(80);
    require(!d.disk_load_ready && !d.disk_load_done && s.ram.writes==writes,
            "out-of-range upload accepted",s.cycles);
    d.disk_load_valid=0; d.reset=0; s.step();
    // Runtime upload is allowed while the guest runs; its accepted write
    // uses the same physical buffer and does not reset the guest.
    s.disk_load(6,0x79,35);
    require(s.disk_read(6)==0x79,"live disk upload did not complete",s.cycles);
    for(unsigned a: {0u,1u,2u,3u,0x1ffffu,0x20000u,0x3ffffu,0x40000u,0x59ffeu,0x59fffu})
        require(s.disk_read(a,35)==((a^0x5b)&255),"disk byte/physical mapping",s.cycles);
    reads=s.ram.reads;
    require(s.disk_read(0x5a000)==255 && s.ram.reads==reads,"invalid disk read touched SDRAM",s.cycles);
    require(s.request(0)==0x63 && s.request(1)==0x95 && s.request(0x80000)==0xc1 &&
            s.request(0x80001)==0xc2 && s.request(0x9fffe)==0xc3 && s.request(0x9ffff)==0xc4 &&
            s.request(0xfffff)==0xa5,
            "disk buffer aliases guest RAM or firmware",s.cycles);
    // During reset no disk reads are admitted; firmware and disk uploads
    // contend for the sole physical port with firmware taking precedence.
    d.reset=1; d.media_read_addr=0; d.media_read_valid=1;
    reads=s.ram.reads; s.idle(80);
    require(!d.media_read_ready && s.ram.reads==reads,"disk read admitted during reset",s.cycles);
    d.media_read_valid=0; d.eval();
    d.firmware_address=0x4321; d.firmware_data=0xe1; d.firmware_valid=1;
    d.disk_load_address=4; d.disk_load_data=0x71; d.disk_load_valid=1; d.eval();
    require(d.firmware_ready && !d.disk_load_ready,"simultaneous upload priority",s.cycles);
    s.step();
    bool fw_done=false, disk_done=false;
    for(unsigned i=0;i<2000 && (!fw_done || !disk_done);++i) {
        s.step();
        if(d.firmware_done) {require(!fw_done,"firmware repeated under held valid",s.cycles); fw_done=true;}
        if(d.disk_load_done) {require(!disk_done && fw_done,"upload priority/order",s.cycles); disk_done=true;}
        require(!(d.firmware_done && d.disk_load_done),"two uploads completed together",s.cycles);
    }
    require(fw_done && disk_done,"contending uploads did not complete",s.cycles);
    s.idle(80); require(!d.firmware_done && !d.disk_load_done,"held uploads repeated",s.cycles);
    d.firmware_valid=0; d.disk_load_valid=0; s.step();
    // Once an upload is accepted its physical write retires even if a
    // caller violates the requirement to hold reset through DONE.
    d.disk_load_address=5; d.disk_load_data=0x72; d.disk_load_valid=1;
    require(d.disk_load_ready,"upload did not admit",s.cycles); s.step();
    d.reset=0;
    for(unsigned i=0;!d.disk_load_done && i<1000;++i) s.step();
    require(d.disk_load_done && !d.disk_load_ready,"accepted upload did not drain",s.cycles);
    writes=s.ram.writes; s.idle(80);
    require(!d.disk_load_done && !d.disk_load_ready && s.ram.writes==writes,
            "upload repeated after reset release",s.cycles);
    d.disk_load_valid=0; s.step();
    require(s.request(0xf4321)==0xe1 && s.disk_read(4)==0x71 && s.disk_read(5)==0x72,
            "contending/retired uploads corrupted data",s.cycles);
    // Ejection can race an accepted device byte. Do not overwrite while its
    // request is held, including the completion cycle.
    d.media_read_addr=1; d.media_read_valid=1;
    d.disk_load_address=7; d.disk_load_data=0x73; d.disk_load_valid=1;
    d.eval();
    bool fetched=false;
    for(unsigned i=0;i<1000 && !fetched;++i) {
        require(!d.disk_load_ready,"upload passed outstanding media read",s.cycles);
        s.step(); fetched=d.media_read_ready;
    }
    require(fetched && d.media_read_data==0x5a,"pending media read did not drain",s.cycles);
    require(!d.disk_load_ready,"upload passed held completed media request",s.cycles);
    d.media_read_valid=0; d.eval();
    for(unsigned i=0;!d.disk_load_ready && i<1000;++i) s.step();
    require(d.disk_load_ready,"upload did not resume after media drain",s.cycles);
    s.step(); d.disk_load_valid=0;
    for(unsigned i=0;!d.disk_load_done && i<1000;++i) s.step();
    require(d.disk_load_done && s.disk_read(7)==0x73,"upload after media drain",s.cycles);
    // An accepted media read drains on warm reset but must never report a
    // stale completion, even if reset releases before the SDRAM response.
    for(unsigned offset=0;offset<80;++offset) {
        d.media_read_addr=0x59fff; d.media_read_valid=1; s.step();
        s.idle(offset); d.reset=1; d.media_read_valid=0; s.step();
        d.reset=0;
        for(unsigned i=0;i<150;++i) {
            s.step(); require(!d.media_read_ready,"disk completion escaped warm reset",s.cycles);
        }
        require(s.disk_read(0x59fff)==0xa4,"disk read did not recover after reset",s.cycles);
    }
    // CPU and device both make progress under simultaneous held requests.
    for(unsigned round=0;round<20;++round) {
        d.address=0; d.write_cycle=0; d.memory_valid=1;
        d.media_read_addr=1; d.media_read_valid=1;
        bool memory=false, media=false;
        for(unsigned i=0;i<2000 && (!memory || !media);++i) {
            s.step();
            if(d.memory_done) {require(!memory && d.memory_rdata==0x63,"CPU arbitration response",s.cycles); memory=true;}
            if(d.media_read_ready) {require(!media && d.media_read_data==0x5a,"disk arbitration response",s.cycles); media=true;}
            require(!(d.memory_done && d.media_read_ready),"two SDRAM owners completed together",s.cycles);
        }
        require(memory && media,"CPU/disk arbitration starvation",s.cycles);
        d.memory_valid=0; d.media_read_valid=0; s.step();
    }
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
