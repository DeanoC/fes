// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vpcxt_xt_keyboard.h"
#include "verilated.h"
#include <iostream>
#include <stdexcept>
#include <string>
static void check(bool ok,const char* why) {if(!ok)throw std::runtime_error(why);}
struct Bench {
    Vpcxt_xt_keyboard d;
    unsigned ticks=0;
    void tick() {d.clk=0;d.eval();d.clk=1;d.eval();++ticks;}
    void idle(unsigned n) {while(n--)tick();}
    void reset() {
        d.reset=1;d.code_valid=0;d.clock_low=0;d.data_low=0;d.keyboard_clear=0;idle(10);
        d.reset=0;tick();check(d.code_ready && d.keyboard_clock && !d.keyboard_data,"reset idle");
    }
    void start(unsigned code) {
        check(d.code_ready,"transport not ready");d.code=code;d.code_valid=1;tick();
        d.code_valid=0;check(!d.code_ready && !d.keyboard_clock,"RTS missing");
    }
    void frame(unsigned code,bool inhibit=false) {
        start(code);
        unsigned falls=0,rises=0,last_edge=ticks,last_change=ticks,previous_fall=0;
        bool clock=d.keyboard_clock,data=d.keyboard_data;
        unsigned deadline=ticks+100000;
        while(!d.code_ready && ticks<deadline) {
            if(inhibit && ticks-last_edge==1000 && falls==2 && clock) {
                d.clock_low=1;idle(20000);
                check(d.keyboard_clock==clock && d.keyboard_data==data,"clock inhibit did not pause bit");
                d.clock_low=0;
            }
            tick();
            if(data!=bool(d.keyboard_data)) {
                if(previous_fall)check(ticks-previous_fall>=250,"keyboard data hold <2.5us");
                last_change=ticks;data=d.keyboard_data;
            }
            if(clock!=bool(d.keyboard_clock)) {
                unsigned elapsed=ticks-last_edge;
                if(!inhibit)check(elapsed==(rises==0?8000u:4000u),"keyboard edge timing");
                last_edge=ticks;clock=d.keyboard_clock;
                if(clock) {
                    ++rises;
                    if(rises<=9)check(ticks-last_change>=250,"keyboard data setup <2.5us");
                } else {
                    check(falls<9,"too many frame bits");
                    check(data==bool(falls==0 ? 1 : ((code>>(falls-1))&1)),"start/data bit order");
                    ++falls;previous_fall=ticks;
                }
            }
        }
        check(d.code_ready && falls==9 && rises==10,"frame length/timeout");
        check(d.keyboard_clock && !d.keyboard_data,"idle pins after frame");
    }
};
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    try {
        Bench b;b.reset();
        for(unsigned code: {0u,0xffu,0x1eu,0x9eu,0xa5u})b.frame(code);
        b.frame(0x69,true);
        // An accepted byte waits through lost CTS, then gives a full 80 us
        // setup interval after CTS returns; VALID may already be deasserted.
        b.start(0x55);b.idle(4000);b.d.data_low=1;b.idle(30000);
        check(!b.d.keyboard_clock && !b.d.code_ready,"busy CTS allowed frame");
        b.d.data_low=0;b.idle(7999);check(!b.d.keyboard_clock,"short RTS after CTS");
        b.tick();check(b.d.keyboard_clock,"CTS resume timeout");b.reset();
        // No byte is accepted while clock, data, or receiver clear inhibits admission.
        // Clear matches port 61h = 0xC0: PB7 held, PB6 released, cable lines free.
        for(unsigned line=0;line<3;line++) {
            b.d.clock_low=line==0;b.d.data_low=line==1;b.d.keyboard_clear=line==2;b.tick();
            b.d.code_valid=1;b.d.code=line==2?0x1e:0xaa;b.idle(90000);
            check(!b.d.code_ready && b.d.keyboard_clock && !b.d.keyboard_data,"busy admission");
            b.d.code_valid=0;b.d.clock_low=0;b.d.data_low=0;b.d.keyboard_clear=0;b.tick();
            check(b.d.code_ready,"admission did not resume");
        }
        // Reset cancels an accepted byte at RTS, high, low and final-bit phases.
        for(unsigned phase: {1u,8001u,12001u,74000u}) {
            b.start(0xff);b.idle(phase);b.reset();b.frame(0x96);
        }
        std::cout<<"PASS keyboard: bit timing, CTS, inhibit, receiver clear, ordering and reset, "<<b.ticks<<" clocks\n";
    }catch(const std::exception& e){std::cerr<<"FAIL keyboard: "<<e.what()<<"\n";return 1;}
}
