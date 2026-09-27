// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_menu_video.h"
#include "verilated.h"
#include <deque>
#include <iostream>
#include <fstream>
#include <vector>
#include <string>
#include <stdexcept>
static void check(bool ok, const char* why) { if (!ok) throw std::runtime_error(why); }
struct Beat { unsigned index, slot; };
struct Bench {
    Vfes_menu_video t;
    std::deque<Beat> beats;
    unsigned h=0,v=720;
    unsigned frame=0,active=0,black=0;
    uint32_t seq=0;
    uint64_t cycles=0;
    bool stall=false, inspect=false;
    std::string output;
    std::vector<char> image;
    bool saved[2]={false,false};
    static uint32_t pixel(unsigned slot,unsigned index) {
        return (slot ? 0x00654321u : 0x00123456u) ^ (index & 0xffffu);
    }
    void tick() {
        t.clk=0; t.waitrequest=stall;
        t.readdatavalid=!beats.empty()&&!stall;
        if(t.readdatavalid) for(unsigned lane=0;lane<4;++lane)
            t.readdata[lane]=pixel(beats.front().slot,beats.front().index*4+lane);
        t.eval();
        if(inspect) {
            check(t.de == (h<1280 && v<720),"DE geometry wrong");
            check(t.hs == (h>=1390 && h<1430),"HS geometry/polarity wrong");
            check(t.vs == (v>=725 && v<730),"VS geometry/polarity wrong");
            if(h==0 && v==0) {seq=t.displayed_sequence;active=black=0;image.clear();}
            else check(t.displayed_sequence==seq,"sequence changed within frame");
            if(t.de) {
                ++active;
                image.push_back(char(t.rgb>>16)); image.push_back(char(t.rgb>>8)); image.push_back(char(t.rgb));
                unsigned index=v*1280+h;
                if(t.rgb==0) ++black;
                else check(t.rgb == (pixel(seq==42,index)&0xffffffu),"mixed frame or shifted pixels");
            }
            if(h==1649 && v==749) {
                ++frame;
                unsigned slot=seq==42;
                if(active==921600 && black==0 && !saved[slot]) {
                    std::ofstream file(output + "/slot" + std::to_string(slot) + ".ppm",std::ios::binary);
                    file << "P6\n1280 720\n255\n";
                    file.write(image.data(),image.size());
                    check(bool(file),"frame artifact write failed");saved[slot]=true;
                }
            }
        }
        bool returning=t.readdatavalid;
        if(t.read&&!t.waitrequest) {
            uint32_t base=t.address*16u;
            unsigned slot=base>=0x30400000u;
            unsigned index=(base-(slot?0x30400000u:0x30000000u))/16;
            check(index+t.burstcount<=230400,"scanout crossed frame end");
            for(unsigned i=0;i<t.burstcount;++i) beats.push_back({index+i,slot});
        }
        if(returning) beats.pop_front();
        t.clk=1;t.eval();++cycles;
        if(t.rst){h=0;v=720;} else if(++h==1650){h=0;if(++v==750)v=0;}
    }
    void next_frame(){unsigned f=frame;uint64_t limit=cycles+1300000;while(frame==f && cycles<limit)tick();check(frame!=f,"frame timing timed out");}
};
int main(int argc,char**argv) {
    Verilated::commandArgs(argc,argv);
    try {
        check(argc==2,"expected artifact output directory");
        Bench b;b.output=argv[1];b.t.rst=1;b.tick();b.t.rst=0;b.t.enable=1;b.inspect=true;
        b.next_frame();b.next_frame();
        check(b.active==921600 && b.black==0 && b.t.underflows==0,"unstalled frame underflow");
        b.t.submit_slot=1;b.t.submit_sequence=42;b.t.submit_valid=1;
        check(b.t.submit_ready,"initial submit not ready");b.tick();b.t.submit_valid=0;
        check(!b.t.submit_ready,"pending submit not locked");
        b.t.submit_slot=0;b.t.submit_sequence=99;b.t.submit_valid=1;b.tick();b.t.submit_valid=0;
        b.next_frame();b.next_frame();
        check(b.t.displayed_sequence==42 && b.black==0,"frame switch did not complete");
        std::cout<<"PASS video_timing/frame_switch/busy_submit\n";
        b.stall=true;b.next_frame();b.next_frame();
        check(b.black>0 && b.t.underflows>0,"stalls did not produce black underflow");
        b.stall=false;b.next_frame();b.next_frame();b.next_frame();
        check(b.black==0,"late responses did not recover without stale pixels");
        std::cout<<"PASS underflow/late_response\n";
        // Reset with a command held and responses queued; retain fixed timing on recovery.
        b.stall=true;
        for(unsigned i=0;i<500;++i)b.tick();
        b.inspect=false;b.t.rst=1;b.tick();b.tick();
        b.stall=false;
        for(unsigned i=0;i<1000;++i)b.tick();
        check(b.beats.empty(),"reset did not drain old responses");
        b.t.rst=0;b.seq=b.t.displayed_sequence;b.inspect=true;b.next_frame();b.next_frame();
        check(b.black==0 && b.t.displayed_sequence==0,"reset frame retained old sequence/pixels");
        std::cout<<"PASS reset_drain/restart\n";
        b.stall=true;
        while(!b.t.de)b.tick();
        b.t.underflows=0xfffffffeu;
        for(unsigned i=0;i<3000;++i)b.tick();
        check(b.t.underflows==0xffffffffu,"underflow counter wrapped");
        b.stall=false;
        b.t.submit_slot=1;b.t.submit_sequence=100;b.t.submit_valid=1;b.tick();b.t.submit_valid=0;
        uint32_t before=b.t.displayed_sequence;
        b.t.quiesce=1;
        for(unsigned i=0;i<2000&&!b.t.quiesced;++i)b.tick();
        check(b.t.quiesced&&b.beats.empty(),"quiesce did not drain reads");
        check(b.t.displayed_sequence==before,"quiesce acknowledged pending frame");
        for(unsigned i=0;i<500;++i){b.tick();check(!b.t.read,"quiesced reader restarted");}
        check(b.saved[0]&&b.saved[1],"both frame artifacts not saved");
        std::cout<<"PASS quiesce\n";
    }catch(const std::exception&e){std::cerr<<e.what()<<'\n';return 1;}
}
