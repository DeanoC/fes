// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "Vcpu6502___024root.h"
#include "verilated.h"
#include <array>
#include <vector>
#include <cstdio>
struct Bus { unsigned address,data,write; };
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv);unsigned cases=0;
 for(unsigned op:{0x8fu,0x87u,0x97u,0x83u})for(unsigned a:{0u,0xa5u,0xffu})for(unsigned x:{0u,0x3cu,0xffu})for(unsigned gap:{1u,17u})for(unsigned variant:{0u,1u}){
  Vcpu6502 t;std::array<unsigned char,65536> ram{};std::vector<Bus> bus;
  unsigned at=0x1000;auto byte=[&](unsigned n){ram[at++]=n;};
  byte(0xa2);byte(x);byte(0xa0);byte(3);byte(variant?0x38:0x18);byte(variant?0xf8:0xd8);byte(0x2c);byte(0);byte(0x23);ram[0x2300]=variant?0xc0:0;byte(0xa9);byte(a);byte(0xea);
  unsigned insn=at;byte(op);unsigned target=0x2100;
  if(op==0x8f){byte(0);byte(0x21);}else if(op==0x83){byte(0xff);unsigned ptr=(0xff+x)&255;ram[ptr]=0;ram[(ptr+1)&255]=0x21;}else{byte(0xff);target=op==0x97?2:255;}
  unsigned end=at;byte(0x4c);byte(end&255);byte(end>>8);ram[0xfffc]=0;ram[0xfffd]=0x10;
  t.clk=0;t.reset=1;t.IRQ=0;t.NMI=0;t.SO=1;t.RDY=0;t.DI=0;t.eval();
  bool active=false,done=false;unsigned flags=0,after=0,stack=0;
  auto status=[&](){auto r=t.rootp;return unsigned(r->cpu6502__DOT__N*128+r->cpu6502__DOT__V*64+r->cpu6502__DOT__D*8+r->cpu6502__DOT__I*4+r->cpu6502__DOT__Z*2+r->cpu6502__DOT__C);};
  for(unsigned cycle=0;cycle<6000&&!done;++cycle){
   t.clk=0;t.RDY=gap==1||cycle%gap==0||cycle%gap==8;t.eval();if(cycle==100)t.reset=0;
   if(t.RDY&&!t.reset&&t.rootp->cpu6502__DOT__state==12){
    if(t.rootp->cpu6502__DOT__PC==insn+1){active=true;flags=status();stack=t.rootp->cpu6502__DOT__AXYS[1];}
    else if(active&&t.rootp->cpu6502__DOT__PC==end+1){done=true;}
   }
   Bus b{unsigned(t.AB),unsigned(t.DO),unsigned(t.WE)};
   if(active&&!done&&t.RDY)bus.push_back(b);
   t.clk=1;t.eval();if(t.RDY){if(b.write)ram[b.address]=b.data;t.DI=ram[b.address];t.eval();}
   after=status();
  }
  unsigned writes=0;for(auto b:bus)if(b.write){++writes;if(b.address!=target||b.data!=(a&x))writes=999;}
  unsigned count=op==0x83?6:op==0x8f||op==0x97?4:3;
  auto r=t.rootp;
  if(!done||writes!=1||bus.size()!=count||ram[target]!=(a&x)||flags!=after||r->cpu6502__DOT__AXYS[1]!=stack||r->cpu6502__DOT__AXYS[0]!=a||r->cpu6502__DOT__AXYS[2]!=x||r->cpu6502__DOT__AXYS[3]!=3){
   std::printf("SAX %02x A=%02x X=%02x gap=%u done=%u writes=%u cycles=%zu/%u flags=%02x/%02x target=%04x data=%02x/%02x\n",op,a,x,gap,done,writes,bus.size(),count,flags,after,target,ram[target],a&x);
   unsigned shown=0;for(auto b:bus)if(shown++<12)std::printf(" %04x %02x W%u\n",b.address,b.data,b.write);return 1;
  }
  ++cases;
 }
 std::printf("SAX PASS: %u cases, all four modes, zero-page/pointer wrapping, exact enabled cycle count, one A&X write, registers/flags unchanged, 8/9-clock enable gaps\n",cases);
}
