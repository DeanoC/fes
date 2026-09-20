// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_demo_core.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
static void require(bool ok,const char* msg) { if(!ok){std::cerr<<msg<<'\n';std::exit(1);} }
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv); Vfes_demo_core d;
    d.pixel_clk=0;d.exec_reset=1;d.buttons=0;d.palette=0xffffff;d.eval();
    uint64_t hashes[3]={};
    for(unsigned frame=0;frame<3;++frame) {
        unsigned active=0,hs=0,vs=0,lit=0;
        for(unsigned y=0;y<750;++y) for(unsigned x=0;x<1650;++x) {
            require(bool(d.hdmi_de)==(x<1280&&y<720),"data enable timing");
            require(bool(d.hdmi_hs)==(x>=1390&&x<1430),"hsync timing");
            require(bool(d.hdmi_vs)==(y>=725&&y<730),"vsync timing");
            active+=d.hdmi_de;hs+=d.hdmi_hs;vs+=d.hdmi_vs;lit+=d.hdmi_rgb!=0;
            if(x<160||x>=1120||y>=720)require(!d.hdmi_rgb,"blanking/sidebars");
            hashes[frame]=hashes[frame]*33+d.hdmi_rgb;
            d.pixel_clk=1;d.eval();d.pixel_clk=0;d.eval();
        }
        require(active==1280*720&&hs==40*750&&vs==5*1650,"frame dimensions");
        require(frame?lit>0:lit==0,"reset/active image");
        d.exec_reset=0;d.eval();
    }
    require(hashes[1]!=hashes[2],"autonomous animation did not advance");
    std::cout<<"application video passed fixed720p reset and autonomous animation\n";
}
