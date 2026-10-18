#pragma once
// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vmotherboard_top.h"
#include "verilated.h"
#include <array>
#include <vector>
#include <iostream>
#include <stdexcept>
#include <string>
static void check(bool value, const std::string& message) {
    if (!value) throw std::runtime_error(message);
}
struct Bench {
    Vmotherboard_top d;
    std::array<uint8_t, 1<<20> mem{};
    unsigned ticks=0, memory_requests=0, transactions=0, delay=0, wait=3;
    unsigned pending_address=0, pending_data=0;
    bool pending=false, pending_write=false, timer=true;
    struct DmaByte {unsigned channel,data,tick;bool write,tc;};
    std::vector<DmaByte> dma_bytes;
    unsigned refreshes=0;
    bool previous_commit=false;
    bool watch_accept=false, accepted_code=false;
    void tick() {
        bool ready = watch_accept && d.code_valid && d.code_ready;
        d.memory_done=0;
        d.memory_ready=!pending && ticks%3!=0;
        if (pending && delay--==0) {
            if (pending_write) mem[pending_address]=pending_data;
            d.memory_rdata=mem[pending_address]; d.memory_done=1; pending=false;
        }
        d.guest_ce=timer && ticks%21==0;
        d.clk=0; d.eval();
        if (d.memory_valid && d.memory_ready) {
            check(!pending,"duplicate memory request");
            pending=true; delay=wait; pending_address=d.address;
            pending_data=d.wdata; pending_write=d.write_cycle;
            check(d.kind==0,"I/O leaked to storage");
            check(d.address<0xa0000 || (d.address>=0xf0000 && !d.write_cycle),"invalid storage decode");
            ++memory_requests;
        }
        bool commit = !d.reset && ((d.observed_request>>33)&1);
        if(commit && !previous_commit && ((d.observed_request>>28)&3)==3) {
            check((d.observed_request>>35)&1,"DMA missing AEN");
            unsigned ch=(d.observed_request>>37)&3;check(ch!=0,"DMA0 escaped to card");
            dma_bytes.push_back({ch,unsigned((d.observed_request>>20)&255),ticks,
                                 bool((d.observed_request>>30)&1),bool((d.observed_request>>39)&1)});
        }
        previous_commit=commit;
        if(!d.reset && d.refresh_done)++refreshes;
        d.clk=1; d.eval(); ++ticks;
        if (ready && !d.code_ready) accepted_code=true;
    }
    void idle(unsigned n=30) { while(n--) tick(); }
    void reset() {
        d.reset=1; previous_commit=false; dma_bytes.clear(); refreshes=0; d.cpu_bus_lock=0; d.external_drq=0; d.cycle_valid=0; d.external_irq=0; d.keyboard_clock=1;d.keyboard_data=0;
        d.external_keyboard=1; d.code_valid=0; d.code=0; watch_accept=false; accepted_code=false;
        d.switches=0x2d;d.parity_error=0;d.channel_error=0; d.inject=0; pending=false; d.memory_done=0;
        idle(); d.reset=0; idle();
    }
    uint8_t bus(unsigned kind, unsigned address, bool wr=false, unsigned data=0, unsigned fault=0) {
        for(unsigned n=0;!d.cycle_ready && n<20000;n++) tick();
        check(d.cycle_ready,"admission timeout");
        d.cycle_valid=1; d.cycle_address=address; d.cycle_kind=kind;
        d.cycle_write=wr; d.cycle_wdata=data; d.cycle_lock=kind==2;
        tick(); d.cycle_valid=0;
        unsigned deadline=ticks+20000;
        while(!d.cycle_done && ticks<deadline) tick();
        check(d.cycle_done,"bus timeout"); check(d.cycle_fault==fault,"bus fault");
        uint8_t result=d.cycle_rdata;
        ++transactions; tick(); return result;
    }
    void out(unsigned port, unsigned value) { bus(1,port,true,value); }
    uint8_t in(unsigned port) { return bus(1,port); }
    void init_pic(unsigned icw1=0x13,unsigned icw4=1) {
        out(0x20,icw1);out(0x21,8);out(0x21,icw4);out(0x21,0xff);
    }
    uint8_t ack() { check(bus(2,0)==0xff,"first INTA must float"); return bus(2,0); }
    uint8_t isr() { out(0x20,0x0b);return in(0x20); }
    uint8_t irr() { out(0x20,0x0a);return in(0x20); }
    void irq(unsigned bits) { d.external_irq=bits>>2; idle(); }
    void keyboard_bit(bool data) {
        d.keyboard_clock=1;d.keyboard_data=data;idle(4000);
        d.keyboard_clock=0;idle(4000);
    }
    void key(unsigned code) {
        keyboard_bit(false); // RTS, ignored before start
        keyboard_bit(true);
        for(unsigned i=0;i<8;i++)keyboard_bit((code>>i)&1);
        d.keyboard_clock=1;d.keyboard_data=0;idle(30);
    }
    void clear_key() { out(0x61,0xc0);out(0x61,0x40); }
    unsigned latch(unsigned ch) {
        out(0x43,ch<<6); unsigned lo=in(0x40+ch); return lo | (unsigned(in(0x40+ch))<<8);
    }
    void pit(unsigned ch,unsigned mode,unsigned count,bool bcd=false) {
        out(0x43,(ch<<6)|0x30|(mode<<1)|bcd);
        out(0x40+ch,count&255);out(0x40+ch,count>>8);
    }
};
