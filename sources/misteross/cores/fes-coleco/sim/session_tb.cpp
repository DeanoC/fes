// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_session_harness.h"
#include "Vcoleco_session_harness___024root.h"
#include <verilated.h>
#include <cstdint>
#include <cstdlib>
#include <deque>
#include <iostream>
#include <string>

static void check(bool condition, const std::string &message) {
    if (!condition) { std::cerr << "Coleco session display: " << message << '\n'; std::exit(1); }
}
struct Bench {
    Vcoleco_session_harness t;
    uint64_t sys_phase = 17000000;
    bool toggle = false;
    // The last three machine words and the plane's raster coordinate for each.
    // HDMI carries the oldest; h and v are its coordinate.
    unsigned h = 0, v = 0;
    std::deque<uint32_t> history;
    std::deque<uint32_t> coordinates;
    Bench() {
        t.clk_sys=0; t.pixel_clk=0; t.reset_hold=1; t.gpo=0; t.eval();
        // The launcher draws opaque frames today.
        t.rootp->coleco_session_harness__DOT__memory__DOT__f2sdram__DOT__alpha=0xff;
        for(unsigned i=0;i<100;++i) tick();
        t.reset_hold=0;
        // The plane composites only after a whole frame confirms the part's raster.
        for(unsigned i=0;i<4000000 && !t.rootp->coleco_session_harness__DOT__display__DOT__locked;++i) tick();
        check(t.rootp->coleco_session_harness__DOT__display__DOT__locked,"plane did not lock to the native part's raster");
    }
    // The output carries the machine word captured three edges ago.
    bool machine_output() const { return history.size()==3 && t.hdmi_word==history.front(); }
    void tick() {
        // Unrelated 52.224/74.25 MHz clocks; request timing walks across phases.
        sys_phase += 52224000;
        if (sys_phase >= 74250000) {
            sys_phase -= 74250000;
            t.clk_sys=0; t.eval(); t.clk_sys=1; t.eval();
        }
        history.push_back(t.machine_word);
        if (history.size()>3) history.pop_front();
        t.pixel_clk=0; t.eval(); t.pixel_clk=1; t.eval();
        // The plane now holds that word with its raster coordinate.
        const unsigned ph=t.rootp->coleco_session_harness__DOT__display__DOT__raster_h;
        const unsigned pv=t.rootp->coleco_session_harness__DOT__display__DOT__raster_v;
        coordinates.push_back(ph<<16|pv);
        if (coordinates.size()>3) coordinates.pop_front();
        h=coordinates.front()>>16; v=coordinates.front()&65535;
        if (!t.reset_hold && t.rootp->coleco_session_harness__DOT__display__DOT__locked) {
            // The part's raster is CTA 720p60, the only raster the plane accepts.
            const uint32_t word=history.back();
            check(bool((word>>24)&1)==(ph<1280 && pv<720), "part DE left CTA 720p60");
            check(bool((word>>25)&1)==(ph>=1390 && ph<1430), "part HS left CTA 720p60");
            check(bool((word>>26)&1)==(pv>=725 && pv<730), "part VS left CTA 720p60");
        }
        if (history.size()==3)
            check((t.hdmi_word>>24)==(history.front()>>24),"HDMI sync lost its fixed latency");
        check(!t.rootp->coleco_session_harness__DOT__memory__DOT__f2sdram__DOT__violation,
              "DDR bounds, writes or unused-port violation");
    }
    void cycles(unsigned n) { for(unsigned i=0;i<n;++i) tick(); }
    unsigned command(unsigned op,unsigned index=0,unsigned arg=0) {
        toggle=!toggle;
        t.gpo=unsigned(toggle)<<31 | op<<24 | index<<16 | arg;
        for(unsigned i=0;i<4000000 && bool(t.gpi&0x800000)!=toggle;++i) tick();
        check(bool(t.gpi&0x800000)==toggle,"GP/CDC acknowledgement timeout");
        unsigned result=t.gpi&0x40ffff;
        cycles(12); return result;
    }
    void submit(unsigned seq,unsigned slot) {
        check(command(21,0,seq&65535)==0,"sequence low rejected");
        check(command(21,1,seq>>16)==0,"sequence high rejected");
        check(command(21,2,slot)==0,"slot commit rejected");
    }
    void wait_sequence(unsigned seq) {
        for(unsigned i=0;i<4000000 && t.displayed_sequence!=seq;++i) tick();
        check(t.displayed_sequence==seq,"full frame did not complete");
        for(unsigned i=0;i<1300000 && !t.ui_active;++i) tick();
        check(t.ui_active,"completed frame did not become visible");
    }
    // HDMI at the first visible pixel of a frame.
    uint32_t frame_origin() {
        do tick(); while(h!=0 || v!=0);
        return t.hdmi_word&0xffffff;
    }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Bench b;
    // Fixed video, audio, controller and keypad ports, HPS DDR and the
    // session plane (bit 11); never the idle menu's bit 9.
    check(b.command(1,7)==0x972,"session display/DDR capability bits wrong");
    check(b.command(18,0)==1280,"display geometry CDC mismatch");
    check(b.command(19,0,1)==0,"fixed display layout configuration rejected");
    check(b.command(22,0,0)==0x400001,"a session plane accepted the menu blitter opcode");
    check(b.command(2,0,1)==0 && !b.t.exec_reset,"execution release rejected");
    check(b.command(13,0,0x20)==0 && b.t.controller_buttons==0x20,"controller press rejected");
    check(b.command(13,0,0)==0 && b.t.controller_buttons==0,"controller release rejected");
    const auto frames=b.t.source_frames;
    check(b.command(20,0,1)==0,"display enable rejected");
    check(!b.t.ui_active,"enable exposed an uncompleted frame");
    check(b.command(2,0,0)==0x400004,"execution hold abandoned an active display");
    b.submit(1,0);
    b.wait_sequence(1);
    check(b.t.underflows==0 && !b.t.faulted,"initial scanout underflowed");
    check((b.frame_origin())==0x22,"slot 0 pixels missing from HDMI selection");
    // The machine keeps running under the plane: the source still delivers
    // frames and no reset reached it.
    b.cycles(1300000);
    check(b.t.source_frames>frames+1 && !b.t.exec_reset,"machine stopped while the launcher was shown");
    b.submit(2,1); b.wait_sequence(2);
    check((b.frame_origin())==0x77,"slot 1 pixels missing from HDMI selection");
    check(b.command(20)==0,"display close rejected");
    check(!b.t.ui_active && b.t.quiesced,"close preceded machine return");
    b.cycles(3);
    check(b.machine_output(),"close failed to return the part's pixels");
    // Inside the machine picture the part's own pixels are back.
    do b.tick(); while(!(b.h==640 && b.v==360));
    check(b.machine_output() && (b.t.hdmi_word&0xffffff)!=0x22 && (b.t.hdmi_word&0xffffff)!=0x77,
          "closed plane still showed launcher pixels in the machine picture");
    check(b.command(2,0,0)==0 && b.t.exec_reset,"Stop hold failed after the plane drained");
    std::cout << "PASS Coleco session display: Coleco mailbox capability bit 11, native part raster lock, fixed latency, launcher frames over the running machine, execution hold gate, close returns the part's pixels\n";
}
