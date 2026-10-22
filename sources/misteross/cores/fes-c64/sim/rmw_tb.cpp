// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "verilated.h"
#include <array>
#include <vector>
#include <cstdio>
#include <cstdlib>
struct Write{unsigned address,data;};
int main(int argc,char **argv){
 Verilated::commandArgs(argc,argv);
 const unsigned opcodes[]={0x06,0x16,0x0e,0x1e,0x26,0x36,0x2e,0x3e,0x46,0x56,0x4e,0x5e,0x66,0x76,0x6e,0x7e,0xe6,0xf6,0xee,0xfe,0xc6,0xd6,0xce,0xde};
 for(unsigned op:opcodes){
  Vcpu6502 t;std::array<unsigned char,65536> ram{};std::vector<Write>writes;
  bool indexed=(op&0x10)!=0,absolute=(op&0x08)!=0;unsigned target=absolute?0x2100:(indexed?0x81:0x80);
  unsigned at=0x1000;ram[at++]=0xa2;ram[at++]=1;ram[at++]=0x38;ram[at++]=op;
  ram[at++]=absolute?(indexed?0xff:0x00):0x80;if(absolute)ram[at++]=indexed?0x20:0x21;
  ram[at++]=0x4c;ram[at++]=(at-1)&255;ram[at++]=0x10;
  ram[0xfffc]=0x00;ram[0xfffd]=0x10;ram[target]=0x81;
  t.clk=0;t.reset=1;t.IRQ=0;t.NMI=0;t.SO=1;t.RDY=0;t.DI=0;t.eval();
  for(unsigned cycle=0;cycle<2000;++cycle){
   t.clk=0;t.RDY=(cycle%17)==0||(cycle%17)==8;t.eval();
   unsigned address=t.AB,data=t.DO;bool write=t.WE;
   if(cycle==100)t.reset=0;
   t.clk=1;t.eval();
   if(t.RDY){
    if(write){if(address==target)writes.push_back({address,data});ram[address]=data;}
    t.DI=ram[address];t.eval();
   }
  }
  unsigned kind=op&0xe0,expected=kind==0x00?0x02:kind==0x20?0x03:kind==0x40?0x40:kind==0x60?0xc0:kind==0xe0?0x82:0x80;
  if(writes.size()!=2||writes[0].address!=target||writes[1].address!=target||writes[0].data!=0x81||writes[1].data!=expected){
   std::printf("opcode %02x target=%04x expected=81,%02x writes=%zu\n",op,target,expected,writes.size());for(auto w:writes)std::printf(" %04x=%02x\n",w.address,w.data);return 1;
  }
 }
 std::puts("6502 NMOS RMW: 24 opcode/address modes preserve old/new writes with alternating 8/9-clock CPU enable gaps");
}
