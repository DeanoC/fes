// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vfes_de25_menu_cdc.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <vector>
static void check(bool value,const char* why) { if(!value)throw std::runtime_error(why); }
struct Packet { uint32_t data; uint8_t address,strobe; bool write; };
struct Bench {
    Vfes_de25_menu_cdc d;
    uint64_t time=0,cpu_edges=0,replies=0,phase;
    bool cpu_run=true,pixel_run=true,hold_ready=false,hold_result=false;
    bool pending=false,last_error=false;
    uint32_t expected=0,last_data=0,delay=0;
    std::vector<Packet> commands;
    explicit Bench(uint64_t offset):phase(offset) {
        d.clk=0;d.pixel_clk=0;d.reset_n=0;d.pixel_reset_n=0;
        d.address=0;d.read=0;d.write=0;d.byteenable=15;d.writedata=0;
        d.command_ready=1;d.result_valid=0;d.result_error=0;d.result_data=0;
        advance(4000);d.reset_n=1;d.pixel_reset_n=1;advance(12000);
    }
    bool step() {
        bool cpu=cpu_run && ((time/297)&1),pixel=pixel_run && (((time+phase)/400)&1);
        bool cr=!d.clk && cpu,pr=!d.pixel_clk && pixel;
        d.command_ready=!hold_ready;
        d.result_valid=pending && !delay && !hold_result;
        d.result_data=expected;d.result_error=pending && commands.back().address==0xff;
        d.eval();
        bool accept=cr && (d.read||d.write) && !d.waitrequest;
        bool issue=pr && d.command_valid && d.command_ready;
        bool result=pr && d.result_valid;
        Packet packet{d.command_wdata,d.command_address,d.command_wstrb,bool(d.command_write)};
        d.clk=cpu;d.pixel_clk=pixel;d.eval();
        if(pr) {
            if(result)pending=false;
            if(delay)delay--;
            if(issue) {
                check(!pending,"overlapping pixel commands");commands.push_back(packet);
                expected=packet.data ^ (uint32_t(packet.address)<<24) ^ (uint32_t(packet.strobe)<<16) ^ uint32_t(packet.write);
                delay=2;pending=true;
            }
        }
        if(cr) {
            cpu_edges++;
            if(d.readdatavalid||d.writeresponsevalid) {
                replies++;last_error=d.response!=0;last_data=d.readdata;
            }
        }
        check(++time<20000000,"CDC model total time cap");
        return accept;
    }
    void advance(uint64_t ticks) { while(ticks--)step(); }
    void begin(Packet p) {
        check(!d.waitrequest,"CPU began before prior acknowledgement withdrew");
        d.address=p.address;d.byteenable=p.strobe;d.writedata=p.data;d.write=p.write;d.read=!p.write;
        bool accepted=false;for(unsigned n=0;n<1000&&!accepted;n++)accepted=step();
        check(accepted,"CPU request acceptance bound");d.read=0;d.write=0;
    }
    void finish(uint64_t before) {
        uint64_t start=cpu_edges;
        for(unsigned n=0;n<50000 && replies==before;n++)step();
        check(replies==before+1,"CPU reply missing or duplicated");
        check(cpu_edges-start<=68,"CPU response exceeded compiled timeout");
        for(unsigned n=0;n<20000 && d.waitrequest;n++)step();
        check(!d.waitrequest,"CPU return handshake blocked");
    }
    void round(Packet p) {
        uint64_t prior=replies;size_t count=commands.size();begin(p);finish(prior);
        check(commands.size()==count+1,"command replay or loss");
        auto q=commands.back();check(q.data==p.data && q.address==p.address && q.strobe==p.strobe && q.write==p.write,"torn command packet");
        check(last_error==(p.address==0xff) && last_data==(last_error?0xffffffffU:expected),"torn reply packet");
    }
    void refused() {
        size_t count=commands.size();uint64_t prior=replies;begin({0x12345678,8,15,true});finish(prior);
        advance(8000);check(last_error && last_data==0xffffffffU && d.failed && commands.size()==count,"retired bridge executed a new command");
    }
};
int main() {
    try {
        unsigned cases=0;
        // Exact 100/74.25 MHz ratio, unrelated phases, every packet bit and
        // returned error; the pixel execution oracle is independent of RTL.
        for(unsigned phase: {0U,17U,149U,296U,399U}) {
            Bench b(phase);uint32_t random=0x12345678;
            for(unsigned n=0;n<128;n++) {
                random=random*1664525+1013904223;
                b.round({random,uint8_t(n*37),uint8_t(n%16),bool(n&1)});
            }
            check(!b.d.failed,"healthy CDC retired");cases++;
        }
        for(unsigned mode=0;mode<5;mode++) {
            Bench b(31);b.round({17,4,15,false});uint64_t prior=b.replies;
            if(mode==0)b.pixel_run=false;
            if(mode==1)b.hold_ready=true;
            if(mode==2)b.hold_result=true;
            if(mode==3) { b.d.pixel_reset_n=0;b.step();b.d.pixel_reset_n=1;b.advance(10000);b.refused();cases++;continue; }
            if(mode==4) { b.cpu_run=false;b.d.pixel_reset_n=0;b.advance(30);b.d.pixel_reset_n=1;b.advance(3000);b.cpu_run=true;b.advance(3000);b.refused();cases++;continue; }
            size_t count=b.commands.size();b.begin({0xaabbccdd,12,9,true});b.finish(prior);
            check(b.last_error && b.d.failed,"missing clock/ready/result did not retire");
            b.pixel_run=true;b.hold_ready=false;b.hold_result=false;b.advance(12000);
            check(b.commands.size()<=count+1,"uncertain request replayed after timeout");
            b.refused();cases++;
        }
        // A pixel reset while CPU is stopped must not adopt its outstanding
        // high request as a new command after reset release.
        {
            Bench b(91);b.round({1,4,15,false});uint64_t prior=b.replies;
            b.begin({2,8,15,true});b.cpu_run=false;b.d.pixel_reset_n=0;b.advance(1000);
            b.pending=false;b.delay=0;size_t count=b.commands.size();b.d.pixel_reset_n=1;b.advance(16000);
            check(b.commands.size()==count,"pixel reset adopted an old high request");
            b.cpu_run=true;b.finish(prior);check(b.last_error && b.d.failed,"lost pixel epoch reported success");
            b.refused();cases++;
        }
        // CPU clock can pause without losing ownership: the held response
        // completes once when it resumes, with no duplicate pixel execution.
        {
            Bench b(193);uint64_t prior=b.replies;b.begin({0x87654321,16,7,false});
            b.cpu_run=false;b.advance(20000);check(b.commands.size()==1,"paused CPU repeated request");
            b.cpu_run=true;b.finish(prior);check(!b.last_error && b.last_data==b.expected && !b.d.failed,"paused CPU lost held response");cases++;
        }
        std::cout<<"PASS "<<cases<<" bounded two-clock CDC cases; 640 exact request/reply packets, stopped clocks, short reset, in-flight reset and no replay\n";
        return 0;
    } catch(const std::exception& e) {std::cerr<<"menu CDC: "<<e.what()<<"\n";return 1;}
}
