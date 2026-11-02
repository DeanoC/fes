// SPDX-License-Identifier: GPL-3.0-or-later
// Verilator client for the same Avalon aperture used by the board diagnostic.
#include "Vfes_de25_embedded.h"
#include <cstdint>
#include <iomanip>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>

static void require(bool ok, const char *message) { if (!ok) throw std::runtime_error(message); }
struct Bench {
    Vfes_de25_embedded d;
    Bench() {
        d.reset_n=0; d.address=0; d.read=0; d.write=0; d.byteenable=15; d.writedata=0;
        d.ddr_arready=1; d.ddr_rvalid=1; d.ddr_rlast=1; d.ddr_rresp=3; d.ddr_rid=15;
        tick(); tick(); d.reset_n=1; tick();
    }
    void tick() {
        d.clk=0; d.eval(); d.clk=1; d.eval(); d.clk=0; d.eval();
        require(!d.ddr_arvalid && !d.ddr_rready,"on-chip fixture reached the DDR bus");
    }
    bool access(bool write, uint32_t offset, uint32_t &value, uint32_t byteenable=15) {
        require(offset<256 && offset%4==0,"invalid test offset");
        d.address=offset; d.read=!write; d.write=write; d.byteenable=byteenable; d.writedata=value; d.eval();
        unsigned timeout=16;
        while (d.waitrequest && timeout--) tick();
        require(!d.waitrequest,"Avalon request timeout");
        tick(); d.read=0; d.write=0;
        timeout=16;
        while (!(write ? d.writeresponsevalid : d.readdatavalid) && timeout--) tick();
        require(write ? d.writeresponsevalid : d.readdatavalid,"Avalon response timeout");
        value=d.readdata; bool ok=d.response==0; tick(); return ok;
    }
    uint32_t read(uint32_t offset) { uint32_t v=0; require(access(false,offset,v),"read rejected"); return v; }
    void write(uint32_t offset,uint32_t value) { require(access(true,offset,value),"write rejected"); }
};

static std::string build_id(Bench &b) {
    std::ostringstream out;
    for (int i=3;i>=0;--i) out<<std::hex<<std::setfill('0')<<std::setw(8)<<b.read(12+4*i);
    return out.str();
}
static void enable(Bench &b, uint32_t generation) {
    b.write(0x24,generation); b.write(0x28,0); b.write(0x34,1);
}
static void probe(const std::string &expected, uint32_t abi) {
    Bench b;
    require(b.read(0)==0x46455345 && b.read(4)==0x00010000,"signature mismatch");
    require(b.read(8)==abi,"control ABI mismatch");
    require(build_id(b)==expected,"live build identity mismatch");
    require(b.read(0x38)==0 && b.read(0x3c)==0,"reset falsely acknowledged drain");
    uint32_t bad=256;
    require(!b.access(true,0x88,bad),"oversized diagnostic length accepted");
    bad=0;
    require(!b.access(true,0x34,bad,1),"partial command accepted");
    enable(b,1);
    b.write(0x84,0x1000); b.write(0x88,1); b.write(0x80,0x0c);
    require((b.read(0x8c)&4) && b.read(0x90)==0x1000,"offered AXI address not held");
    b.write(0x2c,7); b.write(0x30,0); b.write(0x34,2);
    require(b.read(0x38)==2 && (b.read(0x8c)&4),"disable lost held AXI address");
    b.write(0x80,0x05);
    require((b.read(0x8c)&8)!=0,"AXI address not accepted");
    b.write(0x80,0x07);
    require(b.read(0x38)==2 && b.read(0x3c)==1,"partial burst falsely drained");
    b.write(0x80,0x07);
    require(b.read(0x38)==4 && b.read(0x3c)==0 && b.read(0x40)==1 && b.read(0x48)==7,"drain acknowledgement mismatch");
    // An erroneous response must latch a fault and never claim successful drain.
    Bench fault; enable(fault,1);
    fault.write(0x84,0x2000); fault.write(0x88,0); fault.write(0x80,0x0d);
    require((fault.read(0x8c)&8)!=0,"fault test address not accepted");
    fault.write(0x80,0x17);
    require((fault.read(0x38)&8)!=0 && (fault.read(0x38)&4)==0,"bad response did not latch fault");
    std::cout<<"{\"status\":\"passed\",\"build_id\":\""<<expected<<"\",\"control_abi\":"<<abi
             <<",\"held_address\":true,\"partial_drain\":true,\"fault\":true}"<<std::endl;
}
static void serve() {
    Bench b; std::string line;
    while (std::getline(std::cin,line)) {
        std::istringstream in(line); std::string op; uint32_t offset=0,value=0; in>>op;
        if (op=="read") { in>>std::hex>>offset; bool ok=b.access(false,offset,value); std::cout<<"R "<<ok<<" "<<std::hex<<value<<std::dec<<std::endl; }
        else if (op=="write") { in>>std::hex>>offset>>value; bool ok=b.access(true,offset,value); std::cout<<"R "<<ok<<std::endl; }
        else throw std::runtime_error("unknown operation");
    }
}
int main(int argc,char **argv) {
    try {
        if (argc==2 && std::string(argv[1])=="--serve") { serve(); return 0; }
        if (argc!=3) throw std::runtime_error("usage: probe BUILD_ID CONTROL_ABI | --serve");
        std::string id=argv[1];
        require(id.size()==32 && id.find_first_not_of("0123456789abcdef")==std::string::npos,"invalid build ID");
        std::size_t n=0; auto abi=std::stoull(argv[2],&n,10);
        require(n==std::string(argv[2]).size() && abi>0 && abi<=0xffffffffULL,"invalid ABI");
        probe(id,static_cast<uint32_t>(abi)); return 0;
    } catch (const std::exception &error) { std::cerr<<"FAIL "<<error.what()<<"\n"; return 1; }
}
