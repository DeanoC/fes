// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_sn76489.h"
#include <cstdlib>
#include <iostream>
static void check(bool b, const char *s) { if(!b){std::cerr<<s<<'\n';std::exit(1);} }
int main() {
    Vfes_sn76489 d;
    auto tick=[&](){d.clk=0;d.eval();d.clk=1;d.eval();};
    auto write=[&](unsigned v){d.write=1;d.data=v;tick();d.write=0;tick();};
    d.ce=1;d.reset=1;d.write=0;tick();d.reset=0;
    for(int i=0;i<100;++i){tick();check(d.sample==0,"reset must mute all four channels");}
    // N=50: exact half period is 800 chip-clock enable pulses.
    write(0x82);write(0x03);write(0x90);
    for(int i=0;i<2000;++i)tick();
    int last=-1, edges=0; bool sign=int16_t(d.sample)>0;
    for(int i=0;i<8000;++i){tick();bool next=int16_t(d.sample)>0;
        check(std::abs(int(int16_t(d.sample)))==8191,"full scale attenuation");
        if(next!=sign){if(last>=0)check(i-last==800,"tone divider/latch-data protocol");last=i;++edges;}sign=next;}
    check(edges>=9,"tone must oscillate");
    write(0x91);for(int i=0;i<3;++i)tick();
    check(std::abs(int(int16_t(d.sample)))==6507,"2 dB attenuation");
    write(0x0f);for(int i=0;i<3;++i)tick();check(d.sample==0,"data byte updates latched volume");
    // Periodic TI noise contains one high bit every fifteen shifts.
    write(0xe0);write(0xf0);
    int rise=-1,periods=0;sign=int16_t(d.sample)>0;
    for(int i=0;i<512*60;++i){tick();bool next=int16_t(d.sample)>0;
        if(next&&!sign){if(rise>=0){check(i-rise==512*15,"TI 15-bit periodic noise and rate");++periods;}rise=i;}sign=next;}
    check(periods>=2,"periodic noise progression");
    write(0xc2);write(0x03); // tone 2 N=50, still muted
    for(unsigned rate=1;rate<4;++rate){
        write(0xe0|rate);
        const int shift_period=rate==3?1600:(512<<rate);
        rise=-1;periods=0;sign=int16_t(d.sample)>0;
        for(int i=0;i<shift_period*46;++i){tick();bool next=int16_t(d.sample)>0;
            if(next&&!sign){if(rise>=0){check(i-rise==shift_period*15,"fixed and tone-2 noise clock rates");++periods;}rise=i;}sign=next;}
        check(periods>=1,"noise rate must progress");
    }
    write(0xe4); int positives=0;
    for(int i=0;i<512*100;++i){tick();if(int16_t(d.sample)>0)++positives;}
    check(positives>512*8,"white-noise feedback");
    // Every volume register can silence and enable its channel; summed PCM
    // must retain full precision rather than wrapping above four channels.
    write(0x90);write(0xb0);write(0xd0);write(0xf0);
    for(int i=0;i<10000;++i){tick();check(std::abs(int(int16_t(d.sample)))<=32764,"four-channel signed mix range");}
    write(0x9f);write(0xbf);write(0xdf);write(0xff);
    for(int i=0;i<3;++i)tick();check(d.sample==0,"all four attenuators mute");
    d.reset=1;tick();check(d.sample==0,"hold/reset clears audible state");
    std::cout<<"SN76489 tone periods, latch/data, attenuation, TI noise and reset passed\n";
}
