// SPDX-License-Identifier: MIT
#include "Vmailbox_medium_top.h"
#include "verilated.h"
#include "rotating_disk.h"
#include <algorithm>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <stdexcept>
#include <vector>

static void check(bool ok,const char* message){if(!ok)throw std::runtime_error(message);}
static std::vector<uint8_t> load(const char* path){
 std::ifstream in(path,std::ios::binary);
 std::vector<uint8_t> result((std::istreambuf_iterator<char>(in)),{});
 check(result.size()==174848,"expected exact D64 fixture");return result;
}
static uint32_t crc32(const std::vector<uint8_t>& data){
 uint32_t crc=~0u;
 for(auto byte:data){crc^=byte;for(unsigned bit=0;bit<8;++bit)crc=(crc>>1)^((crc&1)?0xedb88320u:0);}
 return ~crc;
}
int main(int argc,char** argv){try{
 Verilated::commandArgs(argc,argv);if(argc<5)return 2;
 auto first=load(argv[1]),second=load(argv[3]);
 RotatingDisk expected(argv[2]),replacement(argv[4]);
 Vmailbox_medium_top top;
 top.clk=0;top.reset=1;top.ref_ce=0;top.gpo=0;top.drive_pins=4;top.drive_ddr=7;
 uint64_t cycles=0,compared=0,fluxes=0,hidden=0,requests=0;
 unsigned phase=0,worst_build=0;bool toggle=false,released=false;
 auto tick=[&](){
  phase+=16000000;top.ref_ce=phase>=52224000;
  if(top.ref_ce)phase-=52224000;top.clk=0;top.eval();
  if(!top.reset){
   check(top.halftrack==expected.halftrack,"head changed across mailbox lifecycle");
   check(top.rotation==expected.rotation,"spindle angle changed across mailbox lifecycle");
   bool want=top.cache_ready&&expected.flux(top.ref_ce,top.drive_pins&4);
   if(bool(top.flux)!=want){
    std::fprintf(stderr,"flux mismatch cycle%llu track%u angle%u byte%u=%02x state%u\n",
     (unsigned long long)cycles,top.halftrack/2,top.rotation,top.debug_byte_addr,top.debug_byte,top.unit0_state);
    throw std::runtime_error("mailbox medium flux differs from independent G64");
   }
   if(top.unit0_state!=3){check(!top.cache_ready&&!top.flux,"uncommitted media is visible");++hidden;}
   if(top.cache_ready&&top.ref_ce&&(top.drive_pins&4)){++compared;fluxes+=top.flux;}
   if(released)check(!top.exec_reset,"media operation asserted execution reset");
  }
  top.clk=1;top.eval();++cycles;
  if(!top.reset){expected.move(top.drive_pins,top.drive_ddr);expected.advance(top.ref_ce,top.drive_pins&4);}
 };
 for(unsigned i=0;i<4;++i)tick();top.reset=0;
 auto gp=[&](unsigned op,unsigned index,unsigned value,bool error=false){
  toggle=!toggle;top.gpo=(toggle?0x80000000u:0)|(op<<24)|(index<<16)|value;
  unsigned wait=0;while(bool(top.gpi&0x00800000u)!=toggle){check(++wait<20,"mailbox acknowledgement timeout");tick();}
  check(bool(top.gpi&0x00400000u)==error,"unexpected mailbox error flag");++requests;return top.gpi&65535u;
 };
 auto build=[&](){
  unsigned start=cycles;while(!top.cache_ready){tick();check(cycles-start<50000,"track rebuild timeout");}
  worst_build=std::max(worst_build,unsigned(cycles-start));
 };
 auto revolution=[&](){auto start=compared;while(compared-start<3200000)tick();};
 auto step=[&](int dir,unsigned dwell=9){
  top.drive_pins=(top.drive_pins&~3)|((expected.halftrack+dir)&3);
  for(unsigned i=0;i<dwell;++i)tick();
 };
 auto begin=[&](const std::vector<uint8_t>& data,bool bad_crc=false){
  uint32_t crc=crc32(data)^(bad_crc?1:0);
  gp(6,0,data.size()&65535);check(top.unit0_state==2,"begin did not hide old medium");
  gp(6,1,data.size()>>16);gp(6,2,crc&65535);gp(6,3,crc>>16);
 };
 auto upload=[&](const std::vector<uint8_t>& data){
  // Exercise an odd chunk boundary, both store byte lanes and maximum chunks.
  const unsigned chunks[]={1,511,512,127,255};unsigned offset=0,chunk=0;
  while(offset<data.size()){
   unsigned count=std::min(chunks[chunk++%5],unsigned(data.size())-offset);
   gp(7,0,offset&65535);gp(7,1,offset>>16);gp(7,2,count);
   for(unsigned i=0;i<count;i+=2){
    unsigned word=data[offset+i]|(i+1<count?unsigned(data[offset+i+1])<<8:0);
    gp(8,i/2,word);
   }
   offset+=count;
  }
  check(top.unit0_state==2&&!top.cache_ready,"upload became visible before CRC commit");
 };
 gp(2,0,1);released=true;
 begin(first);upload(first);gp(9,0,0);
 check(top.unit0_state==3&&top.unit0_size==first.size(),"valid CRC commit failed");
 build();revolution();std::puts("initial CRC commit: complete track18 flux passed");

 begin(second,true);upload(second);
 check(gp(9,0,0,true)==3,"bad CRC did not return InvalidArgument");
 for(unsigned i=0;i<40000;++i)tick();
 check(top.unit0_state==2&&!top.cache_ready,"failed CRC exposed partial replacement");
 gp(10,0,0);check(top.unit0_state==1,"eject did not empty unit");
 for(unsigned i=0;i<1000;++i)tick();
 std::puts("bad CRC, hidden upload and eject passed with continuous spindle angle");

 begin(second);upload(second);expected.tracks=replacement.tracks;gp(9,0,0);
 // Interrupt rebuild repeatedly by moving from track18 to track25. Each
 // partially encoded track must remain hidden and be discarded at a new track.
 for(unsigned i=0;i<100;++i)tick();check(!top.cache_ready,"rebuild unexpectedly complete");
 while(expected.halftrack<50){step(1,200);check(!top.cache_ready,"head move exposed incomplete cache");}
 build();check(expected.halftrack==50,"seek failed");revolution();
 std::puts("replacement and head movement during rebuild: complete track25 flux passed");

 // Eject a ready image; begin another image while the motor is stopped, then
 // eject again in its rebuild before restoring the first disk without a reset.
 gp(10,0,0);top.drive_pins&=~4;auto stopped_angle=top.rotation;
 begin(first);upload(first);check(top.rotation==stopped_angle,"upload moved stopped spindle");
 expected.tracks=RotatingDisk(argv[2]).tracks;gp(9,0,0);
 for(unsigned i=0;i<100;++i)tick();check(!top.cache_ready,"early rebuild unexpectedly complete");
 gp(10,0,0);for(unsigned i=0;i<1000;++i)tick();
 begin(first);upload(first);gp(9,0,0);top.drive_pins|=4;build();revolution();
 std::puts("eject during rebuild and reinsertion: complete restored track25 flux passed");
 std::printf("PASS: %llu mailbox requests, %llu checked reference ticks, %llu flux pulses, %llu hidden system clocks, maximum rebuild %u clocks\n",
  (unsigned long long)requests,(unsigned long long)compared,(unsigned long long)fluxes,
  (unsigned long long)hidden,worst_build);
 return 0;
}catch(const std::exception& error){std::fprintf(stderr,"FAIL: %s\n",error.what());return 1;}}
