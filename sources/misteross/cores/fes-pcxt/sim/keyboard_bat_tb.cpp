// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vkeyboard_bat_top.h"
#include "verilated.h"
#include <iostream>
#include <stdexcept>
static void check(bool ok, const char *why) { if (!ok) throw std::runtime_error(why); }
struct Bench {
    Vkeyboard_bat_top d;
    unsigned long long ticks = 0;
    void tick() { d.clk=0; d.eval(); d.clk=1; d.eval(); ++ticks; }
    void idle(unsigned n) { while(n--) tick(); }
    void write(unsigned reg, unsigned value) {
        d.address=reg; d.wdata=value; d.chip_select_n=0; d.write_enable_n=0;
        idle(4); d.write_enable_n=1; d.chip_select_n=1; idle(4);
    }
    unsigned read(unsigned reg) {
        d.address=reg; d.chip_select_n=0; d.read_enable_n=0; idle(4);
        unsigned result=d.rdata;
        d.read_enable_n=1; d.chip_select_n=1; idle(4); return result;
    }
    void reset() {
        d.reset=1; d.chip_select_n=d.read_enable_n=d.write_enable_n=1; idle(8);
        d.reset=0; write(3,0x99); // BIOS PPI mode: port A/C input, B output.
        write(1,0x40); check(!d.clock_low && !d.keyboard_clear,"PPI setup");
    }
    void quiet(unsigned n) {
        while(n--) {tick(); check(!d.keyboard_irq,"unsolicited or stale BAT IRQ");}
    }
    void request() { write(1,0); idle(2000000); write(1,0xc0); }
    void receive() {
        write(1,0x40);
        unsigned n=0; while(!d.keyboard_irq && n++<100000) tick();
        check(d.keyboard_irq,"BAT did not reach the actual motherboard receiver");
        check(read(0)==0xaa,"BAT data was not AA through PPI port A");
        idle(50000); check(d.keyboard_irq && read(0)==0xaa,"BAT not held until acknowledgement");
        write(1,0xc0); check(!d.keyboard_irq,"PPI PB7 did not acknowledge BAT");
        write(1,0x40); quiet(100000);
    }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    try {
        Bench b; b.reset(); b.quiet(100000);
        // Brief inhibit is a pause, not a reset request.
        b.write(1,0); b.idle(1000); b.write(1,0x40); b.quiet(100000);
        b.request(); check(b.d.keyboard_clear,"BIOS clear not asserted");
        b.quiet(100000); // PB7 must delay admission without cancelling pending BAT.
        b.receive();
        b.request(); b.receive(); // one response for each distinct reset.
        b.request(); // BAT is pending behind PB7, before any serial byte starts.
        b.reset(); b.quiet(100000);
        // Global reset cancels a queued byte and every part of an active frame.
        for(unsigned delay: {0u,1u,8001u,12001u,74000u}) {
            b.request(); b.write(1,0x40); b.idle(delay); b.reset(); b.quiet(100000);
            b.request(); b.receive();
        }
        // A new long inhibit cancels an in-flight byte and requests a fresh
        // frame only after release. No half-frame may reach IRQ1.
        b.request(); b.write(1,0x40); b.idle(13000);
        b.write(1,0x80); b.idle(2000000); b.quiet(50000);
        b.write(1,0xc0); b.quiet(10000); b.receive();
        std::cout<<"PASS XT BAT: real PPI receive/IRQ/ack, inhibit, clear, re-reset and cancellation; "
                 <<b.ticks<<" clocks\n";
    } catch(const std::exception &e) { std::cerr<<"FAIL XT BAT: "<<e.what()<<'\n'; return 1; }
}
