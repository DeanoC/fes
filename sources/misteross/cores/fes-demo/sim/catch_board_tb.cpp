// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
static void check(bool ok,const char* m){if(!ok){std::cerr<<m<<'\n';std::exit(1);}}
int main(){
    Vtop d; auto& r=*d.rootp; d.FPGA_CLK1_50=0; d.eval();
    bool toggle=false;unsigned serial_ones=0, paddle_pixels=0;
    auto cycle=[&](){
        for(unsigned i=0;i<6;++i){
            r.top__DOT__video_clock__DOT__outclk_0=0;d.eval();
            r.top__DOT__video_clock__DOT__outclk_0=1;d.eval();
            if(d.HDMI_TX_DE && d.HDMI_TX_D==0x40c0ff) ++paddle_pixels;
        }
        r.top__DOT__audio_clock__DOT__clk=0;d.eval();
        r.top__DOT__audio_clock__DOT__clk=1;d.eval();
        serial_ones+=d.HDMI_I2S;
    };
    auto command=[&](unsigned op,unsigned arg,unsigned index){
        toggle=!toggle;r.top__DOT__hps_gp__DOT__gp_out=(toggle?0x80000000u:0)|(op<<24)|(index<<16)|arg;
        for(unsigned i=0;i<8;++i)cycle();
        unsigned reply=r.top__DOT__hps_gp__DOT__observed_gpi;
        check(bool(reply&0x800000)==toggle && !(reply&0x400000),"GP command rejected");return reply&0xffff;
    };
    r.top__DOT__audio_clock__DOT__locked=1;
    for(unsigned i=0;i<800;++i)cycle();check(!serial_ones,"held startup silent");
    check(command(1,0,7)==0x13,"game capabilities match manifest");
    check(command(2,1,0)==0,"release ack");
    // Run the actual fixed raster until the first centered target is caught.
    for(unsigned i=0;i<19600000;++i)cycle();
    check(r.top__DOT__core__DOT__game__DOT__score==1,"raster frame tick reaches game");
    check(serial_ones>0,"game event reaches board I2S pins");
    check(paddle_pixels>0,"game pixels reach board HDMI output");
    check(command(3,8,0)==0,"Right input ack");
    for(unsigned i=0;i<420000;++i)cycle();
    check(r.top__DOT__core__DOT__game__DOT__paddle>140,"generic gamepad moves paddle");
    check(command(3,0,0)==0,"neutral input ack");
    check(command(2,0,0)==0,"hold ack");for(unsigned i=0;i<512;++i)cycle();
    check(r.top__DOT__core__DOT__game__DOT__score==0,"hold resets game");
    serial_ones=0;for(unsigned i=0;i<2000;++i)cycle();check(!serial_ones,"held board silence");
    std::cout<<"Catch board mailbox, exact raster, gamepad, event audio and hold passed\n";
}
