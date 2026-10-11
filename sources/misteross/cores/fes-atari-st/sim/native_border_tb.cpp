// SPDX-License-Identifier: GPL-3.0-or-later
// Border runs preserve within-line colours, row repetition and immutable banks.
#include "Vst_native_border.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>

struct Test {
    Vst_native_border dut;
    unsigned checks=0;
    void check(bool good, const char *why) {
        ++checks;
        if (!good) { std::cerr << "FAIL " << why << '\n'; std::exit(1); }
    }
    void system() { dut.clk_sys=0;dut.eval();dut.clk_sys=1;dut.eval(); }
    void pixel() { dut.clk_pixel=0;dut.eval();dut.clk_pixel=1;dut.eval(); }
    static unsigned colour(unsigned row,unsigned x,unsigned bias=0) {
        return (row ? (x<75?0x1c0:x<300?0x38:0x107) : (x<12?0x1ff:x<81?0x7:0x1c7)) ^ bias;
    }
    void capture(unsigned bank,unsigned bias,bool pal=true) {
        dut.capture_display=0;dut.capture_bank=bank;dut.capture_pal=pal;
        dut.capture_start=1;system();dut.capture_start=0;dut.capture_active=1;
        const unsigned first=pal?34:5, left=pal?8:4;
        for (unsigned y=0;y<2;++y) {
            dut.native_line=first+y;
            for(unsigned cyc=0;cyc<512;++cyc) {
                dut.native_cycle=cyc;dut.capture_tick=1;
                dut.palette_zero=colour(y,cyc>=left?cyc-left:0,bias);system();
                dut.capture_tick=0;system(); // gaps must not append duplicates
            }
        }
        dut.capture_active=0;check(!dut.capture_overflow,"ordinary colour runs overflowed");
    }
    void display(unsigned bank,unsigned row,unsigned bias,bool sof=false) {
        dut.output_bank=bank;dut.output_row=row;dut.output_ce=0;
        pixel();pixel();
        dut.output_sof=sof;dut.output_line_start=!sof;pixel();
        dut.output_sof=0;dut.output_line_start=0;
        for(unsigned x=0;x<416;++x) {
            dut.output_x=x;dut.output_ce=1;dut.eval();
            check(dut.output_rgb==colour(row,x,bias),"border colour changed at wrong native pixel");
            pixel();
            // Repeat native pixels three times, as in the full-width renderer.
            for(unsigned repeat=0;repeat<2;++repeat) {
                check(dut.output_rgb==colour(row,x,bias),"repeated pixel lost border colour");pixel();
            }
        }
        dut.output_ce=0;
    }
    void run() {
        dut.reset_sys=dut.reset_pixel=1;system();pixel();
        dut.reset_sys=dut.reset_pixel=0;
        capture(0,0);capture(1,0x49,false);
        display(0,0,0,true);display(0,0,0);display(0,1,0);display(0,1,0);
        display(1,0,0x49,true);display(1,1,0x49);
        capture(2,0x92); // another producer bank must preserve the front bank
        display(1,0,0x49,true);display(1,1,0x49);
        display(2,0,0x92,true);display(2,1,0x92);
        // Fill every valid event slot, then replay consecutive one-pixel runs.
        // The final successor wraps to zero but must not be read as a new run.
        dut.capture_bank=2;dut.capture_pal=1;dut.capture_start=1;system();dut.capture_start=0;
        dut.capture_active=1;dut.capture_tick=1;dut.native_line=34;
        for(unsigned x=0;x<416;++x) {
            dut.native_cycle=8+x;dut.palette_zero=x<32?x:31;system();
        }
        check(!dut.capture_overflow,"full event arena overflowed without an extra run");
        dut.capture_active=0;dut.capture_tick=0;
        dut.output_bank=2;dut.output_row=0;dut.output_ce=0;pixel();pixel();
        dut.output_sof=1;pixel();dut.output_sof=0;
        for(unsigned x=0;x<416;++x) {
            dut.output_x=x;dut.output_ce=1;dut.eval();
            check(dut.output_rgb==(x<32?x:31),"consecutive border run or last event replay failed");
            pixel();
        }
        dut.output_ce=0;display(1,0,0x49,true);
        // The small test capacity exhausts on a deliberately dense border.
        dut.capture_bank=2;dut.capture_pal=1;dut.capture_start=1;system();dut.capture_start=0;
        dut.capture_active=1;dut.capture_tick=1;dut.native_line=34;
        for(unsigned cyc=8;cyc<70;++cyc) {dut.native_cycle=cyc;dut.palette_zero=cyc;system();}
        check(dut.capture_overflow,"event overflow was not reported");
        dut.capture_active=0;display(1,0,0x49,true);
        dut.capture_start=1;system();dut.capture_start=0;
        check(!dut.capture_overflow,"next capture did not clear overflow");
        std::cout<<"Native border PASS: "<<checks<<" checks, PAL/NTSC porches, within-line colours, consecutive runs, full arena, repeated rows, banks and overflow\n";
        dut.final();
    }
};
int main(int argc,char **argv) {Verilated::commandArgs(argc,argv);Test test;test.run();}
