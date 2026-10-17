// SPDX-License-Identifier: MIT
#include "Vc64_via.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
struct Bench {
    Vc64_via t;
    Bench() {
        t.clk=0;t.reset=1;t.phi=0;t.access=0;t.cs=0;t.we=0;t.rs=0;t.din=0;
        t.pa_pin=0xff;t.pb_pin=0xff;t.ca1=1;t.ca2=1;t.cb1=1;t.cb2=1;
        tick();t.reset=0;tick();
    }
    void tick(){t.clk=0;t.eval();t.clk=1;t.eval();}
    void phi(unsigned n=1){while(n--){t.phi=1;tick();t.phi=0;tick();}}
    void write(unsigned r,unsigned v){t.rs=r;t.din=v;t.access=1;t.cs=1;t.we=1;tick();t.access=0;t.cs=0;t.we=0;tick();}
    unsigned peek(unsigned r){t.rs=r;t.eval();return t.dout;}
    unsigned read(unsigned r){unsigned v=peek(r);t.access=1;t.cs=1;tick();t.access=0;t.cs=0;tick();return v;}
    void check(bool ok,const char *msg){if(!ok){std::printf("VIA: %s IFR=%02x IER=%02x T1=%04x T2=%04x\n",msg,peek(13),peek(14),peek(4)|(peek(5)<<8),peek(8)|(peek(9)<<8));std::exit(1);}}
};
int main(int argc,char **argv){
    Verilated::commandArgs(argc,argv);
    Bench b;
    b.check(b.peek(14)==0x80 && b.peek(13)==0 && b.t.pa_ddr==0 && b.t.pb_ddr==0,"reset masks and directions");
    b.write(0,0xa5);b.write(2,0xf0);b.t.pb_pin=0x3c;
    b.check(b.peek(0)==0xac,"PB combines output latch and input pins");
    b.write(1,0xff);b.write(3,0xff);b.t.pa_pin=0x42;
    b.check(b.peek(1)==0x42 && b.peek(15)==0x42,"PA reads physical pins even for outputs");
    b.write(11,3);b.t.ca1=0;b.t.cb1=0;b.tick();
    b.t.pa_pin=0x81;b.t.pb_pin=0x09;b.tick();
    b.check(b.peek(1)==0x42 && b.peek(0)==0xac,"input latches retain edge-time data");
    b.write(14,0x92);b.check(b.t.irq,"enable already pending CA1 and CB1");
    b.read(15);b.check((b.peek(13)&2)!=0,"PA alias does not clear interrupt");
    b.read(1);b.check((b.peek(13)&2)==0 && b.peek(1)==0x81,"ordinary PA read clears and releases latch");
    b.read(0);b.check(!b.t.irq && b.peek(0)==0xa9,"PB read clears CB1 and releases latch");
    b.write(12,0x22);b.t.ca2=0;b.t.cb2=0;b.tick();
    b.read(1);b.read(0);b.check((b.peek(13)&9)==9,"independent CA2/CB2 interrupts survive port accesses");
    b.write(13,1);b.check((b.peek(13)&9)==8,"IFR W1C selects only requested source");
    b.write(13,8);b.check(b.peek(13)==0,"clear second source");
    b.write(12,1);b.t.ca1=1;b.tick();b.check(b.peek(13)&2,"positive CA1 edge selectable");
    b.write(13,0x7f);b.write(12,0x88);
    b.read(1);b.check(!b.t.ca2_output && b.t.ca2_oe,"PA read starts handshake");
    b.t.ca1=0;b.tick();b.check(b.t.ca2_output,"CA1 active edge releases handshake");
    b.read(0);b.check(b.t.cb2_output,"PB read does not start output handshake");
    b.write(0,0);b.check(!b.t.cb2_output,"PB write starts handshake");
    b.t.cb1=1;b.tick();b.t.cb1=0;b.tick();b.check(b.t.cb2_output,"CB1 releases handshake");
    b.write(12,0xaa);b.read(1);b.write(0,1);b.check(!b.t.ca2_output && !b.t.cb2_output,"pulse outputs asserted");
    b.phi();b.check(b.t.ca2_output && b.t.cb2_output,"pulse duration one Phi2");
    b.write(12,0xcc);b.check(!b.t.ca2_output && !b.t.cb2_output,"manual low outputs");
    b.write(12,0xee);b.check(b.t.ca2_output && b.t.cb2_output,"manual high outputs");
    b.write(13,0x7f);b.write(14,0x7f);b.write(11,0x80);
    b.write(4,3);b.write(5,0);b.check(!(b.t.pb_output&0x80),"T1 starts PB7 low");
    b.phi(4);b.check(!(b.peek(13)&0x40),"T1 does not fire early");
    b.phi();b.check((b.peek(13)&0x40) && !b.t.irq && (b.t.pb_output&0x80),"T1 N+2 one-shot expiry while masked");
    b.check(b.peek(4)==0xff && b.peek(5)==0xff,"T1 exposes underflow before reload");
    b.write(14,0xc0);b.check(b.t.irq && (b.peek(13)&0x80),"T1 enabled pending summary");
    b.read(6);b.check(b.t.irq,"latch read preserves T1 interrupt");
    b.read(4);b.check(!b.t.irq,"counter low read clears T1 interrupt");
    b.phi(100);b.check(!(b.peek(13)&0x40),"one-shot does not retrigger");
    b.write(11,0xc0);b.write(4,2);b.write(5,0);b.phi(4);
    b.check(b.t.irq && (b.t.pb_output&0x80),"continuous first expiry");
    b.write(13,0x40);b.phi(3);b.check(!b.t.irq,"continuous period not early");
    b.phi();b.check(b.t.irq && !(b.t.pb_output&0x80),"continuous N+2 period and PB7 toggle");
    b.write(7,1);b.check(!b.t.irq && b.peek(7)==1,"latch-high write clears without loading count");
    b.write(14,0x40);b.write(11,0);b.write(8,1);b.write(9,0);
    b.phi(2);b.check(!(b.peek(13)&0x20),"T2 not early");b.phi();
    b.check(b.peek(13)&0x20,"T2 N+2 expiry");b.read(8);b.phi(100);b.check(!(b.peek(13)&0x20),"T2 one-shot acknowledgement");
    b.write(11,0x20);b.t.pb_pin=0x40;b.phi();b.write(8,1);b.write(9,0);b.phi(20);
    b.check(b.peek(8)==1,"pulse counter ignores Phi2 when PB6 unchanged");
    b.t.pb_pin=0;b.phi();b.check(b.peek(8)==0,"PB6 falling edge counts once");
    b.phi(5);b.check(!(b.peek(13)&0x20),"low PB6 does not repeatedly count");
    b.t.pb_pin=0x40;b.phi();b.t.pb_pin=0;b.phi();b.check(b.peek(13)&0x20,"second falling edge expires pulse counter");
    std::puts("c64 VIA: ports, edge latches, handshakes, IRQ masks/W1C, T1/PB7 and T2 passed");
}
