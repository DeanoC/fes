// SPDX-License-Identifier: GPL-3.0-or-later
// Independent coordinate oracle from Hatari's completed-access timestamps:
// admitted C=80/84/88 first change active x=C-83; marker x=0 stays index 2.
#include "Vst_shifter_output_sim_top.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>

struct Test {
    Vst_shifter_output_sim_top d;
    unsigned checks=0, samples=0, borders=0, wraps=0, published=0;
    unsigned frame=0, stamp=80;
    bool abort=false;
    void need(bool v,const char*m){++checks;if(!v){std::cerr<<"FAIL "<<m<<" frame="<<frame<<" line="<<d.native_line<<" cycle="<<d.native_cycle<<" token="<<d.token_line<<":"<<d.token_cycle<<" x="<<d.x<<" row="<<d.row<<" rgb="<<d.rgb<<'\n';std::exit(1);}}
    void fabric(){
        d.eval();
        if(d.pixel){
            int first=int(stamp)-83; // 80 -> -3; 84 -> 1; 88 -> 5.
            unsigned expected=d.x==0?0x1c7:(int(d.x)<first?0x040:0x007);
            need(d.rgb==expected,"current palette/index span");++samples;
        }
        unsigned oldframes=d.frames;
        d.memory_ready=d.memory_req;
        unsigned word=(d.memory_addr-0x10000/2)%80;
        d.memory_data=word%4==0?(word==0?0x7fff:0xffff):word==1?0x8000:0;
        d.clk_sys=1;d.eval();d.clk_sys=0;d.eval();
        if(d.frames!=oldframes){need(!d.pending&&!d.frame_write,"published before output drain");++published;}
        d.commit=0;d.native_pixel_ce=0;d.native_vblank=0;
    }
    void pulse(unsigned line,unsigned cycle){
        d.native_line=line;d.native_cycle=cycle;d.native_pixel_ce=1;
        d.native_vblank=line==0&&cycle==0;
        d.native_display=line>=63&&line<310&&cycle>=56&&cycle<376;
        d.hold=abort&&line==309&&cycle>=390;
        d.commit=0;
        // The palette bus admits phase 1 in FES's cycle label. Reset index 1
        // during blanking, then commit C+1 on the native output edge.
        if(cycle==5){d.commit=1;d.commit_index=1;d.commit_color=0x040;}
        if(cycle==stamp+1){d.commit=1;d.commit_index=1;d.commit_color=0x007;}
        // Border independently exercises side/bottom runs and persistent
        // colour across line wrap. C40 -> canvas x5; C420 -> canvas x385.
        if(cycle==1||cycle==41||cycle==421){d.commit=1;d.commit_index=0;d.commit_color=cycle==1?0x012:cycle==41?0x038:0x1c0;}
        need(!d.commit||(cycle&3)==1,"authored write outside admitted phase");
        fabric();
        if(d.tick&&!abort&&line>0&&cycle<28){need(d.token_line==line-1&&d.token_cycle==cycle+484,"token line wrap");++wraps;}
        // Variable fabric spacing proves elapsed system clocks do not advance
        // the calibrated stage. Its output corresponds to exactly 28 pulses.
        if(d.tick&&!abort&&line>=1){unsigned n=line*512+cycle-28;need(d.token_line==n/512&&d.token_cycle==n%512,"28 native pulse delay");}
        for(unsigned i=0;i<5+cycle%2;++i)fabric();
        if(d.hold)need(!d.pending&&!d.pixel&&!d.frame_write,"Hold failed to cancel output");
    }
    void pixel_clock(){d.clk_pixel=1;d.eval();d.clk_pixel=0;d.eval();}
    unsigned read(unsigned address){d.output_address=address;pixel_clock();return d.output_rgb;}
    void replay(){
        for(unsigned i=0;i<4;++i)pixel_clock();d.output_sof=1;pixel_clock();d.output_sof=0;
        need(d.front_valid&&d.sequence_number==frame,"frame publication identity");
        for(unsigned row: {0u,246u})for(unsigned x: {0u,1u,4u,5u,319u}) {
            int first=int(stamp)-83;
            unsigned expected=x==0?0x1c7:(int(x)<first?0x040:0x007);
            need(read(row*320+x)==expected,"immutable first/last bottom pixel");
        }
        for(unsigned row: {0u,29u,275u}){
            d.border_row=row;for(unsigned i=0;i<4;++i)pixel_clock();
            d.border_start=1;pixel_clock();d.border_start=0;pixel_clock();
            for(unsigned x=0;x<416;++x){
                d.border_x=x;d.border_ce=1;d.eval();
                unsigned expected=x<5?0x012:x<385?0x038:0x1c0;
                need(d.border_rgb==expected,"side/bottom border current palette span");++borders;
                pixel_clock();d.border_ce=0;pixel_clock();
            }
        }
    }
    void reset(){d.reset_sys=d.reset_pixel=1;d.hold=d.commit=d.native_pixel_ce=d.native_vblank=0;fabric();pixel_clock();d.reset_sys=d.reset_pixel=0;frame=0;}
    void run(){
        reset();
        for(unsigned c: {80u,84u,88u}){
            stamp=c;++frame;
            for(unsigned line=0;line<313;++line)for(unsigned cycle=0;cycle<512;++cycle)pulse(line,cycle);
            need(d.frames==frame,"complete opened-bottom frame count");replay();
        }
        // Abort while the final pixel is still in flight. No partial bank may
        // publish, and a subsequent complete frame must recover.
        abort=true;++frame;
        for(unsigned line=0;line<313;++line)for(unsigned cycle=0;cycle<512;++cycle)pulse(line,cycle);
        need(d.frames==3,"Hold published a partial bottom line");abort=false;
        ++frame;for(unsigned line=0;line<313;++line)for(unsigned cycle=0;cycle<512;++cycle)pulse(line,cycle);
        need(d.frames==4,"Hold recovery did not publish");replay();
        // Reset with live tokens cancels the pipeline and publication state.
        for(unsigned line=0;line<=63;++line)for(unsigned cycle=0;cycle<(line==63?100u:512u);++cycle)pulse(line,cycle);
        need(d.pending,"reset did not exercise live tokens");reset();
        need(!d.pending&&!d.pixel&&!d.frame_write&&!d.front_valid&&d.frames==0,"reset leaked capture state");
        ++frame;for(unsigned line=0;line<313;++line)for(unsigned cycle=0;cycle<512;++cycle)pulse(line,cycle);
        need(d.frames==1,"reset recovery did not publish");replay();
        std::cout<<"Shifter output PASS: "<<checks<<" checks, "<<samples<<" pixel samples, "<<borders<<" replay border pixels, "<<wraps<<" wrapped tokens, "<<published<<" drained publications\n";
    }
};
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);Test t;t.run();}
