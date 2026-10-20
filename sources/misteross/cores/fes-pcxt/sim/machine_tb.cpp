// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vmachine_top.h"
#include "verilated.h"
#include <array>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <vector>
static void check(bool ok,const char* why) {if(!ok)throw std::runtime_error(why);}
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    try {
        check(argc==2,"ROM path required");std::ifstream f(argv[1],std::ios::binary);
        std::vector<uint8_t> rom((std::istreambuf_iterator<char>(f)),{});
        check(rom.size()==65536,"ROM size");
        struct Profile { unsigned waits, switches; };
        for(Profile profile: {Profile{0,0x2d},Profile{31,0x2d},Profile{173,0x2d},Profile{0,0x73}}) {
            unsigned waits=profile.waits, switches=profile.switches;
            Vmachine_top d;std::array<uint8_t,1<<20> mem{};
            std::copy(rom.begin(),rom.end(),mem.begin()+0xf0000);
            mem[0x620]=switches>>4; mem[0x621]=switches&0x0f;
            bool pending=false,passed=false,saw_halt=false,ack_pending=false;
            unsigned pa=0,pd=0,delay=0,ticks=0,acks=0,timer_vectors=0,card_vectors=0;
            unsigned transactions=0,ce=0,keys=0,keyboard_vectors=0,audio_samples=0;
            unsigned refreshes=0,dma2=0,dma3=0,locked_memory=0,terminal_bytes=0;
            bool previous_owner=false,previous_commit=false;
            int audio_min=32767,audio_max=-32768;bool pw=false;
            d.reset=1;d.code_valid=0;d.code=0;d.switches=switches;d.memory_done=0;d.memory_ready=0;
            for(unsigned i=0;i<32;i++){d.clk=0;d.eval();d.clk=1;d.eval();}d.reset=0;
            while(ticks++<6000000 && !passed) {
                d.memory_done=0;d.memory_ready=!pending && (!waits || ticks%4!=0);
                if(pending && delay--==0) {
                    if(pw)mem[pa]=pd;
                    d.memory_rdata=mem[pa];d.memory_done=1;pending=false;
                    if(pw && pa==0x600) {
                        if(pd==0xee)throw std::runtime_error("ROM failure phase "+std::to_string(mem[0x601]));
                        check(pd==0xaa,"invalid success marker");passed=true;
                    }
                }
                d.code_valid=mem[0x601]==4 && keys<2;
                d.code=keys==0 ? 0x1e : 0x9e;
                d.clk=0;d.eval();
                if(d.code_valid && d.code_ready)++keys;
                if(d.audio_valid && mem[0x601]==4) {
                    ++audio_samples;int value=int16_t(d.audio_sample);
                    audio_min=std::min(audio_min,value);audio_max=std::max(audio_max,value);
                }
                if(d.guest_ce)++ce;
                if(d.refresh_done)++refreshes;
                if(d.dma_owner && !previous_owner)check(!d.bus_locked && !(acks&1),"DMA grant split LOCK/INTA");
                previous_owner=d.dma_owner;
                bool commit=(d.observed_request>>33)&1;
                if(commit && !previous_commit && ((d.observed_request>>28)&3)==3) {
                    unsigned channel=(d.observed_request>>37)&3;
                    check((d.observed_request>>35)&1,"DMA AEN absent");
                    if(channel==2) {check(!((d.observed_request>>30)&1),"DMA2 direction");++dma2;}
                    else if(channel==3) {
                        check((d.observed_request>>30)&1,"DMA3 direction");
                        check(((d.observed_request>>20)&255)==0x31+dma3,"DMA3 byte ordering");++dma3;
                    } else throw std::runtime_error("unexpected DMA channel");
                    if((d.observed_request>>39)&1)++terminal_bytes;
                }
                previous_commit=commit;
                if(d.halted)saw_halt=true;
                check(!d.fault,"CPU fabric fault");
                if(d.accepted) {
                    ++transactions;
                    if(d.cycle_kind==0 && d.cycle_lock)++locked_memory;
                    if(d.cycle_kind==2){check(d.cycle_lock,"INTA not locked");ack_pending=true;}
                }
                if(d.completed && ack_pending) {
                    if((acks++&1)==0) check(d.cycle_rdata==0xff,"first INTA drove data");
                    else if(d.cycle_rdata==8)++timer_vectors;
                    else if(d.cycle_rdata==9)++keyboard_vectors;
                    else if(d.cycle_rdata==13)++card_vectors;
                    else throw std::runtime_error("unexpected PIC vector");
                    ack_pending=false;
                }
                if(d.memory_valid && d.memory_ready) {
                    check(!pending,"duplicate storage request");
                    pa=d.address;pd=d.wdata;pw=d.write_cycle;pending=true;
                    check(pa<0xa0000 || (pa>=0xf0000&&!pw),"storage decode");
                    delay=waits ? (transactions*17)%waits : 0;
                }
                d.clk=1;d.eval();
            }
            check(passed,"machine diagnostic timeout");check(saw_halt,"HLT not exercised");
            check(timer_vectors>=3 && card_vectors==1 && keyboard_vectors==2 && !(acks&1),"IRQ coverage");
            check(mem[0x610]>=3 && mem[0x612]==1,"interrupt handlers did not execute");
            check(keys==2 && mem[0x614]==2 && mem[0x616]==0x1e && mem[0x617]==0x9e,"keyboard make/break handlers");
            check(mem[0x619]==(switches>>4) && mem[0x618]==(switches&0x0f),"equipment switch profile");
            check(audio_samples>50 && audio_max-audio_min>7000,"CPU programmed speaker tone");
            check(!d.speaker,"ROM did not mute speaker");
            check(dma2==8 && dma3==4 && terminal_bytes==2,"DMA byte/TC coverage");
            check(refreshes>1000 && locked_memory>=160 && mem[0x5100]==40,"refresh/LOCK coverage");
            for(unsigned a=0x3000;a<0x3008;a++)check(mem[a]==0x6d,"DMA RAM result");
            check(ce>ticks/22 && ce<ticks/20,"CPU clock cadence");
            std::cout<<"PASS machine waits="<<waits<<" switches="<<std::hex<<switches<<std::dec
                     <<": "<<transactions<<" transactions, "
                     <<timer_vectors<<" timer vectors, "<<card_vectors<<" ISA vector, "<<keyboard_vectors<<" keyboard vectors, "<<refreshes<<" refresh cycles, "<<ticks<<" fabric clocks\n";
        }
    }catch(const std::exception& e){std::cerr<<"FAIL machine: "<<e.what()<<"\n";return 1;}
}
