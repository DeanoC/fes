// SPDX-License-Identifier: GPL-3.0-or-later
// PC/XT 1.1 identity, complete read-only media transaction and cold reset.
#include "Vfes_computer_mailbox_pcxt.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

static void require(bool ok, const char *what) {
    if (!ok) { std::cerr << "FAIL: " << what << '\n'; std::exit(1); }
}

struct Test {
    Vfes_computer_mailbox_pcxt d;
    uint32_t toggle = 0;
    std::vector<uint8_t> image = std::vector<uint8_t>(368640, 0);
    uint64_t writes = 0;
    Test() {
        d.clk=0; d.gpo=0; d.media_reset=0; d.media_write_ready=1; d.media_write_busy=0;
        for (int i=0;i<4;++i) d.build_id[i]=0;
        d.eval(); for(int i=0;i<6;++i) tick();
    }
    void tick() {
        if (d.media_write_ready && d.media_write_enable) {
            require(d.media_write_addr<image.size(), "write address outside image");
            if (d.media_write_enable&1) { image[d.media_write_addr]=d.media_write_data; ++writes; }
            if (d.media_write_enable&2) {
                require(d.media_write_addr+1<image.size(), "high byte outside image");
                image[d.media_write_addr+1]=d.media_write_data>>8; ++writes;
            }
        }
        d.clk=1; d.eval(); d.clk=0; d.eval();
    }
    void begin_request(uint8_t op, uint8_t index, uint16_t arg) {
        const uint32_t command=(uint32_t(op)<<24)|(uint32_t(index)<<16)|arg;
        d.gpo=(toggle<<31)|command;
        for(int i=0;i<3;++i) tick();
        toggle^=1; d.gpo=(toggle<<31)|command;
    }
    uint32_t finish_request() {
        for(int i=0;i<40;++i) {
            tick();
            if (((d.gpi>>23)&1)==toggle) return d.gpi;
        }
        require(false,"mailbox timeout"); return 0;
    }
    uint16_t good(uint8_t op,uint8_t index,uint16_t arg=0) {
        begin_request(op,index,arg); const uint32_t value=finish_request();
        require((value&0xff000000u)==0xf5000000u && !(value&(1u<<22)),"unexpected mailbox error");
        return value;
    }
    void bad(uint8_t op,uint8_t index,uint16_t arg,uint16_t error) {
        begin_request(op,index,arg); const uint32_t value=finish_request();
        require((value&(1u<<22)) && uint16_t(value)==error,"unexpected error code");
    }
};

static uint8_t payload(unsigned a) { return uint8_t(a*13+7); }
static uint32_t crc32() {
    uint32_t c=0xffffffff;
    for(unsigned a=0;a<368640;++a) {
        c^=payload(a);
        for(int i=0;i<8;++i) c=(c&1) ? (c>>1)^0xedb88320u : c>>1;
    }
    return c^0xffffffffu;
}

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Test t;
    require(t.good(1,6)==1 && t.good(1,7)==1 && t.good(1,16)==1,"1.1 capability identity");
    t.bad(1,17,0,2);
    require(t.good(5,0)==0xa000 && t.good(5,1)==5 && t.good(5,2)==0xa000 &&
            t.good(5,3)==5 && t.good(5,4)==512 && t.good(5,5)==1,"PC floppy geometry and empty state");
    require(t.good(2,0,1)==0 && !t.d.exec_reset,"release with empty drive");
    // Invalid size is rejected without completing the staged header; eject
    // allows the caller to recover and supply the exact-sized image.
    t.good(6,0,0); t.good(6,1,0); t.good(6,2,0); t.bad(6,3,0,3);
    t.good(10,0); require(t.d.unit0_state==1,"eject incomplete header");
    const uint32_t crc=crc32();
    t.good(6,0,0xa000); t.good(6,1,5); t.good(6,2,crc); t.good(6,3,crc>>16);
    require(t.d.unit0_state==2,"begin does not expose incomplete media");
    for(unsigned offset=0;offset<t.image.size();offset+=512) {
        t.good(7,0,offset); t.good(7,1,offset>>16); t.good(7,2,512);
        for(unsigned i=0;i<512;i+=2)
            t.good(8,i/2,uint16_t(payload(offset+i)) | uint16_t(payload(offset+i+1))<<8);
    }
    require(t.writes==t.image.size(),"upload writes exactly once");
    for(unsigned a=0;a<t.image.size();++a) require(t.image[a]==payload(a),"image data differs");
    t.good(9,0); require(t.d.unit0_state==3 && t.d.unit0_size==t.image.size(),"commit publishes image");
    // A physical cold reset invalidates READY without changing execution or
    // identity. After reset the old transfer cannot be committed again.
    t.d.media_reset=1; for(int i=0;i<4;++i)t.tick();
    require(t.d.unit0_state==1 && t.d.unit0_size==0 && !t.d.exec_reset,"reset invalidation/execution");
    t.d.media_reset=0; for(int i=0;i<5;++i)t.tick();
    require(t.good(1,16)==1 && t.good(5,5)==1,"identity or empty state changed after reset");
    t.bad(9,0,0,4);
    // Partially collected header is cancelled and a fresh Begin starts at 0.
    t.good(6,0,0xa000); t.good(6,1,5);
    t.d.media_reset=1; for(int i=0;i<4;++i)t.tick();
    t.d.media_reset=0; for(int i=0;i<5;++i)t.tick();
    t.bad(6,2,crc,2); t.good(6,0,0xa000); t.good(6,1,5); t.good(6,2,crc); t.good(6,3,crc>>16);
    t.good(7,0,0); t.good(7,1,0); t.good(7,2,2);
    t.d.media_write_ready=0;
    t.begin_request(8,0,0x1234);
    for(int i=0;i<8;++i)t.tick();
    require(((t.d.gpi>>23)&1)!=t.toggle && t.d.media_write_enable,"backpressure did not hold request");
    const auto before=t.writes;
    t.d.media_reset=1; t.d.eval();
    require(!t.d.media_write_enable,"reset did not suppress pending write");
    const uint32_t interrupted=t.finish_request();
    require((interrupted&(1u<<22)) && uint16_t(interrupted)==4 && t.writes==before,
            "reset accepted or lost interrupted media request");
    t.d.media_write_ready=1; for(int i=0;i<4;++i)t.tick();
    t.d.media_reset=0; for(int i=0;i<5;++i)t.tick();
    require(t.good(5,5)==1,"reset left interrupted medium ready");
    t.bad(9,0,0,4); t.good(6,0,0xa000); t.good(10,0);
    require(t.d.unit0_state==1 && !t.d.exec_reset,"restart/eject changed execution");
    // A one-clock physical reset can coincide with an asynchronous HPS
    // toggle. The synchronizer may discover it after the pulse itself.
    t.d.media_reset=1;
    t.toggle^=1; t.d.gpo=(t.toggle<<31)|(6u<<24)|0xa000u;
    t.tick(); t.d.media_reset=0;
    const uint32_t raced=t.finish_request();
    require((raced&(1u<<22)) && uint16_t(raced)==4,
            "reset race accepted a synchronized request");
    for(int i=0;i<5;++i)t.tick();
    require(t.good(5,5)==1,"reset race staged a stale header");
    std::cout << "PASS PC/XT mailbox: 1.1 identity, " << t.writes << " image bytes, reset and live eject\n";
}
