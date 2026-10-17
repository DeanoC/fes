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
        for(unsigned waits: {0u,31u,173u}) {
            Vmachine_top d;std::array<uint8_t,1<<20> mem{};
            std::copy(rom.begin(),rom.end(),mem.begin()+0xf0000);
            bool pending=false,passed=false,saw_halt=false,ack_pending=false;
            unsigned pa=0,pd=0,delay=0,ticks=0,acks=0,timer_vectors=0,card_vectors=0;
            unsigned transactions=0,ce=0;bool pw=false;
            d.reset=1;d.memory_done=0;d.memory_ready=0;
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
                d.clk=0;d.eval();
                if(d.guest_ce)++ce;
                if(d.halted)saw_halt=true;
                check(!d.fault,"CPU fabric fault");
                if(d.accepted) {
                    ++transactions;
                    if(d.cycle_kind==2){check(d.cycle_lock,"INTA not locked");ack_pending=true;}
                }
                if(d.completed && ack_pending) {
                    if((acks++&1)==0) check(d.cycle_rdata==0xff,"first INTA drove data");
                    else if(d.cycle_rdata==8)++timer_vectors;
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
            check(timer_vectors>=3 && card_vectors==1 && !(acks&1),"IRQ coverage");
            check(mem[0x610]>=3 && mem[0x612]==1,"interrupt handlers did not execute");
            check(ce>ticks/22 && ce<ticks/20,"CPU clock cadence");
            std::cout<<"PASS machine waits="<<waits<<": "<<transactions<<" transactions, "
                     <<timer_vectors<<" timer vectors, "<<card_vectors<<" ISA vector, "<<ticks<<" fabric clocks\n";
        }
    }catch(const std::exception& e){std::cerr<<"FAIL machine: "<<e.what()<<"\n";return 1;}
}
