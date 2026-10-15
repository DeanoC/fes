// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "Vcpu6502___024root.h"
#include "verilated.h"
#include <array>
#include <vector>
#include <cstdio>
#include <cstdlib>
struct Bus{unsigned address,data,write;};
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);unsigned cases=0;uint64_t sys=0;
 auto check=[&](unsigned a,unsigned x,unsigned immediate,unsigned flags,unsigned gap)->bool{
 Vcpu6502 t;std::array<unsigned char,65536> ram{};std::vector<Bus> bus;unsigned at=0x1000;
 auto byte=[&](unsigned v){ram[at++]=v;};
 byte(0xa2);byte(x);byte(0xa0);byte(3);byte(flags&1?0x38:0x18);byte(flags&2?0xf8:0xd8);byte(flags&4?0x78:0x58);
 byte(0x2c);byte(0);byte(0x23);ram[0x2300]=flags&8?0xc0:0;byte(0xa9);byte(a);byte(0xea);
 unsigned insn=at;byte(0xcb);byte(immediate);unsigned end=at;byte(0x4c);byte(end&255);byte(end>>8);ram[0xfffc]=0;ram[0xfffd]=0x10;
 t.clk=0;t.reset=1;t.IRQ=0;t.NMI=0;t.SO=1;t.RDY=0;t.DI=0;t.eval();bool active=false,done=false;unsigned before=0,after=0,stack=0;
 auto status=[&](){auto r=t.rootp;return unsigned(r->cpu6502__DOT__N*128+r->cpu6502__DOT__V*64+r->cpu6502__DOT__D*8+r->cpu6502__DOT__I*4+r->cpu6502__DOT__Z*2+r->cpu6502__DOT__C);};
 for(unsigned cycle=0;cycle<6000&&!done;++cycle){
 t.clk=0;t.RDY=gap==1||cycle%gap==0||cycle%gap==8;t.eval();if(cycle==100)t.reset=0;
 if(t.RDY&&!t.reset&&t.rootp->cpu6502__DOT__state==12){if(t.rootp->cpu6502__DOT__PC==insn+1){active=true;before=status();stack=t.rootp->cpu6502__DOT__AXYS[1];}else if(active&&t.rootp->cpu6502__DOT__PC==end+1)done=true;}
 Bus b{unsigned(t.AB),unsigned(t.DO),unsigned(t.WE)};if(active&&!done&&t.RDY)bus.push_back(b);
 t.clk=1;t.eval();++sys;if(t.RDY){if(b.write)ram[b.address]=b.data;t.DI=ram[b.address];t.eval();}after=status();}
 unsigned difference=((a&x)-immediate)&255;unsigned expected=(before&0x4c)|(difference&128)|(difference==0?2:0)|((a&x)>=immediate?1:0);auto r=t.rootp;
 bool ok=done&&bus.size()==2&&bus[0].address==insn+1&&bus[1].address==end&&!bus[0].write&&!bus[1].write&&after==expected&&r->cpu6502__DOT__AXYS[0]==a&&r->cpu6502__DOT__AXYS[2]==difference&&r->cpu6502__DOT__AXYS[3]==3&&r->cpu6502__DOT__AXYS[1]==stack;
 if(!ok){printf("AXS FAIL A%02x X%02x imm%02x flags%u gap%u done%u bus%zu P%02x/%02x X%02x/%02x\n",a,x,immediate,flags,gap,done,bus.size(),after,expected,unsigned(r->cpu6502__DOT__AXYS[2]),difference);for(unsigned i=0;i<bus.size()&&i<10;++i)printf(" %04x %02x W%u\n",bus[i].address,bus[i].data,bus[i].write);}
 ++cases;return ok;};
 for(unsigned a:{0u,1u,0x17u,0x7fu,0x80u,0xa5u,255u})for(unsigned x:{0u,1u,0x55u,0x80u,255u})for(unsigned immediate:{0u,1u,0x7fu,0x80u,255u})for(unsigned flags=0;flags<16;++flags)for(unsigned gap:{1u,17u})if(!check(a,x,immediate,flags,gap))return 1;
 // Exhaust every subtract input pair; vary all flags and independent A/X bits.
 for(unsigned ax=0;ax<256;++ax)for(unsigned immediate=0;immediate<256;++immediate){unsigned a=ax|((~ax)&0x55),x=ax|((~ax)&0xaa);if(!check(a,x,immediate,(ax^immediate)&15,1))return 1;}
 printf("AXS PASS %u cases %llu SYS: all65536 subtract pairs, independentAND inputs, C/D/I/V combinations, exact2enabled readcycles, A/Y/S preserved, RDYgaps1/17\n",cases,(unsigned long long)sys);}
