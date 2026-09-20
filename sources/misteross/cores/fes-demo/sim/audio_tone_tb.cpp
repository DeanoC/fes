// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_demo_audio.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
#include <vector>
static void check(bool ok,const char* m){if(!ok){std::cerr<<m<<'\n';std::exit(1);}}
static std::vector<int16_t> capture(Vfes_demo_audio& d,unsigned frames,bool right){
    bool old_b=d.sclk,old_lr=d.lrclk; unsigned bit=0; uint16_t sample=0;
    std::vector<int16_t> result;
    while(result.size()<frames){
        d.clk=0;d.eval();d.clk=1;d.eval();
        if(!old_b && d.sclk){
            if(old_lr!=d.lrclk){bit=0;sample=0;old_lr=d.lrclk;}
            else ++bit;
            if(bool(d.lrclk)==right && bit>=1 && bit<=16){sample=uint16_t((sample<<1)|d.sdata);if(bit==16)result.push_back(int16_t(sample));}
        }
        old_b=d.sclk;
    }
    return result;
}
static void tone(Vfes_demo_audio& d,bool right,unsigned half_period){
    auto samples=capture(d,400,right);unsigned previous=0,changes=0;
    for(unsigned i=4;i<samples.size();++i){
        check(samples[i]==4096 || samples[i]==-4096,"tone amplitude must be signed4096");
        if(samples[i]!=samples[i-1]){if(changes++)check(i-previous==half_period,"tone frequency / channel mismatch");previous=i;}
    }
    check(changes>5,"insufficient tone cycles");
}
int main(){
    Vfes_demo_audio d;d.clk=0;d.locked=0;d.exec_reset=1;d.buttons=0;d.eval();
    d.locked=1;d.eval();
    for(auto s:capture(d,5,false))check(s==0,"held startup must be silent");
    d.exec_reset=0;tone(d,false,24);tone(d,true,48);
    d.buttons=8;tone(d,false,12);tone(d,true,24);
    d.exec_reset=1;auto held=capture(d,5,false);for(unsigned i=2;i<held.size();++i)check(held[i]==0,"held output must become silent");
    d.exec_reset=0;d.buttons=0;tone(d,false,24);
    // Stop MCLK with data high, then lose lock: data must fall without edges.
    while(!d.sdata){d.clk=0;d.eval();d.clk=1;d.eval();}
    d.locked=0;d.eval();check(!d.sdata,"PLL unlock must inhibit serial data without clock edges");
    d.locked=1;tone(d,false,24);
    std::cout<<"Tone1000/500Hz, Right2000/1000Hz, CDC hold/release and clock-loss silence passed\n";
}
