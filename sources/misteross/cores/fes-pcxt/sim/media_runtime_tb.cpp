// SPDX-License-Identifier: GPL-3.0-or-later
// Real mailbox -> byte adapter -> ordered loader -> physical SDRAM model.
#include "Vmedia_runtime_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <cstdint>
#include <iostream>

static uint8_t byte_at(unsigned a, unsigned seed) { return uint8_t(a*13 + (a>>8) + seed); }
static uint32_t crc(unsigned seed) {
    uint32_t c=0xffffffffu;
    for(unsigned a=0;a<368640;++a) {
        c^=byte_at(a,seed);
        for(unsigned i=0;i<8;++i)c=(c&1)?(c>>1)^0xedb88320u:c>>1;
    }
    return c^0xffffffffu;
}

struct Test {
    Vmedia_runtime_top d;
    Sdram<Vmedia_runtime_top> ram;
    uint64_t cycles=0, cpu_completions=0;
    unsigned toggle=0;
    bool traffic=false, cpu_pending=false, require_live=false;
    Test() {
        d.clk=0; d.cold_reset=1; d.gpo=0; d.memory_valid=0; d.address=0x42;
        d.media_read_valid=0; d.media_read_addr=0; d.dq_sample=0;
        for(unsigned i=0;i<8;++i)tick();
        ram.cold_reset(); d.cold_reset=0;
        for(unsigned i=0;!d.initialized && i<20000;++i)tick();
        require(d.initialized,"SDRAM initialization",cycles);
    }
    void tick() {
        d.clk=0;d.eval();d.dq_sample=ram.tick(d);d.eval();
        const bool accepted=d.memory_valid && d.memory_ready;
        const bool done=d.memory_done;
        if(require_live)require(!d.exec_reset,"media operation reset execution",cycles);
        d.clk=1;d.eval();++cycles;
        if(accepted) {cpu_pending=true;d.memory_valid=0;}
        if(done) {require(cpu_pending,"unexpected CPU completion",cycles);cpu_pending=false;++cpu_completions;}
        if(traffic && !cpu_pending && !d.memory_valid)d.memory_valid=1;
    }
    void begin(unsigned op,unsigned index,unsigned arg) {
        const uint32_t cmd=(op<<24)|(index<<16)|arg;
        d.gpo=(toggle<<31)|cmd;
        for(unsigned i=0;i<3;++i)tick();
        toggle^=1;d.gpo=(toggle<<31)|cmd;
    }
    uint32_t finish() {
        for(unsigned i=0;i<4000;++i) {
            tick();
            if(((d.gpi>>23)&1)==toggle)return d.gpi;
        }
        require(false,"mailbox acknowledgement timeout",cycles);return 0;
    }
    uint16_t good(unsigned op,unsigned index,unsigned arg=0) {
        begin(op,index,arg);const auto v=finish();
        require((v&0xff000000u)==0xf5000000u && !(v&(1u<<22)),"unexpected mailbox error",cycles);
        return uint16_t(v);
    }
    void start(unsigned seed) {
        const auto sum=crc(seed);
        good(6,0,0xa000);good(6,1,5);good(6,2,sum&0xffff);good(6,3,sum>>16);
        require(d.unit_state==2 && !d.media_present,"loading exposed media",cycles);
    }
    void chunk_header(unsigned offset,unsigned length) {
        good(7,0,offset&0xffff);good(7,1,offset>>16);good(7,2,length);
    }
    void chunk(unsigned offset,unsigned length,unsigned seed) {
        chunk_header(offset,length);
        for(unsigned i=0;i<length;i+=2) {
            const auto value=unsigned(byte_at(offset+i,seed)) |
                (i+1<length ? unsigned(byte_at(offset+i+1,seed))<<8 : 0);
            good(8,i/2,value);
        }
    }
    void upload(unsigned seed) {
        start(seed);chunk(0,3,seed);
        for(unsigned offset=3;offset<368640;) {
            const unsigned length=std::min<unsigned>(512,368640-offset);
            chunk(offset,length,seed);offset+=length;
        }
        require(d.disk_loaded && !d.media_present && d.unit_state==2,"upload exposed uncommitted image",cycles);
        good(9,0);
        require(d.unit_state==3 && d.media_present,"commit did not publish disk",cycles);
        for(unsigned a=0;a<368640;++a)
            require(((ram.words.at(0x80000+(a>>1))>>(8*(a&1)))&255)==byte_at(a,seed),
                    "physical SDRAM bytes differ",cycles);
    }
};

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Test t;
    require(t.good(1,6)==1 && t.good(1,16)==1,"PCXT 1.1 identity",t.cycles);
    t.good(2,0,1);
    require(!t.d.exec_reset && t.d.unit_state==1 && !t.d.media_present,
            "empty drive prevented execution",t.cycles);
    t.require_live=true;t.traffic=true;
    t.upload(7);
    require(t.cpu_completions>100,"guest starved during upload",t.cycles);
    // Keep a completed FDC request asserted as if the card has not yet seen
    // its response. A new media data word must not overwrite that byte.
    t.d.media_read_addr=1;t.d.media_read_valid=1;
    for(unsigned i=0;!t.d.media_read_ready && i<1000;++i)t.tick();
    require(t.d.media_read_ready && t.d.media_read_data==byte_at(1,7),"old media read",t.cycles);
    t.start(19);t.chunk_header(0,3);
    const auto writes=t.ram.writes;
    t.begin(8,0,unsigned(byte_at(0,19)) | unsigned(byte_at(1,19))<<8);
    for(unsigned i=0;i<100;++i) {
        t.tick();
        require(((t.d.gpi>>23)&1)!=t.toggle && t.ram.writes==writes,
                "overwrite before held FDC request drained",t.cycles);
    }
    t.d.media_read_valid=0;
    const auto response=t.finish();
    require(!(response&(1u<<22)),"upload did not resume after read drain",t.cycles);
    t.good(8,1,byte_at(2,19));
    t.good(10,0);
    for(unsigned i=0;i<3;++i)t.tick();
    require(t.d.unit_state==1 && !t.d.media_present && !t.d.disk_loaded,"partial eject leaked image",t.cycles);
    const auto before=t.cpu_completions;
    t.upload(19);
    require(t.cpu_completions>before+100,"guest starved during replacement",t.cycles);
    t.good(10,0);
    for(unsigned i=0;i<3;++i)t.tick();
    require(t.d.unit_state==1 && !t.d.media_present && !t.d.disk_loaded,"live eject retained disk",t.cycles);
    // A cold reset during a blocked DATA request revokes eligibility and
    // completes the interrupted protocol request with an error.
    t.start(7);t.chunk_header(0,2);
    t.d.media_read_valid=1;t.d.media_read_addr=0;
    t.begin(8,0,unsigned(byte_at(0,7)) | unsigned(byte_at(1,7))<<8);
    for(unsigned i=0;i<20;++i)t.tick();
    require(((t.d.gpi>>23)&1)!=t.toggle,"blocked data unexpectedly acknowledged",t.cycles);
    t.d.cold_reset=1;t.ram.cold_reset();
    const auto interrupted=t.finish();
    require((interrupted&(1u<<22)) && uint16_t(interrupted)==4,"cold reset lost interrupted request",t.cycles);
    t.d.media_read_valid=0;
    for(unsigned i=0;i<8;++i)t.tick();
    require(t.d.unit_state==1 && !t.d.disk_loaded && !t.d.media_present,"cold reset retained media",t.cycles);
    t.d.cold_reset=0;
    for(unsigned i=0;!t.d.initialized && i<20000;++i)t.tick();
    require(t.d.initialized && t.good(5,5)==1,"cold reset did not recover empty unit",t.cycles);
    std::cout<<"PASS PC/XT live media: two full mailbox uploads, odd chunks, read drain, eject, reset; "
             <<t.cpu_completions<<" guest memory completions, "<<t.ram.refreshes<<" refreshes\n";
}
