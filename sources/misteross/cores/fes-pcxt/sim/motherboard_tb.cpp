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
        d.clk=1; d.eval(); ++ticks;
        if (ready && !d.code_ready) accepted_code=true;
    }
    void idle(unsigned n=30) { while(n--) tick(); }
    void reset() {
        d.reset=1; d.cycle_valid=0; d.external_irq=0; d.keyboard_clock=1;d.keyboard_data=0;
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
        b.out(0x43f,0xdb);check(b.in(0x21)==0xdb,"PIC aliases");
        check(b.in(0x121)==0xff,"PIC A8 decode");b.out(0x21,0xff);
        b.irq(0x24);check(!b.d.intr,"masked interrupt");check((b.irr()&0x24)==0x24,"masked IRR");
        b.out(0x21,0xdb);b.idle();check(b.d.intr,"unmasked IRQ");
        check(b.ack()==10,"IRQ2 priority/vector");check(b.isr()==4,"IRQ2 ISR");
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
        b.init_pic();b.out(0x21,0xdb);b.out(0x20,0xc2);b.irq(0x24);
        check(b.ack()==13,"rotated priority");b.irq(4);b.out(0x20,0x20);
        check(b.ack()==10,"rotated pending IRQ2");b.irq(0);b.out(0x20,0x20);
        b.init_pic();b.out(0x21,0xdf);b.irq(0x20);b.out(0x20,0x0c);
        check(b.in(0x20)==0x85,"poll interrupt number");check(b.isr()==0x20,"poll enters service");
        b.irq(0);b.out(0x20,0x20);b.out(0x20,0x0c);check(b.in(0x20)==0,"empty poll");
        b.reset();
        // PPI mode 0, switch multiplexer, aliases and direction changes.
        check(b.in(0x61)==0xff && b.d.keyboard_clock_low,"PPI reset/input state");
        check(!b.d.parity_enable && !b.d.channel_check_enable,"reset check enables");
        b.out(0x63,0x99);b.out(0x61,0x40);
        check(b.in(0x47d)==0x40 && (b.in(0x62)&15)==13,"PPI alias/low switches");
        b.out(0x47d,0x48);check((b.in(0x47e)&15)==2,"high switches");
        check(b.in(0x162)==0xff,"PPI A8 decode");
        b.d.parity_error=1;b.d.channel_error=1;b.idle();
        check((b.in(0x62)&0xc0)==0xc0,"PPI error status");
        check(b.d.parity_enable && b.d.channel_check_enable,"active low enables");
        b.out(0x61,0x70);check(!b.d.parity_enable && !b.d.channel_check_enable,"disable checks");
        b.d.parity_error=0;b.d.channel_error=0;
        b.out(0x63,0x80);b.out(0x60,0xa5);b.out(0x62,0x55);
        check(b.in(0x60)==0xa5 && b.in(0x62)==0x55,"diagnostic PPI outputs");
        b.out(0x63,0x0f);check(b.in(0x62)==0xd5,"BSR set PC7");
        b.out(0x63,0x00);check(b.in(0x62)==0xd4,"BSR clear PC0");
        b.out(0x63,0x80);check(b.in(0x60)==0 && b.in(0x62)==0,"repeated mode clears output latches");
        b.out(0x63,0x99);b.out(0x61,0x40);b.out(0x63,0x99);
        check(b.in(0x61)==0 && b.d.keyboard_clock_low,"repeated mode clears port B");
        b.out(0x61,0x40);b.init_pic();b.out(0x21,0xfd);
        for(unsigned code: {0x1eu,0x9eu,0u,0xffu}) {
            b.key(code);check(b.d.keyboard_irq && b.d.keyboard_data_low,"keyboard busy/IRQ");
            check(!b.d.keyboard_clock_low,"keyboard busy must not inhibit clock");
            check(b.in(0x60)==code && b.in(0x460)==code,"scan code readback");
            check(b.d.keyboard_irq,"port A read cleared IRQ");
            b.key(code^0xff);check(b.in(0x60)==code,"busy keyboard overwrite");
            check(b.ack()==9 && b.isr()==2,"keyboard IRQ1 vector");
            b.out(0x61,0xc0);check(!b.d.keyboard_irq && !b.d.keyboard_data_low,"keyboard acknowledge");
            check(b.in(0x60)==0xff,"disabled scan data");
            b.out(0x61,0x40);b.out(0x20,0x20);b.idle();check(!b.d.intr,"acknowledged keyboard retrigger");
        }
        b.out(0x61,0);b.key(0x55);check(!b.d.keyboard_irq,"inhibited keyboard received byte");
        b.out(0x61,0x40);b.key(0xaa);check(b.in(0x60)==0xaa,"keyboard resumes after inhibit");
        b.clear_key();b.keyboard_bit(true);b.keyboard_bit(true);b.reset();
        b.out(0x63,0x99);b.out(0x61,0x40);b.key(0x69);
        check(b.in(0x60)==0x69 && b.d.keyboard_irq,"reset partial keyboard frame");
        b.clear_key();b.reset();
        // Firmware writes 0xC0 and leaves PB7 set. Clock and data stay released,
        // so a cable keyboard would shift 0x1E into the cleared receiver and lose
        // it. The serializer must keep the byte until PB7 drops, then deliver it.
        b.out(0x63,0x99);b.out(0x61,0xc0);
        check(b.d.keyboard_clear && !b.d.keyboard_clock_low && !b.d.keyboard_data_low,
              "held PB7 clears the receiver without clamping clock or data");
        b.d.external_keyboard=0;b.d.code=0x1e;b.d.code_valid=1;b.d.eval();
        check(!b.d.code_ready,"READY while the receiver is held clear");
        for(unsigned i=0;i<100000;i++) {
            check(!b.d.code_ready,"accepted scan code during receiver clear");
            b.tick();
            check(!b.d.keyboard_irq,"IRQ1 during receiver clear");
        }
        check(b.in(0x60)==0xff,"scan data while the receiver is held clear");
        b.accepted_code=false;b.watch_accept=true;b.out(0x61,0x40);
        unsigned deadline=b.ticks+120000;
        while(b.ticks<deadline && !b.d.keyboard_irq) b.tick();
        check(b.accepted_code && b.d.keyboard_irq && b.in(0x60)==0x1e,
              "0x1E delivered after PB7 release");
        b.d.code_valid=0;b.reset();
        b.out(0x63,0x99);b.out(0x61,0x41);b.timer=false;b.pit(2,0,1000);b.timer=true;b.idle(1000);
        unsigned count=b.latch(2);check(count<1000 && count>900,"PIT mode0 count");
        // Latch must hold both bytes despite time passing between reads.
        b.out(0x43,0x80);unsigned lo=b.in(0x42);b.idle(3000);unsigned hi=b.in(0x42);
        check((lo|(hi<<8))>b.latch(2)+20,"PIT latched read");
        b.out(0x61,0x40);b.idle(200);count=b.latch(2);b.idle(2000);check(b.latch(2)==count,"PIT gate stops mode0");
        b.out(0x61,0x41);b.idle(90000);check(b.d.pit_out&4,"PIT mode0 terminal output");
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
            b.out(0x61,0x40);b.pit(2,mode,10);b.idle(500);
            check(b.d.pit_out&4,"triggered mode idle output");
            b.out(0x61,0x41);
            unsigned low=0,fall=0,rise=0;
            for(unsigned i=0;i<3000;i++) {
                b.tick();
                if(!(b.d.pit_out&4)) {if(!low)fall=i;++low;}
                else if(low&&!rise)rise=i;
            }
            check(low==(mode==1?840u:84u) && rise>fall,"PIT one-shot/strobe pulse");
        }
        // Speaker is the AND of port B data and PIT OUT2; verify the same
        // raw signal feeds integrated 48 kHz audio, including DC removal.
        b.pit(2,3,1194);b.out(0x61,0x43);
        unsigned samples=0,last_sample=0;int minimum=32767,maximum=-32768;
        for(unsigned i=0;i<1000000;i++) {
            b.tick();check(bool(b.d.speaker)==bool(b.d.pit_out&4),"speaker PIT gating");
            if(b.d.audio_valid) {
                if(last_sample)check(i-last_sample==2083 || i-last_sample==2084,"audio cadence");
                last_sample=i;++samples;
                int value=int16_t(b.d.audio_sample);minimum=std::min(minimum,value);maximum=std::max(maximum,value);
            }
        }
        check(samples==480 && maximum-minimum>7000,"speaker tone/audio rate");
        b.timer=false;
        check(((b.in(0x62)>>5)&1)==((b.d.pit_out>>2)&1),"PIT2 port C feedback");b.timer=true;
        b.out(0x61,0x40);check(!b.d.speaker,"speaker mute");
        b.idle(5000000);check(std::abs(int16_t(b.d.audio_sample))<4,"muted DC tail");
        b.out(0x61,0x42);b.idle(5000000);
        check(b.d.speaker && std::abs(int16_t(b.d.audio_sample))<6,"static speaker DC removal");
        // A sub-sample-period square wave should integrate to near DC,
        // rather than alias to full-scale audio at the sample instants.
        b.pit(2,3,2);b.out(0x61,0x43);b.idle(4000000);
        for(unsigned i=0;i<1000000;i++) {
            b.tick();if(b.d.audio_valid)check(std::abs(int16_t(b.d.audio_sample))<400,"rapid speaker integration");
        }
        b.reset();check(!b.d.audio_valid && b.d.audio_sample==0,"audio reset silence");
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
        std::cout<<"PASS motherboard: "<<b.transactions<<" transactions, memory/PIC/PIT/PPI/keyboard/speaker/ISA, "<<b.ticks<<" fabric clocks\n";
    } catch(const std::exception& e) {std::cerr<<"FAIL motherboard: "<<e.what()<<"\n";return 1;}
}
