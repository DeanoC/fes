// SPDX-License-Identifier: MIT
#include "Vc64_rom.h"
#include "verilated.h"
#include <cstdio>
unsigned value(unsigned i){return (i*37+(i>>8)*11)&255;}
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv);Vc64_rom t;t.clk=0;t.video_clk=0;t.address=0;t.video_address=0;t.eval();
 auto cpu=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};
 auto vid=[&](){t.video_clk=0;t.eval();t.video_clk=1;t.eval();};
 for(unsigned i=0;i<20480;++i){t.address=i;cpu();cpu();if(t.data!=value(i)){std::printf("CPU ROM address %04x got%02x expected%02x\n",i,t.data,value(i));return 1;}}
 // Holding the video clock never prevents CPU reads of character ROM.
 for(unsigned i=0;i<4096;++i){t.address=0x4000+i;cpu();cpu();if(t.data!=value(0x4000+i))return 1;}
 t.address=0x4011;cpu();cpu();unsigned held=t.data;
 for(unsigned i=0;i<4096;++i){t.video_address=i;vid();if(t.video_data!=value(0x4000+i)||t.data!=held){std::printf("video ROM %03x got%02x expected%02x CPU=%02x\n",i,t.video_data,value(0x4000+i),t.data);return 1;}}
 std::puts("20 KiB ROM: every CPU byte and all 4096 VIC bytes match; independent registered clocks");
}
