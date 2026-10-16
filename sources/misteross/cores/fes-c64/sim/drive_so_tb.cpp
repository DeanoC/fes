// SPDX-License-Identifier: MIT
#include "Vdrive_so_sim_top.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>

// Golden pin/overflow observations from perfect6502's original NMOS netlist,
// commit09fc542877a84318291aa42dab143a3e2c3db974, with SO transitions at16
// positions per CPU cycle. Clock toggles every8 positions. These describe
// digital half-cycle sampling, not the analogue setup-time aperture.
static constexpr unsigned delays[16]={16,15,14,13,12,11,10,9,24,23,22,21,20,19,18,17};
static constexpr unsigned widths[7]={1,2,4,8,16,32,0}; // 0 holds the pin low
static constexpr unsigned captured[7]={0x0080,0x00c0,0x00f0,0x00ff,0xffff,0xffff,0xffff};

static void run(unsigned phase,unsigned width,unsigned mask) {
    Vdrive_so_sim_top t;
    t.clk=0;t.reset=1;t.byte_ready_n=1;t.clear_overflow=0;t.adjacent_clv=0;
    unsigned refs=0,fetches=0,injection=0,v_tick=0,marker_tick=0;
    bool armed=false,injected=false,clearing=false,cleared=false;
    for(unsigned sys=0;sys<18000;++sys){
        t.clk=0;t.eval();bool ce=t.ref_ce;
        if(sys==400)t.reset=0;
        if(t.phi&&!injected&&t.pc==0xc004&&t.state==12&&++fetches==2)armed=true;
        if(ce){
            if(armed&&!injected&&t.divider==phase){
                t.byte_ready_n=0;injected=true;injection=refs;
            }else if(injected&&width&&refs==injection+width)t.byte_ready_n=1;
            ++refs;
        }
        t.eval();
        if(t.raw_ca1!=t.byte_ready_n)std::abort();
        t.clk=1;t.eval();
        if(injected&&t.overflow&&!v_tick)v_tick=refs-1;
        if(t.marker==0xa5&&!marker_tick){
            marker_tick=refs;t.clear_overflow=1;clearing=true;
        }
        if(clearing&&!t.overflow)cleared=true;
        if(cleared&&t.overflow)std::abort();
    }
    bool expected=(mask>>phase)&1;
    if(!injected || bool(v_tick)!=expected || bool(marker_tick)!=expected ||
        (expected&&(v_tick-injection!=delays[phase]||!cleared))){
        std::printf("SO phase%u width%u injection%u V%u marker%u expected%u delay%u\n",
            phase,width,injection,v_tick,marker_tick,expected,delays[phase]);
        std::exit(1);
    }
}
static void reset_case(bool pending) {
    Vdrive_so_sim_top t;
    t.clk=0;t.reset=1;t.byte_ready_n=pending ? 1 : 0;t.clear_overflow=0;t.adjacent_clv=0;
    unsigned refs=0,reset_ref=0;
    bool requested=false,asserted=false,released=false;
    for(unsigned sys=0;sys<18000;++sys){
        t.clk=0;t.eval();
        if(sys==400)t.reset=0;
        if(pending&&!requested&&!t.cpu_reset&&t.pc==0xc004&&t.state==12&&t.phi)
            requested=true;
        if(t.ref_ce){
            ++refs;
            if(requested&&!asserted&&t.divider==7){
                t.byte_ready_n=0; // Capture an edge, then reset before publication.
                asserted=true;reset_ref=refs;
            }else if(asserted&&!released&&refs>reset_ref){
                t.reset=refs<reset_ref+32;
                if(!t.reset)released=true;
            }
        }
        t.eval();
        if(t.raw_ca1!=t.byte_ready_n)std::abort();
        t.clk=1;t.eval();
        // Start reset on the SYS edge following the end-Phi1 capture.
        if(pending&&asserted&&!released&&refs==reset_ref)t.reset=1;
        if(!t.cpu_reset&&(!pending||released)&&(t.overflow||t.marker==0xa5))
            std::abort();
    }
    if(pending&&(!asserted||!released))std::abort();
}
static void clv_case(unsigned offset) {
    Vdrive_so_sim_top t;
    t.clk=0;t.reset=1;t.byte_ready_n=1;t.clear_overflow=0;t.adjacent_clv=1;
    unsigned refs=0,anchor=0,v_tick=0,marker_tick=0;
    bool armed=false,injected=false;
    for(unsigned sys=0;sys<18000;++sys){
        t.clk=0;t.eval();
        if(sys==400)t.reset=0;
        if(t.ref_ce){
            // The CLV dummy read precedes this repeated opcode-fetch cycle.
            // Anchor the first BVC's DECODE, not the earlier REG prefetch.
            if(!armed&&t.state==12&&t.pc==0xc002&&t.divider==0){
                armed=true;anchor=refs;
            }
            if(armed&&!injected&&refs==anchor+offset){
                injected=true;t.byte_ready_n=0;
            }
            ++refs;
        }
        t.clk=1;t.eval();
        if(injected&&t.overflow&&!v_tick)v_tick=refs-1;
        if(t.marker==0xa5&&!marker_tick)marker_tick=refs-1;
    }
    // Netlist observations: the first eight positions are inhibited; later
    // edges set V at relative32/48/64 and finish the program in CPU cycle10.
    // The RTL writes RAM late in Phi2; the netlist memory adapter writes at
    // Phi2's rising edge. Compare the complete CPU cycle, not those bus phases.
    bool captured=offset>=8;
    unsigned expected_v=offset<24 ? 32 : offset<40 ? 48 : 64;
    if(!armed||!injected||bool(v_tick)!=captured||bool(marker_tick)!=captured||
        (captured&&(v_tick-anchor!=expected_v||(marker_tick-anchor)/16!=10))){
        std::printf("SO CLV offset%u anchor%u V%u marker%u captured%u\n",
            offset,anchor,v_tick,marker_tick,captured);
        std::exit(1);
    }
}
int main(int argc,char**argv){
    Verilated::commandArgs(argc,argv);
    for(unsigned i=0;i<7;++i)for(unsigned phase=0;phase<16;++phase)
        run(phase,widths[i],captured[i]);
    reset_case(false);reset_case(true);
    for(unsigned offset=0;offset<48;++offset)clv_case(offset);
    std::puts("1541 SO:112 pulse/phase and48 CLV-inhibit netlist cases, BVC, reset and raw VIA path passed");
}
