// SPDX-License-Identifier: MIT
#include "Vclock_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdio>
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Vclock_sim_top top;
    constexpr std::array<unsigned,6> system={52224000,100,10,10,10,5};
    constexpr std::array<unsigned,6> cpu={985248,17,5,6,10,2};
    std::array<uint64_t,6> phase{},count{};
    top.clk=0;top.eval();
    if(top.prepared!=0b100111 || top.envelope_prepared!=0b100111) {
        std::printf("SID preparation guard masks oscillator=%x envelope=%x expected27\n",top.prepared,top.envelope_prepared);
        return 1;
    }
    unsigned ntsc_phase=0, tod_phase=0, last_phi=0, drive_cycles=0, sid_results=0, active_cycles=0;
    for(unsigned tick=0;tick<52224000;++tick) {
        for(unsigned i=0;i<system.size();++i) {
            phase[i]+=cpu[i];
            bool expected=phase[i]>=system[i];
            bool actual=(top.enables>>i)&1;
            if(actual!=expected) {
                std::printf("clock %u tick %u enable %u expected %u\n",i,tick,actual,expected);
                return 1;
            }
            if(expected){phase[i]-=system[i];++count[i];}
        }
        ntsc_phase += 1022730;
        const bool phi = ntsc_phase >= 52224000;
        if (phi) ntsc_phase -= 52224000;
        if (top.ntsc_phi != phi) {
            std::fprintf(stderr, "NTSC Phi mismatch at SYS %u\n", tick);
            return 1;
        }
        if (last_phi && (top.bus_strobe != (tick-last_phi == 27) ||
                         top.bus_sample != (tick-last_phi == 44))) {
            std::fprintf(stderr, "NTSC bus phase mismatch at SYS %u\n", tick);
            return 1;
        }
        if (phi) last_phi = tick;
        if (top.ntsc_reset) tod_phase = 0;
        else if (phi) {
            tod_phase += 60;
            ++active_cycles;
        }
        const bool tod = tod_phase >= 1022730;
        if (tod) tod_phase -= 1022730;
        if (top.ntsc_tod != tod) {
            std::fprintf(stderr, "NTSC TOD mismatch at SYS %u\n", tick);
            return 1;
        }
        drive_cycles += top.drive_phi;
        sid_results += top.sid_done;
        top.clk=1;top.eval();top.clk=0;top.eval();
    }
    if (count[0]!=985248 || drive_cycles!=1000000 || active_cycles!=1022730-15 ||
        sid_results+1!=active_cycles) {
        std::fprintf(stderr, "NTSC active Phi %u, SID results %u, drive Phi %u\n",
                     active_cycles, sid_results, drive_cycles);
        return 1;
    }
    std::puts("NTSC clock PASS: 1022730 Phi/s, 60 Hz TOD, CPU strobe/sample phases, 1 MHz drive and one filtered SID result per completed guest cycle");
    std::puts("C64 clock PASS: 52224000 motherboard ticks match independent fractional reference, PAL/boundary/fractional2-3/adjacent/full-rate enables and SID preparation guard");
}
