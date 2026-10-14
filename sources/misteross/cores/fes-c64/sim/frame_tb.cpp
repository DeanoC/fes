// SPDX-License-Identifier: MIT
#include "Vc64_frame_store.h"
#include "verilated.h"
#include <cstdio>
#include <initializer_list>
#include <stdexcept>
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    for(unsigned native_period : {1401u,1601u,3201u}) {
        Vc64_frame_store t;
        t.clk_sys=0;t.pixel_clk=0;t.reset=1;t.write_enable=0;t.frame_done=0;
        t.frame_start=0;t.write_address=0;t.read_address=0;t.write_data=0;t.eval();
        unsigned produced=0,displayed=0,expected=0,expected_position=0,reads=0;
        bool collecting=false;
        for(unsigned time=0;time<2000000;++time) {
            bool paused=time>=500000&&time<800000;
            bool sys_rise=time%6==0, video_rise=!paused&&time%10==0;
            t.clk_sys=(time%6)<3;t.pixel_clk=!paused&&(time%10)<5;
            if(time==101)t.reset=0;
            unsigned sys_tick=time/6,video_tick=time/10;
            unsigned native_tick=sys_tick%native_period,display_tick=video_tick%1603;
            t.write_enable=!t.reset&&native_tick<1024;
            t.write_address=native_tick;t.write_data=((sys_tick/native_period)%15+1)^((native_tick>>6)&15);
            t.frame_done=!t.reset&&native_tick==1024&&sys_tick/native_period>0;
            t.frame_start=!t.reset&&display_tick==0;
            t.read_address=display_tick<1024?display_tick:0;
            if(sys_rise&&t.frame_done)++produced;
            // Skip the first two read clocks after frame_start: the registered
            // page selection needs to settle, as it does during HDMI blanking.
            if(video_rise) {
                collecting=display_tick>=2&&display_tick<1024&&!t.reset;
                expected_position=display_tick;
            }
            t.eval();
            if(video_rise&&collecting) {
                if(expected_position==2){expected=t.read_data^((expected_position>>6)&15);if(expected)++displayed;}
                unsigned wanted=expected?(expected^((expected_position>>6)&15)):0;
                if(t.read_data!=wanted) {
                    std::printf("period%u time%u reader%u writer%u sample%u expected%u got%u\n",native_period,time,t.read_page,t.write_page,expected_position,wanted,t.read_data);
                    std::fflush(stdout);throw std::runtime_error("mixed native frames within one HDMI frame");
                }
                ++reads;
            }
            if(!t.reset&&t.write_page==t.read_page)
                throw std::runtime_error("writer owns HDMI reader page");
        }
        if(produced<100||displayed<50||reads<90000)throw std::runtime_error("insufficient frame coverage");
        std::printf("Native frame handoff period%u: %u produced, %u displayed, %u coherent pixel reads\n",native_period,produced,displayed,reads);
    }
    Vc64_frame_store t;
    t.clk_sys=0;t.pixel_clk=0;t.reset=1;t.write_enable=0;t.frame_done=0;
    t.frame_start=0;t.write_address=0;t.read_address=0;t.write_data=0;t.eval();
    auto sys=[&](){t.clk_sys=0;t.eval();t.clk_sys=1;t.eval();};
    auto video=[&](){t.pixel_clk=0;t.eval();t.pixel_clk=1;t.eval();};
    sys();video();t.reset=0;
    auto pattern=[](unsigned frame,unsigned address){return (frame^address^(address>>8)^(address>>11))&15;};
    for(unsigned frame=1;frame<=3;++frame){
        for(unsigned address=0;address<104448;++address){
            t.write_address=address;t.write_data=pattern(frame,address);t.write_enable=1;sys();
        }
        t.write_enable=0;t.frame_done=1;sys();t.frame_done=0;
        t.frame_start=1;video();video();video();t.frame_start=0;video();
        for(unsigned address=0;address<104448;++address){
            t.read_address=address;video();
            if(t.read_data!=pattern(frame,address)){
                std::printf("full page frame%u address%04x got%x expected%x\n",frame,address,t.read_data,pattern(frame,address));
                throw std::runtime_error("frame bank/address mapping mismatch");
            }
        }
        sys();sys();sys();
    }
    std::puts("Native frame memory: all 313344 addresses across three pages match indexed patterns");

}
