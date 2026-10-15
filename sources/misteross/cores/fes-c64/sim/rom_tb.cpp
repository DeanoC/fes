// SPDX-License-Identifier: MIT
#include "Vc64_rom.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
unsigned value(unsigned i){return (i*37+(i>>8)*11+(i>>10)*17)&255;}
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv);Vc64_rom t;
 t.clk=0;t.video_clk=0;t.address=0;t.video_address=0;t.drive_address=0;t.eval();
 auto cpu=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};
 auto vid=[&](){t.video_clk=0;t.eval();t.video_clk=1;t.eval();};
 auto die=[&](const char *what,unsigned a,unsigned actual,unsigned expected){
  std::fprintf(stderr,"%s %04x got %02x expected %02x\n",what,a,actual,expected);std::exit(1);};
 cpu();cpu();vid();
 unsigned previous=value(0),previous_v=value(16384);
 // Pipeline changing CPU and drive addresses every clock; include all CPU
 // unmapped addresses and every drive address twice. No settling loops mask latency.
 for(unsigned i=0;i<32768;++i){
  const unsigned ca=(i*109+0x4011)&32767,da=(i*97+0x3fff)&16383;
  t.address=ca;t.drive_address=da;t.video_address=(i*29+7)&4095;
  auto cpu_held=t.data,video_held=t.video_data,drive_held=t.drive_data;
  t.eval();
  if(t.data!=cpu_held||t.video_data!=video_held||t.drive_data!=drive_held)die("unclocked",i,0,1);
  cpu();
  if(t.data!=previous)die("CPU two-clock latency",ca,t.data,previous);
  if(t.drive_data!=value(20480+da))die("drive one-clock latency",da,t.drive_data,value(20480+da));
  if(t.video_data!=previous_v)die("held video",i,t.video_data,previous_v);
  previous=ca<20480?value(ca):255;
  if(i%3==0){
   cpu_held=t.data;drive_held=t.drive_data;vid();
   previous_v=value(16384+t.video_address);
   if(t.video_data!=previous_v)die("video one-clock latency",i,t.video_data,previous_v);
   if(t.data!=cpu_held||t.drive_data!=drive_held)die("video altered CPU/drive",i,0,1);
  }
 }
 // Independent video sweeps every character location with system clock held.
 auto cpu_held=t.data,drive_held=t.drive_data;
 for(unsigned i=0;i<4096;++i){t.video_address=i;vid();
  if(t.video_data!=value(16384+i))die("VIC exhaustive",i,t.video_data,value(16384+i));
  if(t.data!=cpu_held||t.drive_data!=drive_held)die("held system",i,0,1);
 }
 std::puts("PASS: 32768 pipelined CPU accesses (including unmapped), 32768 drive accesses, all4096 VIC bytes; exact2/1/1 latency and independent ports");
}
