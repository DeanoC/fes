// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vst_media_port.h"
#include "verilated.h"
#include <cstdio>
#include <stdexcept>
static void check(bool ok, const char* why) { if (!ok) throw std::runtime_error(why); }
struct Rig {
    Vst_media_port d;
    void tick() { d.clk=0; d.eval(); d.clk=1; d.eval(); d.clk=0; d.eval(); }
    Rig() {
        d.cold_reset=1; d.source_req=0; d.memory_ready=0;
        d.source_addr0=0x1234; d.source_addr1=0x5678;
        d.source_data0=0x1357; d.source_data1=0x2468;
        d.source_enable0=1; d.source_enable1=2;
        tick(); d.cold_reset=0; tick();
    }
    void complete(unsigned owner) {
        d.memory_ready=1; d.eval();
        check(d.source_ready==owner,"completion delivered to wrong owner");
        tick(); check(!d.memory_req && !d.source_ready,"completion failed to lower request");
        d.memory_ready=0; tick();
    }
};
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    try {
        Rig r; auto& d=r.d;
        d.source_req=3; r.tick();
        check(d.memory_req && d.memory_addr==0x1234 && d.memory_data==0x1357 && d.memory_enable==1,
              "first contender not admitted");
        d.source_addr0=0x9999; d.source_data0=0xffff; d.source_enable0=3;
        for(unsigned n=0;n<19;++n) {r.tick();check(d.memory_req && d.memory_addr==0x1234 && d.memory_data==0x1357 && d.memory_enable==1 && !d.source_ready,"stalled payload/owner changed");}
        r.complete(1); r.tick();
        check(d.memory_req && d.memory_addr==0x5678 && d.memory_data==0x2468 && d.memory_enable==2,
              "second contender starved");
        r.complete(2);
        for(unsigned n=0;n<10;++n) {r.tick();check(!d.memory_req,"held request was repeated");}
        // A withdrawn owner may reassert a new job before old physical ACK.
        // It must receive no stale ACK; only the next latched job completes.
        d.source_req=0; r.tick(); d.source_req=1; r.tick();
        check(d.memory_req && d.memory_addr==0x9999,"rearmed job missing");
        d.source_req=0; r.tick(); d.source_addr0=0xaaaa; d.source_req=1; r.tick();
        check(d.memory_addr==0x9999,"withdrawn physical job was replaced");
        r.complete(0); r.tick();
        check(d.memory_req && d.memory_addr==0xaaaa,"new job not admitted after canceled completion");
        r.complete(1); d.source_req=0; r.tick();
        // Simultaneous withdrawal/ACK cannot reach the canceled source.
        d.source_req=2; r.tick(); d.source_req=0; r.complete(0);
        d.source_req=3; r.tick(); check(d.memory_req,"final contenders not admitted");
        d.cold_reset=1; d.memory_ready=1; d.eval();
        check(!d.memory_req && !d.source_ready,"cold reset exposed a transaction");
        r.tick(); d.source_req=0; d.memory_ready=0; d.cold_reset=0; r.tick();
        check(!d.memory_req && !d.source_ready,"cold reset retained ownership");
        std::puts("PASS ST media port: contention, stable held payload, one ACK, cancellation, rearm gap and cold reset");
        return 0;
    } catch(const std::exception& e) {std::fprintf(stderr,"FAIL media port: %s\n",e.what());return 1;}
}
