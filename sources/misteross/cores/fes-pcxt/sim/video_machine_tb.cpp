// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vvideo_machine_top.h"
#include "verilated.h"
#include <array>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <vector>
static void check(bool b,const char*why){if(!b)throw std::runtime_error(why);}
int main(int argc,char**argv){
    Verilated::commandArgs(argc,argv);
    try{
        check(argc==2,"diagnostic ROM required");
        std::ifstream f(argv[1],std::ios::binary);
        const std::vector<uint8_t> rom{std::istreambuf_iterator<char>(f),{}};
        check(rom.size()==65536,"ROM size");
        for(unsigned waits:{0u,137u}){
            Vvideo_machine_top t;
            std::array<uint8_t,1<<20> memory{};
            std::copy(rom.begin(),rom.end(),memory.begin()+0xf0000);
            t.reset=1;t.reset_pixel=1;t.hold=0;t.memory_ready=0;t.memory_done=0;
            unsigned pixel_phase=0;
            std::array<unsigned,2> outx{},outy{},outframes{},checked{};
            auto step=[&](unsigned level){
                t.clk=level;pixel_phase+=1485;bool edge=false;
                if(pixel_phase>=2000){pixel_phase-=2000;t.clk_pixel=!t.clk_pixel;edge=t.clk_pixel;}
                t.eval();
                if(!edge || t.reset_pixel || memory[0x600]!=0xaa)return;
                for(unsigned c=0;c<2;c++){
                    uint32_t r=c?t.mda_video_request:t.cga_video_request;
                    if(r&(1u<<28)){if(outframes[c]>3)check(outx[c]==0 && outy[c]==750,"HDMI frame geometry");
                        ++outframes[c];outx[c]=outy[c]=0;}
                    if(!outframes[c])continue;
                    unsigned x=outx[c],y=outy[c],want=0;
                    if(outframes[c]>3){
                        if(x>=160 && x<1120 && y<720){
                            unsigned sx=(x-160)*(c?720u:640u)/960;
                            bool ink=c ? sx%9==0 || sx%9>=7 : (sx%16<8 ? !(sx&1):bool(sx&1));
                            want=ink?0xffffff:0;
                        }
                        check((r&0xffffff)==want,"8088 ISA-to-720p pixel");++checked[c];
                    }
                    if(++outx[c]==1650){outx[c]=0;++outy[c];}
                }
            };
            for(unsigned i=0;i<32;i++){step(0);step(1);}
            t.reset=0;t.reset_pixel=0;
            bool pending=false,write=false;unsigned address=0,data=0,delay=0,transactions=0;
            std::array<unsigned,2> dots{},lines{},frames{};
            std::array<bool,2> started{};
            unsigned ticks=0;
            for(;ticks<40000000;ticks++){
                t.memory_done=0;
                if(pending){
                    if(delay)--delay;
                    else{
                        if(write && address<0xa0000)memory[address]=data;
                        t.memory_rdata=memory[address];t.memory_done=1;pending=false;
                    }
                }
                t.memory_ready=!pending && (ticks%7!=0);
                step(0);
                if(t.memory_valid && t.memory_ready){
                    check(!pending,"overlapping storage request");
                    check(t.address<0xa0000 || t.address>=0xf0000,"video access escaped ISA");
                    pending=true;address=t.address;data=t.wdata;write=t.write_cycle;delay=waits ? ticks%waits:0;
                    ++transactions;
                }
                t.cga_font_data=0x81;t.mda_font_data=0x81;
                t.eval();step(1);
                check(!t.fault,"CPU or DMA fault");check(memory[0x600]!=0xee,"8088 diagnostic failure");
                if(memory[0x600]!=0xaa)continue;
                for(unsigned c=0;c<2;c++){
                    unsigned mask=1u<<c;
                    if(!(t.pixel_ce&mask))continue;
                    if(t.sof&mask){
                        if(started[c]){check(lines[c]==(c?370u:262u) && dots[c]==0,"CPU frame geometry");++frames[c];}
                        started[c]=true;dots[c]=lines[c]=0;
                    }
                    if(!started[c])continue;
                    unsigned x=dots[c],y=lines[c],want=0;
                    if(c && x<720 && y<350)want=(x%9==0 || x%9>=7)?15:0;
                    if(!c && x<640 && y<200)want=((x%16<8) ? !(x&1):bool(x&1))?15:0;
                    check(((t.rgbi>>(c*4))&15)==want,"CPU-programmed pixel");
                    if(t.eol&mask){dots[c]=0;++lines[c];}else ++dots[c];
                }
                if(checked[0]>=2475000 && checked[1]>=2475000)break;
            }
            check(memory[0x600]==0xaa && checked[0]>=2475000 && checked[1]>=2475000,"8088 display timeout");
            std::cout<<"8088 CGA then MDA: waits="<<waits<<", ticks="<<ticks<<", external storage cycles="<<transactions<<", guest and 720p frames verified\n";
        }
    }catch(const std::exception&e){std::cerr<<e.what()<<'\n';return 1;}
}
