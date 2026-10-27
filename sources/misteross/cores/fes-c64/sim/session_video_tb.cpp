// SPDX-License-Identifier: MIT
#include "Vc64_session_video_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <deque>
#include <iostream>
static void check(bool ok, const char *why) { if (!ok) { std::cerr << why << '\n'; std::exit(1); } }
struct Bench {
    Vc64_session_video_top t;
    uint64_t phase = 17000000, different = 0;
    std::deque<uint32_t> words;
    Bench() {
        t.clk_sys=0; t.pixel_clk=0; t.reset_hold=1; t.request=0; t.opcode=0; t.index=0; t.argument=0; t.eval();
        cycles(100); t.reset_hold=0;
    }
    void tick() {
        phase+=52224000;
        if (phase>=74250000) { phase-=74250000; t.clk_sys=0;t.eval();t.clk_sys=1;t.eval(); }
        words.push_back(t.machine); if(words.size()>3)words.pop_front();
        t.pixel_clk=0;t.eval();t.pixel_clk=1;t.eval();
        if(words.size()==3) {
            check((t.video>>24)==(words.front()>>24), "C64 sync changed through plane");
            if(!t.ui_active)check(t.video==words.front(),"closed plane changed C64 pixels");
            else if((t.video&0xffffff)!=(words.front()&0xffffff))++different;
        }
        check(!t.faulted,"C64 plane faulted");
    }
    void cycles(unsigned n) { while(n--)tick(); }
    unsigned command(unsigned op,unsigned index=0,unsigned arg=0) {
        t.opcode=op;t.index=index;t.argument=arg;t.request=1;
        for(unsigned i=0;i<3000000&&!t.response_valid;++i)tick();
        check(t.response_valid&&!t.response_error,"C64 display command failed");
        unsigned value=t.response_data;t.request=0;
        for(unsigned i=0;i<100&&!t.quiesced&&t.response_valid;++i)tick();
        cycles(16);return value;
    }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);Bench b;
    b.cycles(2600000);check(b.t.locked,"plane did not lock to actual C64 raster");
    check(b.command(18,14)==160&&b.command(18,15)==0&&b.command(18,16)==960&&b.command(18,17)==720,"C64 picture geometry");
    b.command(19,0,1);b.command(20,0,1);b.command(21,0,1);b.command(21,1,0);b.command(21,2,0);
    b.cycles(2600000);check(b.t.displayed_sequence==1&&b.t.ui_active,"C64 overlay never displayed");
    check(b.different>100,"C64 overlay did not alter pixels");
    b.command(20,0,0);b.cycles(1300000);check(!b.t.ui_active&&b.t.quiesced&&!b.t.underflows,"C64 close did not drain");
    std::cout<<"PASS C64 session video: actual VIC raster lock, geometry, fixed sync latency, displayed DDR frame and exact closed return\n";
}
