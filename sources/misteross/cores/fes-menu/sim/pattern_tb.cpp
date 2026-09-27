// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_menu_video.h"
#include "verilated.h"
#include <iostream>
#include <stdexcept>
static void check(bool ok,const char* why){if(!ok)throw std::runtime_error(why);}
static unsigned color(unsigned x,unsigned y,unsigned slot){
    if(x==0||x==1279||y==0||y==719)return 0xffffff;
    if(x<64&&y<64)return slot?0xff00ff:0x00ff00;
    if(y>=560)return ((x/16)^(y/16))&1?0xffffff:0;
    const unsigned bars[]={0xffffff,0xffff00,0x00ffff,0x00ff00,0xff00ff,0xff0000,0x0000ff,0};
    return bars[x/160];
}
int main(int argc,char**argv){
    Verilated::commandArgs(argc,argv);
    try{
        Vfes_menu_video t;unsigned h=0,v=720,frames=0,seen=0;unsigned seq=0;
        auto tick=[&]{
            t.clk=0;t.eval();
            check(!t.read&&t.address==0&&t.burstcount==0,"pattern mode emitted a DDR command");
            if(!t.rst){
                check(t.de==(h<1280&&v<720),"pattern geometry wrong");
                check(t.hs==(h>=1390&&h<1430)&&t.vs==(v>=725&&v<730),"pattern sync wrong");
                if(h==0&&v==0){seq=t.displayed_sequence;seen=0;}
                if(t.de){check(t.rgb==color(h,v,seq==42),"pattern pixel/slot mismatch");++seen;}
                if(h==1649&&v==749)++frames;
            }
            t.clk=1;t.eval();
            if(t.rst){h=0;v=720;}else if(++h==1650){h=0;if(++v==750)v=0;}
        };
        t.waitrequest=1;t.readdatavalid=1;
        t.rst=1;tick();t.rst=0;t.enable=1;
        while(frames<2)tick();
        check(seen==921600&&t.underflows==0,"pattern frame underflow");
        t.submit_slot=1;t.submit_sequence=42;t.submit_valid=1;tick();t.submit_valid=0;
        while(frames<4)tick();
        check(t.displayed_sequence==42&&t.underflows==0,"pattern frame switch failed");
        t.quiesce=1;
        for(unsigned i=0;i<1000&&!t.quiesced;++i){t.clk=0;t.eval();t.clk=1;t.eval();}
        check(t.quiesced&&!t.read,"pattern mode did not quiesce");
        std::cout<<"PASS pattern pixels/timing/frame_switch/no_DDR/quiesce\n";
    }catch(const std::exception&e){std::cerr<<e.what()<<'\n';return 1;}
}
