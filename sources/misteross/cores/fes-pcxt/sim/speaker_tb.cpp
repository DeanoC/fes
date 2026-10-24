// SPDX-License-Identifier: GPL-2.0-or-later

#include "Vpcxt_speaker.h"
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#ifndef RATE
#error RATE
#endif
int main(){Vpcxt_speaker d;int phase=0,area=0;int32_t dc=0;int samples=0,last=0;int min_gap=9999,max_gap=0;
for(int n=0;n<RATE/5;n++){
 bool reset=n<4||n==RATE/10;bool bit=((int64_t(n)*100000000/RATE)/12345)%2;
 d.clk=0;d.reset=reset;d.speaker=bit;d.eval();d.clk=1;d.eval();
 int next=phase+3;int a=area+(bit?(RATE==50000000?8:4):0);bool valid=!reset&&next>=RATE/16000;int expect=0;
 if(reset){phase=0;area=0;dc=0;last=0;}
 else if(valid){int32_t error=(a<<8)-dc;expect=error>>8;dc+=error>>8;phase=next-RATE/16000;area=0;if(last){int gap=n-last;if(gap<min_gap)min_gap=gap;if(gap>max_gap)max_gap=gap;}last=n;samples++;}
 else{phase=next;area=a;}
 if(bool(d.sample_valid)!=valid||(valid&&int16_t(d.sample)!=int16_t(expect))){std::fprintf(stderr,"Mismatch n=%d valid=%d sample=%d expect=%d\n",n,int(d.sample_valid),int16_t(d.sample),expect);return 1;}
}
if(samples<9598||samples>9600||min_gap!=RATE/48000||max_gap!=(RATE+47999)/48000)return 2;
std::printf("PASS rate=%d samples=%d interval=%d..%d\n",RATE,samples,min_gap,max_gap);}
