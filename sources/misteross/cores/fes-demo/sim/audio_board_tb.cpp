// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
static void check(bool ok,const char* m){if(!ok){std::cerr<<m<<'\n';std::exit(1);}}
int main(){
    Vtop d;auto &r=*d.rootp;d.FPGA_CLK1_50=0;d.eval();
    bool toggle=false;unsigned serial_ones=0,video_edges=0;bool old_hs=d.HDMI_TX_HS;
    auto cycle=[&](){
        // Different phase/frequency clocks; no audio depends on pixel edge.
        for(unsigned i=0;i<6;++i){
            r.top__DOT__video_clock__DOT__outclk_0=0;d.eval();
            r.top__DOT__video_clock__DOT__outclk_0=1;d.eval();
            if(old_hs!=bool(d.HDMI_TX_HS)){++video_edges;old_hs=d.HDMI_TX_HS;}
        }
        r.top__DOT__audio_clock__DOT__clk=0;d.eval();check(!d.HDMI_MCLK,"MCLK low wiring");
        r.top__DOT__audio_clock__DOT__clk=1;d.eval();check(d.HDMI_MCLK,"MCLK high wiring");
        serial_ones+=d.HDMI_I2S;
    };
    auto command=[&](unsigned op,unsigned arg,unsigned index){
        toggle=!toggle;r.top__DOT__hps_gp__DOT__gp_out=(toggle?0x80000000u:0)|(op<<24)|(index<<16)|arg;
        for(unsigned i=0;i<8;++i)cycle();
        unsigned reply=r.top__DOT__hps_gp__DOT__observed_gpi;
        check(bool(reply&0x800000)==toggle && !(reply&0x400000),"GP command rejected");return reply&0xffff;
    };
    r.top__DOT__audio_clock__DOT__locked=1;
    for(unsigned i=0;i<800;++i)cycle();check(!serial_ones,"startup must be held silent");
    check(command(1,0,7)==0x13,"audio package live capabilities must exactly match manifest");
    check(command(2,1,0)==0,"release ack");
    for(unsigned i=0;i<2000;++i)cycle();check(serial_ones>0,"released tone must reach board pins");
    check(command(3,8,0)==0,"Right input ack");
    check(command(2,0,0)==0,"hold ack");for(unsigned i=0;i<512;++i)cycle();
    serial_ones=0;unsigned edges_before=video_edges;
    for(unsigned i=0;i<2000;++i)cycle();check(!serial_ones,"held board output silence");
    check(video_edges>edges_before,"video timing must continue during audio hold");
    std::cout<<"Audio board capabilities, GP hold/release/input, independent clocks, pins and ongoing video passed\n";
}
