// SPDX-License-Identifier: MIT
#include "Vmedium_top.h"
#include "verilated.h"
#include "rotating_disk.h"
#include <cstdio>
#include <cstdlib>
#include <memory>
#include <fstream>
#include <vector>
#include <stdexcept>
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv);
 if(argc<4)return 2;
 Vmedium_top t;RotatingDisk expected(argv[1]);
 uint64_t ticks=0,compared=0,fluxes=0;unsigned worst_build=0,reference_phase=0;
 t.clk=0;t.reset=1;t.ref_ce=0;t.disk_ready=0;t.drive_pins=4;t.drive_ddr=7;
 t.write_addr=0;t.write_data=0;t.write_enable=0;
 auto tick=[&](bool check=true){
  reference_phase+=16000000;t.ref_ce=reference_phase>=52224000;
  if(t.ref_ce)reference_phase-=52224000;t.clk=0;t.eval();
  if(!t.reset && check){
   if(t.halftrack!=expected.halftrack || t.rotation!=expected.rotation){
    std::fprintf(stderr,"position mismatch at %llu head %u/%u angle %u/%u\n",(unsigned long long)ticks,t.halftrack,expected.halftrack,t.rotation,expected.rotation);std::exit(1);
   }
   bool want=t.cache_ready&&expected.flux(t.ref_ce,t.drive_pins&4);
   if(bool(t.flux)!=want){
    std::fprintf(stderr,"flux mismatch at %llu head%u angle%u cache%u byte%u=%02x got%u want%u\n",(unsigned long long)ticks,t.halftrack,t.rotation,t.cache_ready,t.debug_byte_addr,t.debug_byte,t.flux,want);std::exit(1);
   }
   if(t.cache_ready&&t.ref_ce&&(t.drive_pins&4)){++compared;fluxes+=t.flux;}
   if(!t.disk_ready && (t.cache_ready||t.flux))throw std::runtime_error("removed disk still visible");
  }
  t.clk=1;t.eval();++ticks;
  if(!t.reset){expected.move(t.drive_pins,t.drive_ddr);expected.advance(t.ref_ce,t.drive_pins&4);}
 };
 for(int i=0;i<4;++i)tick(false);t.reset=0;t.disk_ready=1;
 auto build=[&](){unsigned start=ticks;while(!t.cache_ready){tick();if(ticks-start>50000)throw std::runtime_error("track build timeout");}worst_build=std::max(worst_build,unsigned(ticks-start));};
 auto step=[&](int dir){t.drive_pins=(t.drive_pins&~3)|((expected.halftrack+dir)&3);for(int i=0;i<9;++i)tick();};
 auto seek=[&](unsigned track){while(expected.halftrack!=track*2)step(expected.halftrack<track*2?1:-1);build();};
 auto revolution=[&](){auto start=compared;while(compared-start<3200000)tick();};
 build();
 for(unsigned track=1;track<=35;++track){
  seek(track);revolution();
  std::printf("track%02u zone%u complete revolution passed at angle%u\n",track,RotatingDisk::zone(track),t.rotation);
 }
 // Motor stop and input-direction gating do not move the head/spindle.
 t.drive_pins&=~4;auto angle=expected.rotation;for(unsigned i=0;i<10000;++i)tick();
 if(expected.rotation!=angle)throw std::runtime_error("motor stop lost angle");
 t.drive_pins|=4;t.drive_ddr=0;t.drive_pins=(t.drive_pins&~3)|((expected.halftrack-1)&3);
 for(unsigned i=0;i<50;++i)tick();if(expected.halftrack!=70)throw std::runtime_error("input step moved head");
 t.drive_pins=(t.drive_pins&~3)|(expected.halftrack&3);t.drive_ddr=7;
 // Halftrack, rail and zone changes preserve angular phase.
 step(1);if(expected.halftrack!=70)throw std::runtime_error("upper head limit failed");
 seek(1);step(-1);if(expected.halftrack!=2)throw std::runtime_error("lower head limit failed");
 t.drive_pins=(t.drive_pins&~3)|(expected.halftrack&3);
 // Remove and replace all bytes while the motor continues running.
 t.disk_ready=0;for(unsigned i=0;i<100;++i)tick();
 std::ifstream in(argv[3],std::ios::binary);std::vector<unsigned char>replacement((std::istreambuf_iterator<char>(in)),{});
 if(replacement.size()!=174848)return 2;
 for(unsigned i=0;i<replacement.size();i+=2){t.write_addr=i;t.write_data=replacement[i]|(replacement[i+1]<<8);t.write_enable=3;tick();}
 t.write_enable=0;tick();
 RotatingDisk replacement_disk(argv[2]);expected.tracks=std::move(replacement_disk.tracks);
 t.disk_ready=1;build();
 for(auto track:{1u,18u,25u,31u}){seek(track);t.drive_pins^=0x60;revolution();}
 std::printf("PASS: %llu reference ticks, %llu flux pulses, max build%u system clocks (%.3fms at52.224MHz), all35 tracks plus four replacement tracks\n",(unsigned long long)compared,(unsigned long long)fluxes,worst_build,worst_build/52224.0);
}
