// SPDX-License-Identifier: MIT
#include "Vcpu6502.h"
#include "Vcpu6502___024root.h"
#include "verilated.h"
#include <array>
#include <cstdio>
#include <cstdlib>
int main(int argc,char **argv){
    Verilated::commandArgs(argc,argv);Vcpu6502 t;
    std::array<unsigned char,65536> mem{};
    // CLV; BVC *; PHP; PLA; STA $0200; CLV; PHP; PLA; STA $0201; JMP *.
    unsigned char code[]={0xb8,0x50,0xfe,0x08,0x68,0x8d,0x00,0x02,0xb8,0x08,0x68,0x8d,0x01,0x02,0x4c,0x0e,0x80};
    for(unsigned i=0;i<sizeof(code);++i)mem[0x8000+i]=code[i];
    mem[0xfffc]=0;mem[0xfffd]=0x80;
    t.clk=0;t.reset=1;t.IRQ=0;t.NMI=0;t.RDY=0;t.SO=1;t.DI=0;
    bool injected=false, priority_checked=false;
    for(unsigned cycle=0;cycle<5000;++cycle){
        t.RDY=cycle%8==0;t.clk=0;t.eval();
        if(!priority_checked && t.RDY && t.rootp->cpu6502__DOT__state==12 && t.rootp->cpu6502__DOT__clv){
            t.SO=0;t.eval();priority_checked=true;
        }
        if(priority_checked && !injected && !t.RDY){t.SO=1;t.eval();}
        if(cycle==1202 && (mem[0x200] || mem[0x201] || t.rootp->cpu6502__DOT__V))return 1;
        if(cycle==1203){t.SO=0;injected=true;t.eval();}
        // Keep SO low through CLV: a held level must not reassert V.
        auto address=t.AB;auto data=t.DO;auto write=t.WE;
        if(cycle==100)t.reset=0;
        t.clk=1;t.eval();
        if(t.RDY){if(write)mem[address]=data;t.DI=mem[address];t.eval();}
    }
    std::printf("PC=%04x V=%u\n",t.rootp->cpu6502__DOT__PC,t.rootp->cpu6502__DOT__V);
    std::printf("SO poll status=%02x CLV status=%02x\n",mem[0x200],mem[0x201]);
    if(!priority_checked)return 1;
    // Raising SO after CLV must not set V; another falling edge must.
    t.RDY=0;t.SO=1;t.clk=0;t.eval();t.clk=1;t.eval();
    if(t.rootp->cpu6502__DOT__V)return 1;
    t.SO=0;t.clk=0;t.eval();t.clk=1;t.eval();
    bool disabled=*Verilated::commandArgsPlusMatch("SO_DISABLED=");
    if(disabled)return mem[0x200]==0&&mem[0x201]==0&&t.rootp->cpu6502__DOT__V==0 ? 0 : 1;
    return t.rootp->cpu6502__DOT__V&&injected&&(mem[0x200]&0x40)&&!(mem[0x201]&0x40)&&mem[0x201]!=0 ? 0 : 1;
}
