// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_audio_output.h"
#include <cstdlib>
#include <iostream>
static void check(bool b,const char*s){if(!b){std::cerr<<s<<'\n';std::exit(1);}}
int main(){
    Vfes_audio_output d;d.source_clk=0;d.audio_clk=0;d.locked=0;d.hold=0;d.eval();
    d.locked=1;
    bool old_b=0,old_lr=0,framed=0;unsigned bit=0,frames=0,valid=0;
    uint16_t l=0,r=0;
    // Independent clocks and complementary stereo expose torn snapshots.
    for(unsigned t=0;t<300000;++t){
        if(t%3==0){d.source_clk=!d.source_clk;if(d.source_clk){d.left_sample+=7919;d.right_sample=uint16_t(~d.left_sample);}}
        if(t%13==0)d.audio_clk=!d.audio_clk;
        d.hold=t>=200000;d.eval();
        if(!old_b&&d.sclk){
            if(d.lrclk!=old_lr){
                if(!d.lrclk){
                    if(framed){++frames;if(l||r){check(uint16_t(l^r)==0xffff,"torn stereo CDC sample");++valid;}
                        if(t>215000)check(l==0&&r==0,"hold must serialize silence");}
                    framed=1;l=r=0;
                }
                old_lr=d.lrclk;bit=0;
            }else ++bit;
            if(framed){if(bit>=1&&bit<=16){auto &v=d.lrclk?r:l;v=uint16_t((v<<1)|d.sdata);}
                else check(!d.sdata,"I2S delay/padding");}
        }
        old_b=d.sclk;
    }
    check(frames>30&&valid>20,"enough asynchronously sampled audio frames");
    d.locked=0;d.eval();check(!d.sdata,"unlock must gate serial output without a clock");
    std::cout<<"PCM CDC atomic stereo, independent clocks, I2S padding, hold and clock loss passed\n";
}
