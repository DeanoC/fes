// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfirmware_model.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>

static void require(bool ok, const char* message) {
    if (!ok) { std::cerr << message << '\n'; std::exit(1); }
}
struct Bench {
    Vfirmware_model dut;
    bool toggle = false;
    Bench() { dut.clk=0; dut.gpo=0; dut.peek_addr=0; dut.eval(); }
    void tick() { dut.clk=1; dut.eval(); dut.clk=0; dut.eval(); }
    unsigned command(unsigned op, unsigned index, unsigned argument) {
        const uint32_t fields=(op<<24)|(index<<16)|argument;
        dut.gpo=fields|(uint32_t(toggle)<<31); dut.eval(); tick();
        toggle=!toggle;
        dut.gpo=fields|(uint32_t(toggle)<<31); dut.eval();
        for (int i=0;i<12 && bool(dut.gpi&0x800000)!=toggle;++i) tick();
        require(bool(dut.gpi&0x800000)==toggle,"mailbox timeout");
        unsigned result=dut.gpi&0x40ffff;
        for (int i=0;i<4;++i) tick();
        return result;
    }
    void ok(unsigned op,unsigned index,unsigned arg) {
        require(command(op,index,arg)==0,"command rejected");
    }
    void error(unsigned op,unsigned index,unsigned arg,unsigned code) {
        require(command(op,index,arg)==(0x400000|code),"incorrect rejection");
    }
    uint8_t peek(unsigned addr) {
        dut.peek_addr=addr; dut.eval(); tick(); tick(); return dut.peek_data;
    }
};
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    Bench b;
    require(b.dut.exec_reset,"startup must hold reset");
    require(b.command(1,7,0)&128,"firmware capability absent");
    require(b.peek(0)==0xc3 && b.peek(2)==0x80,"open reset shim missing");
    b.error(15,0,8191,3); b.error(15,1,8192,2);
    b.error(16,0,0,4); b.error(17,0,0,4);
    b.ok(15,0,8192);
    b.error(15,0,8192,4); b.error(17,0,0,4);
    b.error(16,1,0xab,3); b.error(16,2,0xab,2);
    std::array<uint8_t,8192> firmware;
    for(unsigned i=0;i<firmware.size();++i) firmware[i]=uint8_t((i*37)^(i>>5));
    // Open test firmware writes a marker into CPU RAM, then halts.
    const uint8_t program[]={0x3e,0x5a,0x32,0x00,0x60,0x76};
    for(unsigned i=0;i<sizeof(program);++i) firmware[i]=program[i];
    for(unsigned i=0;i<firmware.size();i+=2) {
        b.ok(16,0,firmware[i]|(unsigned(firmware[i+1])<<8));
        require(b.dut.exec_reset,"firmware write released CPU");
    }
    b.error(16,0,0,3); b.error(17,1,0,2); b.error(17,0,1,3);
    for(unsigned i=0;i<firmware.size();++i)
        require(b.peek(i)==firmware[i],"firmware RAM differs from uploaded bytes");
    b.ok(17,0,0);
    require(b.dut.exec_reset,"firmware commit released CPU");
    b.error(2,0,1,4); // Cartridge is still required.
    b.ok(4,0,1); b.ok(5,1,0x76); b.ok(6,0,0);
    b.ok(2,0,1);
    for(unsigned i=0;i<20000 && b.dut.cpu_halt_n;++i) b.tick();
    require(!b.dut.cpu_halt_n,"uploaded firmware did not execute");
    require(b.peek(0x6000)==0x5a,"CPU did not execute firmware marker write");
    b.error(15,0,8192,4);
    b.ok(2,0,0); b.ok(15,0,8192);
    b.ok(16,0,0);
    b.error(2,0,1,4); // Existing cartridge cannot release partial firmware.
    std::cout << "firmware RAM, reset ordering and CPU execution passed\n";
}
