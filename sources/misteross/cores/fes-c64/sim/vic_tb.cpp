// SPDX-License-Identifier: MIT
#include "Vc64_vic.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
struct Bench {
    Vc64_vic t;
    Bench() {
        t.cycle_clock=0; t.cpu_bus_data=0;
        t.clk_sys=0; t.phi=0; t.pixel_clk=0; t.reset=1; t.cs=0; t.we=0;
        t.reading=0; t.read_sample=0; t.color_cs=0; t.color_we=0; t.color_addr=0; t.color_din=0;
        t.bank=0; t.ram_data=0; t.addr=0; t.din=0; tick(); t.reset=0; tick();
    }
    void tick() { t.clk_sys=0; t.eval(); t.clk_sys=1; t.eval(); }
    void phi(unsigned n=1) { while(n--) { t.phi=1; tick(); t.phi=0; tick(); } }
    void write(unsigned a,unsigned v) { t.addr=a; t.din=v; t.cs=1; t.we=1; tick(); t.we=0; t.cs=0; tick(); }
    unsigned read(unsigned a) { t.addr=a; t.eval(); return t.dout; }
    void check(bool condition,const char *name) { if(!condition) { std::printf("VIC: %s raster=%u IRQ=%u status=%02x\n",name,t.raster,t.irq,read(0x19));std::exit(1); } }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Bench b;
    b.write(0x12,2); b.write(0x19,1);
    b.check(b.read(0x12)==0,"D012 reads beam rather than compare latch");
    b.phi(126);
    b.check(b.t.raster==2 && b.read(0x19)==0x71 && !b.t.irq,"masked raster event still latches");
    b.write(0x1a,1);
    b.check(b.t.irq && b.read(0x19)==0xf1 && b.read(0x1a)==0xf1,"enable pending source immediately");
    b.write(0x19,0); b.check(b.t.irq,"zero does not acknowledge");
    b.write(0x19,2); b.check(b.t.irq,"other source does not acknowledge raster");
    b.write(0x1a,0); b.check(!b.t.irq && (b.read(0x19)&1),"disable keeps pending latch");
    b.write(0x1a,1); b.write(0x19,1); b.check(!b.t.irq,"write one acknowledges");
    b.write(0x12,3); b.write(0x12,2);
    b.check(!b.t.irq,"no second event in the same line after acknowledgement");
    b.phi(63); b.write(0x12,3);
    b.check(b.t.irq,"compare write on unfired current line triggers immediately");
    b.write(0x19,1);
    b.write(0x11,0x80); b.write(0x12,0);
    b.check(!(b.read(0x11)&0x80),"D011 reads current raster high bit");
    b.phi(253*63);
    b.check(b.t.raster==256 && (b.read(0x11)&0x80) && b.t.irq,"nine-bit raster compare");
    b.write(0x19,1); b.write(0x11,0); b.write(0x12,0);
    b.phi(55*63);
    b.check(b.t.raster==311 && !b.t.irq,"PAL final line remains visible before wrap");
    b.phi(62); b.check(b.t.raster==311,"PAL line has exactly 63 Phi2 cycles");
    b.phi();
    b.check(b.t.raster==0 && !b.t.irq,"line zero comparison deferred to cycle two");
    b.phi(); b.check(b.t.irq,"line zero event");
    b.write(0x19,1); b.phi(62); b.check(!b.t.irq,"one event per zero line");
    b.phi(311*63); b.check(!b.t.irq,"next frame zero starts without IRQ");
    b.phi(); b.check(b.t.irq,"next frame retriggers raster event");
    std::puts("c64 VIC: compare reads/writes, mask, W1C, per-line limit, ninth bit and 312-line PAL frame wrap passed");
}
