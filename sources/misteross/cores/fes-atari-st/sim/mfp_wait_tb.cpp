// SPDX-License-Identifier: GPL-3.0-or-later
// Check the MFP access boundary against live native Timer B edges, rather
// than acknowledging late after an early read or repeating held side effects.
#include "Vst_io_sim_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

struct Test {
    Vst_io_sim_top dut;
    unsigned clocks=0, cpu_ticks=0, assertions=0;
    void require(bool good,const std::string& message) {
        ++assertions;
        if(!good) {std::cerr<<"MFP wait boundary: "<<message<<'\n';std::exit(1);}
    }
    void tick() {
        dut.cpu_cycle_ce=!dut.reset&&clocks%7==6;
        cpu_ticks+=dut.cpu_cycle_ce;
        dut.clk=0;dut.eval();dut.clk=1;dut.eval();++clocks;
    }
    void init() {
        dut.reset=1;dut.req=0;dut.write=0;dut.addr=0;dut.wdata=0;dut.byte_enable=0;
        dut.irq_ack=0;dut.irq_level=0;dut.screen_base=0;dut.resolution=0;dut.sync_mode=2;
        dut.monochrome=0;dut.controller_buttons=0;dut.mouse_valid=0;dut.mouse_dx=0;dut.mouse_dy=0;
        dut.mouse_buttons=0;dut.media_ready=0;dut.media_data=0;dut.media_valid=0;dut.dma_ready=0;
        for(unsigned k=0;k<5;++k)dut.keyboard[k]=0;
        tick();tick();dut.reset=0;tick();
    }
    void start(bool write,unsigned address,unsigned data=0,unsigned lanes=1) {
        dut.req=1;dut.write=write;dut.addr=address>>1;dut.wdata=data;dut.byte_enable=lanes;
        dut.eval();
    }
    uint16_t finish() {
        unsigned first=cpu_ticks,limit=clocks+100;
        while(!dut.ack&&clocks<limit)tick();
        require(dut.ack&&dut.selected,"bounded selected access acknowledges");
        require(cpu_ticks-first>=(dut.write?4u:5u),"native-cycle access delay");
        auto result=dut.rdata;dut.req=0;tick();require(!dut.ack,"request release clears acknowledgement");
        return result;
    }
    void write(unsigned offset,unsigned value) {start(true,0xfffa00+offset,value);finish();}
    unsigned read(unsigned offset) {start(false,0xfffa00+offset);return finish()&255;}
    void run() {
        init();
        // Dropping an uncompleted write must not change AER or leak its
        // completion into a later access.
        start(true,0xfffa03,0x5a);
        unsigned began=cpu_ticks;
        while(cpu_ticks-began<2) {tick();require(!dut.ack,"cancelled write has no early acknowledgement");}
        dut.req=0;tick();require(read(3)==0,"cancelled write has no side effect");
        start(true,0xfffa03,0xa5);tick();dut.reset=1;tick();
        dut.req=0;dut.reset=0;tick();require(read(3)==0,"reset cancels a pending write");
        write(0x21,3);write(0x1b,8); // Timer B event count, default falling DE edge
        while(dut.display_line!=63||dut.display_phase!=397)tick();
        require(dut.timer_b_level,"DE remains high before delayed falling edge");
        start(false,0xfffa21);
        const unsigned before=cpu_ticks;
        while(!dut.ack) {tick();require(cpu_ticks-before<20,"late Timer B read is bounded");}
        require((dut.rdata&255)==2,"Timer B sampled after the falling edge during the wait");
        require(cpu_ticks-before>=5,"Timer B read did not complete early");
        // Hold through another Timer B event: the sampled value remains
        // fixed, while the actual counter continues to run independently.
        unsigned hold=cpu_ticks;
        while(cpu_ticks-hold<512) {tick();require(dut.ack&&(dut.rdata&255)==2,"held read retains its one sampled value");}
        dut.req=0;tick();require(read(0x21)==1,"next transaction samples the advanced counter");
        start(false,0xfffa21,0,2);require((finish()&0xff00)==0xff00,"upper-only MFP access waits but drives no upper data");
        // Other peripherals retain their existing acknowledgement timing.
        start(true,0xff8800,7<<8,2);tick();require(dut.ack,"PSG access has no MFP delay");dut.req=0;tick();
        std::cout<<"MFP wait boundary PASS: "<<assertions<<" assertions\n";
    }
};
int main(int argc,char**argv) {Verilated::commandArgs(argc,argv);Test t;t.run();}
