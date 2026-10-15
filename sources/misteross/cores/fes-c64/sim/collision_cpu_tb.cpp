// SPDX-License-Identifier: MIT
#include "Vready_sim_top.h"
#include "Vready_sim_top___024root.h"
#include "verilated.h"
#include <iterator>
#include <vector>
#include <array>
#include <cstdio>
#include <fstream>
#include <string>
int main(int argc,char**argv){
 if(argc!=3)return 2;
 std::ifstream input(argv[2],std::ios::binary);
 std::vector<unsigned char> collision_program(std::istreambuf_iterator<char>(input),{});
 if(collision_program.size()!=512)return 2;std::array<unsigned char,36864>fw{};fw[16381]=0x10;
 std::ofstream f(argv[1]);for(unsigned b:fw){char s[4];std::snprintf(s,sizeof(s),"%02x\n",b);f<<s;}f.close();if(!f)return 2;
 std::string opt=std::string("+C64_FIRMWARE_HEX=")+argv[1];char *args[]={argv[0],opt.data()};Verilated::commandArgs(2,args);
 for(unsigned mask:{255u,189u,219u,128u}){
 Vready_sim_top t;t.clk=0;t.reset=1;t.eval();auto*r=t.rootp;auto&ram=r->ready_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
 for(unsigned i=0;i<512;++i)ram[0x1000+i]=collision_program[i];
 for(unsigned i=0;i<1000;++i)ram[0x400+i]=0;
 for(unsigned i=0;i<8;++i){ram[0x7f8+i]=0xc0;ram[0x2000+i]=255;}
 for(unsigned i=0;i<63;++i)ram[0x3000+i]=255;
 for(unsigned i=0;i<16;++i)ram[0x1200+i]=0;
 ram[0x12ff]=mask;ram[0xfffe]=0;ram[0xffff]=0x11;
 bool done=false;
 for(unsigned sys=0;sys<5000000;++sys){t.clk=0;t.eval();if(sys==400)t.reset=0;t.clk=1;t.eval();if(ram[0x1208]==0xa5){done=true;break;}}
 unsigned ss=mask==128?0:mask,status=mask==128?0x72:0x76;
 const unsigned expected[]={status,ss,mask,0xa5,ss,mask,0,0,0xa5,0x70};unsigned errors=0;
 for(unsigned i=0;i<10;++i)if(ram[0x1200+i]!=expected[i]){std::printf("mask%02x result%u got%02x expected%02x\n",mask,i,ram[0x1200+i],expected[i]);++errors;}
 if(!done||errors){std::printf("FAIL collision CPU PC%04x raster%u IRQ%u\n",t.debug_pc,r->ready_sim_top__DOT__machine__DOT__vic__DOT__raster,r->ready_sim_top__DOT__machine__DOT__vic_irq);return 1;}
 std::printf("Actual6510 collision PASS mask%02x:IRQ entry,SS/BG reads,deferred clear,W1C,disable/re-enable\n",mask);std::fflush(stdout);
 }
}
