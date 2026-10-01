// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vzx81_session_harness.h"
#include "Vzx81_session_harness___024root.h"
#include <verilated.h>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

static void check(bool condition, const std::string &message) {
    if (!condition) { std::cerr << "ZX81 session display: " << message << '\n'; std::exit(1); }
}
struct Bench {
    Vzx81_session_harness t;
    uint64_t sys_phase = 17000000;
    bool toggle = false, watch_reset = false;
    unsigned last_sequence = 0;
    uint64_t full_frame_pixels = 0;
    bool last_ui = false;
    Bench() {
        t.clk_sys=0; t.pixel_clk=0; t.reset_hold=1; t.gpo=0;
        t.force_media_busy=0; t.peek_addr=0x5000; t.eval();
        for(unsigned i=0;i<100;++i) tick();
        t.reset_hold=0;
    }
    void tick() {
        // Unrelated 52.224/74.25 MHz clocks; request timing walks across phases.
        sys_phase += 52224000;
        if (sys_phase >= 74250000) {
            sys_phase -= 74250000;
            t.clk_sys=0; t.eval(); t.clk_sys=1; t.eval();
        }
        const auto h=t.raster_h, v=t.raster_v;
        check(t.de==(h<1280 && v<720), "DE changed during UI switching");
        check(t.hs==(h>=1390 && h<1430), "HS changed during UI switching");
        check(t.vs==(v>=725 && v<730), "VS changed during UI switching");
        if (t.de) ++full_frame_pixels;
        t.pixel_clk=0; t.eval(); t.pixel_clk=1; t.eval();
        if (watch_reset) check(!t.exec_reset, "display operation reset the Z80");
        if (last_ui!=bool(t.ui_active))
            check(h==1649 && v==749, "normal plane switch happened inside a frame");
        if (last_sequence!=t.displayed_sequence && t.displayed_sequence!=0) {
            check(h==1279 && v==719, "frame ACK preceded the final pixel");
            check(full_frame_pixels>=921600, "frame ACK lacked a full frame");
        }
        last_sequence=t.displayed_sequence;
        check(!t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__violation,
              "DDR bounds, writes or unused-port violation");
        last_ui=t.ui_active;
    }
    void cycles(unsigned n) { for(unsigned i=0;i<n;++i) tick(); }
    bool acknowledged() const { return bool(t.gpi&0x800000)==toggle; }
    void begin_close() {
        toggle=!toggle;
        t.gpo=unsigned(toggle)<<31 | 20<<24;
    }
    bool guard_busy() const {
        const auto *r=t.rootp;
        return r->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__remaining ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__reads_owed ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__m_read ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__m_write ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__skid ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__beats_owed ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__finishing ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__returned ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__m_readdatavalid ||
            r->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__readdatavalid;
    }
    void finish_fault_close() {
        bool saw_hidden_return=false;
        for(unsigned i=0;i<2000000 && !acknowledged();++i) {
            tick();
            saw_hidden_return |= bool(t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__returned);
            check(!t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__readdatavalid,
                  "held guard exposed old responses to the reader");
            if(acknowledged()) check(!guard_busy(),"fault close ACK preceded physical DDR drain");
        }
        check(saw_hidden_return,"hard-fault regression did not drain hidden responses");
        check(acknowledged() && !(t.gpi&0x400000) && t.quiesced && !t.ui_active,
              "fault close did not acknowledge after physical DDR drain");
        check(t.rootp->zx81_session_harness__DOT__display__DOT__port_reset,
              "hard fault acknowledged before synchronized guard hold");
        cycles(12);
    }
    unsigned command(unsigned op,unsigned index=0,unsigned arg=0) {
        toggle=!toggle;
        t.gpo=unsigned(toggle)<<31 | op<<24 | index<<16 | arg;
        for(unsigned i=0;i<4000000 && bool(t.gpi&0x800000)!=toggle;++i) tick();
        check(bool(t.gpi&0x800000)==toggle,"GP/CDC acknowledgement timeout");
        unsigned result=t.gpi&0x40ffff;
        cycles(12); return result;
    }
    unsigned peek(unsigned addr) { t.peek_addr=addr; t.eval(); return t.peek_data; }
    void submit(unsigned seq,unsigned slot) {
        check(command(21,0,seq&65535)==0,"sequence low rejected");
        check(command(21,1,seq>>16)==0,"sequence high rejected");
        check(command(21,2,slot)==0,"slot commit rejected");
    }
    void wait_sequence(unsigned seq) {
        for(unsigned i=0;i<4000000 && t.displayed_sequence!=seq;++i) tick();
        if(t.displayed_sequence!=seq) std::cerr << "seq=" << t.displayed_sequence << " requested=" << seq
            << " underflow=" << t.underflows << " fault=" << unsigned(t.faulted)
            << " raster=" << t.raster_h << "," << t.raster_v
            << " state=" << unsigned(t.rootp->zx81_session_harness__DOT__display__DOT__video__DOT__state)
            << " pending=" << unsigned(t.rootp->zx81_session_harness__DOT__display__DOT__video__DOT__pending)
            << " fetchseq=" << t.rootp->zx81_session_harness__DOT__display__DOT__video__DOT__fetch_sequence << '\n';
        check(t.displayed_sequence==seq,"full frame did not complete");
        for(unsigned i=0;i<1300000 && !t.ui_active;++i) tick();
        check(t.ui_active,"completed frame did not become visible");
    }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Bench b;
    check(b.command(1,7)==0x307,"session display/DDR capability bits missing");
    check(b.command(18,0)==1280,"display geometry CDC mismatch");
    check(b.command(19,0,1)==0,"fixed display layout configuration rejected");
    check(b.command(2,0,1)==0,"execution release rejected");
    b.watch_reset=true;
    b.cycles(200000);
    check(b.peek(0x5000)==0x5a,"Z80 diagnostic failed to retain RAM marker");
    auto writes=b.t.cpu_writes;
    check(b.command(3,3,30)==0,"keyboard press rejected");
    check(b.command(3,3,31)==0,"keyboard neutralization rejected");
    check(b.t.keyboard==0xffffffffffull,"keyboard remained held before opening");
    check(b.command(20,0,1)==0,"display enable rejected");
    check(!b.t.ui_active,"enable exposed an uncompleted frame");
    check(b.command(2,0,0)==0x400004,"execution hold abandoned an active display");
    b.submit(1,0);
    check(b.command(21,0,2)==0x400004,"second frame bypassed pending completion");
    b.wait_sequence(1);
    check(b.t.underflows==0 && !b.t.faulted,"initial scanout underflowed");
    check(b.t.cpu_writes>writes,"Z80 stopped while launcher was active");
    check(b.peek(0x5000)==0x5a,"opening altered retained RAM");
    // Verify real color order at a full frame's origin.
    while(b.t.raster_h!=0 || b.t.raster_v!=0) b.tick();
    check(b.t.rgb==0x22,"slot 0 pixels missing from HDMI selection");
    b.t.force_media_busy=1;
    check(b.command(4,0,1)==0x400004,"busy LOAD accepted media replacement");
    check(b.command(4,1,0)==0x400004,"busy LOAD accepted eject");
    b.t.force_media_busy=0;
    check(b.command(4,0,1)==0,"live cassette begin failed");
    check(b.command(5,1,0x55)==0,"live cassette byte failed");
    check(b.command(6)==0,"live cassette commit failed");
    check(b.command(4,1,0)==0,"live cassette eject failed");
    check(b.peek(0x5000)==0x5a,"live media reset the machine");
    b.submit(2,1); b.wait_sequence(2);
    while(b.t.raster_h!=0 || b.t.raster_v!=0) b.tick();
    check(b.t.rgb==0x77,"slot 1 pixels missing from HDMI selection");
    // A held DDR response must postpone quiesce ACK and drain before return.
    auto &remaining=b.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__remaining;
    while(!remaining) b.tick();
    b.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=1;
    b.begin_close();
    b.cycles(300);
    check(bool(b.t.gpi&0x800000)!=b.toggle,"quiesce ACK discarded outstanding reads");
    b.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=0;
    for(unsigned i=0;i<2000000 && bool(b.t.gpi&0x800000)!=b.toggle;++i) b.tick();
    check(bool(b.t.gpi&0x800000)==b.toggle && !(b.t.gpi&0x400000),"quiesce did not drain");
    check(!b.t.ui_active && b.t.quiesced,"quiesce ACK preceded machine return");
    check(b.t.rgb==b.t.machine_rgb,"return failed to select continuing ZX81 video");
    check(b.peek(0x5000)==0x5a,"closing altered retained RAM");
    b.cycles(50);
    auto commands=b.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__accepted;
    b.cycles(1000);
    check(commands==b.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__accepted,
          "closed UI continued DDR reads");
    check(b.command(20,0,1)==0,"reopen rejected");
    check(!b.t.ui_active,"reopen displayed a stale completed frame");
    b.submit(3,0); b.wait_sequence(3);
    check(b.peek(0x5000)==0x5a,"reopen reset retained state");
    // A visible-frame underrun disables only the plane, preserving the Z80.
    b.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=1;
    for(unsigned i=0;i<1500000 && !b.t.faulted;++i) b.tick();
    check(b.t.faulted,"visible underflow did not contain the display");
    check(b.t.rgb==b.t.machine_rgb,"display fault did not restore machine pixels");
    b.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=0;
    check(b.command(20)==0,"faulted plane could not be closed");
    check(b.peek(0x5000)==0x5a,"display fault reset retained state");
    check(b.command(18,9)==25,"closed faulted plane did not report configured/quiesced/faulted");
    b.watch_reset=false;
    check(b.command(2,0,0)==0 && b.t.exec_reset,"Stop hold failed after faulted plane drain");

    // Hidden fetches can underrun before the first good submitted frame. Those
    // misses do not fault a later healthy visible frame or discard its sequence.
    Bench hidden;
    check(hidden.command(19,0,1)==0 && hidden.command(2,0,1)==0,"hidden-underrun setup failed");
    hidden.watch_reset=true; hidden.cycles(200000);
    hidden.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=1;
    check(hidden.command(20,0,1)==0,"hidden-underrun enable failed");
    hidden.submit(1,0);
    for(unsigned i=0;i<2000000 && !hidden.t.underflows;++i) hidden.tick();
    check(hidden.t.underflows!=0 && !hidden.t.ui_active && !hidden.t.faulted,
          "hidden stalled frame became visible or faulted before completion");
    check(hidden.t.displayed_sequence==0,"hidden stalled frame acknowledged completion");
    auto hidden_writes=hidden.t.cpu_writes;
    hidden.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=0;
    hidden.wait_sequence(1);
    auto hidden_misses=hidden.t.underflows;
    hidden.cycles(1300000);
    check(hidden.t.ui_active && !hidden.t.faulted && hidden.t.underflows==hidden_misses,
          "pre-visible underrun incorrectly faulted a recovered visible frame");
    check(hidden.t.cpu_writes>hidden_writes && hidden.peek(0x5000)==0x5a,
          "hidden underrun/recovery stopped the Z80 or changed retained RAM");
    check(hidden.command(20)==0,"hidden-underrun recovery failed to close");

    // A PLL hold hides accepted responses from the reader. Hold contains the
    // guard; close and explicit Stop must still wait for physical completion.
    Bench hard;
    check(hard.command(19,0,1)==0 && hard.command(2,0,1)==0,"hard-fault setup failed");
    hard.watch_reset=true; hard.cycles(200000);
    check(hard.command(20,0,1)==0,"hard-fault enable failed");
    hard.submit(1,0); hard.wait_sequence(1);
    while(!hard.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__remaining)
        hard.tick();
    hard.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=1;
    auto hard_writes=hard.t.cpu_writes;
    hard.t.reset_hold=1; hard.cycles(20);
    check(hard.t.faulted && hard.t.rgb==hard.t.machine_rgb,"PLL hold failed to contain the plane");
    check(hard.command(2,0,0)==0x400004,"Stop hold abandoned hard-faulted DDR ownership");
    hard.begin_close(); hard.cycles(1300000);
    check(!hard.t.ui_active && hard.t.rgb==hard.t.machine_rgb,
          "hard-fault close failed to restore machine video at the frame boundary");
    check(!hard.acknowledged() && !hard.t.quiesced && hard.guard_busy(),
          "hard-fault close acknowledged while accepted responses were stalled");
    check(hard.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__remaining!=0,
          "hard-fault regression lost the stalled controller response");
    hard.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=0;
    hard.finish_fault_close();
    check(hard.command(18,9)==25,"hard-fault close/status failed");
    check(hard.t.cpu_writes>hard_writes && hard.peek(0x5000)==0x5a,
          "hard display fault stopped the Z80 or lost machine RAM");
    hard.t.reset_hold=0; hard.cycles(100);
    check(hard.command(20,0,1)==0x400004,"hard-faulted reader resumed after hidden responses");
    hard.watch_reset=false;
    check(hard.command(2,0,0)==0 && hard.t.exec_reset,"Stop hold failed after hard fault");

    // A command accepted by the guard but not yet by the controller is owned
    // traffic too. It must survive the hold, then its hidden response must drain.
    Bench queued;
    check(queued.command(19,0,1)==0 && queued.command(2,0,1)==0,"queued-fault setup failed");
    queued.watch_reset=true; queued.cycles(200000);
    queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__block_commands=1;
    queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=1;
    check(queued.command(20,0,1)==0,"queued-fault enable failed");
    queued.submit(1,0);
    for(unsigned i=0;i<2000000 && !queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__m_read;++i)
        queued.tick();
    check(queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__m_read &&
          queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__port0__DOT__reads_owed &&
          queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__accepted==0,
          "queued-fault regression did not create an unaccepted guard command");
    queued.t.reset_hold=1; queued.cycles(20);
    check(queued.t.faulted && queued.command(2,0,0)==0x400004,
          "queued hard fault permitted execution hold before DDR drain");
    queued.begin_close(); queued.cycles(300);
    check(!queued.acknowledged() && !queued.t.quiesced && queued.guard_busy(),
          "hard-fault close discarded a queued controller command");
    queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__block_commands=0;
    queued.cycles(300);
    check(queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__remaining &&
          !queued.acknowledged() && !queued.t.quiesced,
          "queued command close did not wait for its accepted response");
    queued.t.rootp->zx81_session_harness__DOT__display__DOT__memory__DOT__f2sdram__DOT__pause_responses=0;
    queued.finish_fault_close();
    check(queued.command(18,9)==25 && queued.peek(0x5000)==0x5a,
          "queued hard fault close lost status or retained RAM");
    queued.watch_reset=false;
    check(queued.command(2,0,0)==0 && queued.t.exec_reset,"Stop hold failed after queued fault drain");
    std::cout << "PASS ZX81 session display: complete-frame HDMI, shared raster, asynchronous CDC, live CPU/RAM, keyboard neutralization, cassette busy/replace/eject, drain/reopen/hidden-underrun recovery, physical held-response/queued-command fault drain\n";
}
