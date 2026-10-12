// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vst_mfp_iack_timing.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
struct Test {
    Vst_mfp_iack_timing dut;
    unsigned assertions=0;
    void require(bool ok,const char *message) {
        ++assertions;
        if (!ok) { std::cerr<<message<<"\n"; std::exit(1); }
    }
    void tick(bool edge=false) {
        dut.phi1=0; dut.phi2=edge;
        dut.clk=0;dut.eval();dut.clk=1;dut.eval();
    }
    void reset() {
        dut.iack=0;dut.ds=0;dut.reset=1;tick();
        require(!dut.ready,"reset clears acknowledge");dut.reset=0;
    }
    void halfperiod() { for(unsigned i=0;i<5;++i)tick();tick(true); }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv); Test t;
    // At 52.224 MHz, two periods are below 50 ns and three exceed it.
    // Present pins before a fabric edge, then vary setup at first falling CLK.
    for(unsigned setup=1;setup<=3;++setup) {
        t.reset();t.dut.iack=1;t.dut.ds=1;
        for(unsigned i=1;i<setup;++i)t.tick();
        t.tick(true);t.require(!t.dut.ready,"first falling edge cannot complete IACK");
        unsigned completion=setup==3?7:9;
        for(unsigned edge=1;edge<=completion;++edge) {
            t.halfperiod();
            t.require(bool(t.dut.ready)==(edge==completion),
                      "50 ns setup and seven half-period completion boundary");
        }
        for(unsigned i=0;i<4;++i) {t.halfperiod();t.require(t.dut.ready,"held IACK keeps ready");}
        t.dut.ds=0;t.tick();t.require(!t.dut.ready,"DS withdrawal clears ready");
    }
    for(unsigned cancel=0;cancel<3;++cancel) {
        t.reset();t.dut.iack=1;t.dut.ds=1;t.tick();t.tick();t.tick(true);
        t.halfperiod();t.halfperiod();
        if(cancel==0)t.dut.iack=0;
        if(cancel==1)t.dut.ds=0;
        if(cancel==2)t.dut.reset=1;
        t.tick();t.require(!t.dut.ready,"cancellation clears pending IACK");
        for(unsigned i=0;i<10;++i) {t.halfperiod();t.require(!t.dut.ready,"cancelled IACK cannot complete later");}
    }
    std::cout<<"ST MFP IACK timing: "<<t.assertions<<" assertions passed\n";
}
