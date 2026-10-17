// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vmotherboard_top.h"
#include "verilated.h"
#include <array>
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
    void tick() {
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
        d.clk=1; d.eval(); ++ticks;
    }
    void idle(unsigned n=30) { while(n--) tick(); }
    void reset() {
        d.reset=1; d.cycle_valid=0; d.external_irq=0; d.keyboard_irq=0;
        d.pit_gate2=0; d.inject=0; pending=false; d.memory_done=0;
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
    void irq(unsigned bits) { d.external_irq=bits>>2; d.keyboard_irq=(bits>>1)&1; idle(); }
    unsigned latch(unsigned ch) {
        out(0x43,ch<<6); unsigned lo=in(0x40+ch); return lo | (unsigned(in(0x40+ch))<<8);
    }
    void pit(unsigned ch,unsigned mode,unsigned count,bool bcd=false) {
        out(0x43,(ch<<6)|0x30|(mode<<1)|bcd);
        out(0x40+ch,count&255);out(0x40+ch,count>>8);
    }
};
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    try {
        Bench b; b.reset();
        for (unsigned a: {0u,0x9ffffu}) { b.bus(0,a,true,0x5a);check(b.bus(0,a)==0x5a,"RAM boundary"); }
        for (unsigned a: {0xf0000u,0xffff0u,0xfffffu}) {
            b.mem[a]=0xa5; b.bus(0,a,true,0x11);check(b.bus(0,a)==0xa5,"ROM write protection");
        }
        auto before=b.memory_requests;
        for(unsigned a: {0xa0000u,0xb8000u,0xc0000u,0xeffffu}) {
            b.bus(0,a,true,0x55);check(b.bus(0,a)==0xff,"ISA hole");
        }
        check(b.memory_requests==before,"ISA aperture reached RAM");
        b.bus(0,0xc8000,true,0x42);check(b.bus(0,0xc8000)==0x42,"option memory");
        b.out(0x300,0x67);check(b.in(0x700)==0x67,"ISA alias");
        b.init_pic();check(b.in(0x21)==0xff,"PIC mask");
        b.out(0x43f,0xdd);check(b.in(0x21)==0xdd,"PIC aliases");
        check(b.in(0x121)==0xff,"PIC A8 decode");b.out(0x21,0xff);
        b.irq(0x22);check(!b.d.intr,"masked interrupt");check((b.irr()&0x22)==0x22,"masked IRR");
        b.out(0x21,0xdd);b.idle();check(b.d.intr,"unmasked IRQ");
        check(b.ack()==9,"IRQ1 priority/vector");check(b.isr()==2,"IRQ1 ISR");
        check(!b.d.intr,"lower priority blocked in service");b.irq(0x20);
        b.out(0x20,0x20);b.idle();check(b.d.intr,"EOI releases pending IRQ5");
        check(b.ack()==13,"IRQ5 vector");check(b.isr()==0x20,"IRQ5 ISR");b.irq(0);
        b.out(0x20,0x65);check(b.isr()==0,"specific EOI");
        check(b.ack()==15,"spurious IRQ7 vector");check(b.isr()==0,"spurious ISR unchanged");
        // Real ISA endpoint IRQ reaches PIC, and a held edge is not retriggered.
        b.out(0x301,1);b.idle();check(b.d.intr,"ISA IRQ5 delivery");
        check(b.ack()==13,"ISA IRQ5 vector");b.out(0x20,0x20);b.idle();check(!b.d.intr,"held edge retrigger");
        b.out(0x301,0);
        // Automatic EOI and level-sensitive reassertion.
        b.init_pic(0x13,3);b.out(0x21,0xdf);b.irq(0x20);
        check(b.ack()==13,"AEOI vector");check(b.isr()==0,"AEOI clears ISR");b.irq(0);
        b.init_pic(0x1b,1);b.out(0x21,0xdf);b.irq(0x20);
        check(b.ack()==13,"level vector");b.out(0x20,0x20);b.idle();check(b.d.intr,"held level retrigger");
        b.irq(0);b.reset();
        b.init_pic();b.out(0x21,0xdd);b.out(0x20,0xc1);b.irq(0x22);
        check(b.ack()==13,"rotated priority");b.irq(2);b.out(0x20,0x20);
        check(b.ack()==9,"rotated pending IRQ1");b.irq(0);b.out(0x20,0x20);
        b.init_pic();b.out(0x21,0xdf);b.irq(0x20);b.out(0x20,0x0c);
        check(b.in(0x20)==0x85,"poll interrupt number");check(b.isr()==0x20,"poll enters service");
        b.irq(0);b.out(0x20,0x20);b.out(0x20,0x0c);check(b.in(0x20)==0,"empty poll");
        b.reset();
        b.timer=false;b.d.pit_gate2=1;b.pit(2,0,1000);b.timer=true;b.idle(1000);
        unsigned count=b.latch(2);check(count<1000 && count>900,"PIT mode0 count");
        // Latch must hold both bytes despite time passing between reads.
        b.out(0x43,0x80);unsigned lo=b.in(0x42);b.idle(3000);unsigned hi=b.in(0x42);
        check((lo|(hi<<8))>b.latch(2)+20,"PIT latched read");
        b.d.pit_gate2=0;b.idle(200);count=b.latch(2);b.idle(2000);check(b.latch(2)==count,"PIT gate stops mode0");
        b.d.pit_gate2=1;b.idle(90000);check(b.d.pit_out&4,"PIT mode0 terminal output");
        b.pit(2,0,0);b.idle(500);check(b.latch(2)>65000,"PIT zero means 65536");
        b.pit(2,0,0x0100,true);b.idle(1000);count=b.latch(2);check(count<0x100 && (count&15)<=9,"BCD borrow");
        // Mode 2/3 periodic outputs, including odd reload; clocks keep running
        // while an unrelated storage request is waiting.
        for(unsigned mode: {2u,3u}) for(unsigned reload: {10u,11u}) {
            b.pit(2,mode,reload);b.idle(2000);
            unsigned rises=0, last=b.d.pit_out&4, edge=0;
            for(unsigned i=0;i<10000;i++) {
                b.tick();unsigned now=b.d.pit_out&4;
                if(now!=last) {
                    if(edge) {
                        unsigned expected=mode==2 ? (last ? reload-1 : 1) : (last ? (reload+1)/2 : reload/2);
                        check(i-edge==expected*84,"PIT waveform duty");
                    }
                    edge=i;if(now)++rises;
                }
                last=now;
            }
            check(rises>=10 && rises<=12,"PIT periodic frequency");
        }
        // Hardware one-shot and software/hardware strobes, all on channel 2.
        for(unsigned mode: {1u,4u,5u}) {
            b.d.pit_gate2=0;b.pit(2,mode,10);b.idle(500);
            check(b.d.pit_out&4,"triggered mode idle output");
            b.d.pit_gate2=1;
            unsigned low=0,fall=0,rise=0;
            for(unsigned i=0;i<3000;i++) {
                b.tick();
                if(!(b.d.pit_out&4)) {if(!low)fall=i;++low;}
                else if(low&&!rise)rise=i;
            }
            check(low==(mode==1?840u:84u) && rise>fall,"PIT one-shot/strobe pulse");
        }
        b.pit(1,2,100);b.idle(500);unsigned refresh=b.latch(1);
        check(refresh>0 && refresh<=100,"refresh channel count");
        b.init_pic();b.out(0x21,0xfe);b.pit(0,2,20);b.wait=5000;
        b.bus(0,0x1234);check(b.d.intr,"PIT did not run during memory wait");
        // Mode 2 deliberately lowers IRQ for a clock. Acknowledge just after
        // the next rising edge, avoiding a legitimate spurious IRQ7 window.
        for(unsigned n=0;(b.d.pit_out&1)&&n<2000;n++)b.tick();
        check(!(b.d.pit_out&1),"timer low pulse timeout");
        for(unsigned n=0;!(b.d.pit_out&1)&&n<2000;n++)b.tick();
        check(b.d.pit_out&1,"timer rising edge timeout");
        b.idle(4);auto vec=b.ack();check(vec==8,"timer IRQ0 vector="+std::to_string(vec));b.out(0x20,0x20);
        b.d.inject=1;b.bus(1,0x300,false,0,1);b.d.inject=0;
        // The latched storage request survives changes on the upstream bus;
        // reset cancels it before the storage model commits the byte.
        b.reset();b.wait=5000;b.mem[0x1234]=0;
        b.d.cycle_valid=1;b.d.cycle_kind=0;b.d.cycle_address=0x1234;
        b.d.cycle_write=1;b.d.cycle_wdata=0x77;b.tick();b.d.cycle_valid=0;
        b.d.cycle_address=0x2345;b.d.cycle_wdata=0x88;b.idle(100);
        check(b.pending && b.d.address==0x1234 && b.d.wdata==0x77,"storage payload not held");
        b.reset();b.wait=3;b.idle(100);
        check(b.mem[0x1234]==0 && !b.d.cycle_done,"reset storage cancellation");
        // Sweep reset across every phase of the local write and its release.
        for(unsigned phase=0;phase<20;phase++) {
            b.reset();b.init_pic();b.d.cycle_valid=1;b.d.cycle_kind=1;
            b.d.cycle_address=0x21;b.d.cycle_write=1;b.d.cycle_wdata=0;
            b.tick();b.d.cycle_valid=0;b.idle(phase);b.reset();
            check(!b.d.cycle_done && !b.d.intr,"reset phantom completion/IRQ");
            check(b.in(0x21)==0xff,"reset phantom write");
        }
        std::cout<<"PASS motherboard: "<<b.transactions<<" transactions, memory/PIC/PIT/ISA, "<<b.ticks<<" fabric clocks\n";
    } catch(const std::exception& e) {std::cerr<<"FAIL motherboard: "<<e.what()<<"\n";return 1;}
}
