// SPDX-License-Identifier: MIT
#include "Vc64_gcr_read.h"
#include "verilated.h"
#include <fstream>
#include <vector>
#include <cstdio>
#include <cstdlib>
static std::vector<unsigned> load(const char *path) {
    std::ifstream in(path);std::vector<unsigned> v;unsigned b;
    while(in>>std::hex>>b)v.push_back(b);return v;
}
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    if(argc!=5)return 2;
    auto raw=load(argv[1]);auto expected=load(argv[2]);unsigned zone=std::atoi(argv[3]);
    unsigned sectors=std::atoi(argv[4]),blocks=0,idx=0,start=0,length=0;
    Vc64_gcr_read t;t.clk=0;t.reset=1;t.ref_ce=1;t.motor=1;t.reading=1;t.ready_enable=1;t.density=zone;t.flux=0;
    auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};tick();t.reset=0;
    bool previous_sync=false,previous_ready=true,saw_sync=false,active=false;
    unsigned samples=0;
    const unsigned period=4*(16-zone);
    for(unsigned bit=0;bit<raw.size()*8;++bit){
        bool one=raw[bit/8]&(0x80>>(bit%8));
        for(unsigned r=0;r<period;++r){
            t.flux=one&&r==0;tick();samples+=t.bit_sample;
            if(t.sync_found){saw_sync=true;active=false;}
            if(!t.sync_found&&previous_sync&&saw_sync){
                if(blocks==sectors*2)break;
                length=(blocks%2==0)?10:325;idx=0;active=true;
            }
            if(previous_ready&&!t.byte_ready_n&&active&&idx<length){
                if(start+idx>=expected.size()||t.data!=expected[start+idx]){
                    std::printf("read zone=%u block=%u byte=%u got=%02x expected=%02x\n",zone,blocks,idx,t.data,expected[start+idx]);return 1;
                }
                if(++idx==length){start+=length;++blocks;active=false;}
            }
            previous_ready=t.byte_ready_n;previous_sync=t.sync_found;
        }
    }
    if(blocks!=sectors*2){std::printf("missing blocks=%u expected=%u\n",blocks,sectors*2);return 1;}
    t.ready_enable=0;t.eval();if(!t.byte_ready_n)return 1;
    t.motor=0;tick();if(t.sync_found||!t.byte_ready_n)return 1;
    std::printf("GCR read zone=%u: %u header/data blocks, %u recovered bits passed\n",zone,blocks,samples);
}
