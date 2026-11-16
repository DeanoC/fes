// SPDX-License-Identifier: GPL-3.0-or-later
// Host-only Verilator probe. Its simple text transport exposes the real CSR
// model to the native Go identity check; it is not an FPGA programming path.
#include "Vembedded_producer_top.h"
#include <iomanip>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>

static void require(bool ok, const char* why) { if (!ok) throw std::runtime_error(why); }
struct Bench {
    Vembedded_producer_top d;
    Bench() {
        d.reset_n=0; d.csr_valid=0; d.csr_write=0; d.csr_address=0; d.csr_wdata=0; d.csr_wstrb=15; d.csr_response_ready=1;
        d.reader_valid=0; d.reader_address=0; d.reader_length=0; d.reader_rready=1;
        d.m_arready=0; d.m_rvalid=0; d.m_rlast=0; d.m_rresp=0; d.m_rid=0;
        for (unsigned i=0;i<4;++i) d.m_rdata[i]=0;
        tick(); tick(); d.reset_n=1; tick();
    }
    void tick() { d.clk=0;d.eval();d.clk=1;d.eval();d.clk=0;d.eval(); }
    bool access(bool write, uint32_t offset, uint32_t& value) {
        require(offset<256 && offset%4==0,"invalid CSR offset");
        d.csr_valid=1;d.csr_write=write;d.csr_address=offset;d.csr_wdata=value;d.csr_wstrb=15;d.eval();
        require(d.csr_ready,"CSR not ready");tick();require(d.csr_response_valid,"CSR response missing");
        bool ok=!d.csr_error;value=d.csr_rdata;d.csr_valid=0;tick();return ok;
    }
    uint32_t read(uint32_t offset) { uint32_t v=0;require(access(false,offset,v),"CSR read failed");return v; }
    void write(uint32_t offset,uint32_t v) {require(access(true,offset,v),"CSR write failed");}
    void offer() {
        d.reader_valid=1;d.reader_address=0x1000;d.reader_length=1;d.eval();
        require(d.reader_ready,"reader was not enabled");tick();d.reader_valid=0;d.m_arready=0;tick();
        require(d.m_arvalid,"offered address not held");
    }
    void accept() {require(d.m_arvalid,"no held address");d.m_arready=1;tick();d.m_arready=0;tick();}
    void beat(bool last) { d.m_rvalid=1;d.m_rlast=last;d.eval();require(d.m_rready,"response not accepted");tick();d.m_rvalid=0;tick(); }
};

static std::string build_id(Bench& b) {
    std::ostringstream out;
    for (int i=3;i>=0;--i) out<<std::hex<<std::setfill('0')<<std::setw(8)<<b.read(12+4*i);
    return out.str();
}
static void probe(const std::string& expected, uint32_t abi) {
    Bench b;
    require(b.read(0)==0x46455345 && b.read(4)==0x00010000,"control signature mismatch");
    require(b.read(8)==abi,"control ABI mismatch");
    std::string observed=build_id(b);
    require(observed==expected,"live build identity mismatch");
    require(b.read(0x38)==0 && b.read(0x1c)==0,"reset falsely acknowledged drain");
    b.write(0x24,1);b.write(0x28,0);b.write(0x34,1);b.offer();
    b.write(0x2c,7);b.write(0x30,0);b.write(0x34,2);
    require(b.d.reader_abort && b.d.m_arvalid && b.d.m_araddr==0x1000 && b.read(0x38)==2,"disable lost held address");
    b.accept();b.beat(false);
    require(b.read(0x38)==2,"partial burst falsely drained");b.beat(true);
    require(b.read(0x38)==4 && b.read(0x3c)==0 && b.read(0x40)==1 && b.read(0x48)==7,"drain acknowledgement mismatch");
    std::cout<<"{\"status\":\"passed\",\"build_id\":\""<<observed<<"\",\"control_abi\":"<<abi<<",\"drain\":true}"<<std::endl;
}
static void serve() {
    Bench b; std::string line;
    while (std::getline(std::cin,line)) {
        std::istringstream in(line);std::string op;uint32_t offset=0,value=0;in>>op;
        if(op=="read") {in>>std::hex>>offset;bool ok=b.access(false,offset,value);std::cout<<"R "<<ok<<" "<<std::hex<<value<<std::dec<<std::endl;}
        else if(op=="write") {in>>std::hex>>offset>>value;bool ok=b.access(true,offset,value);std::cout<<"R "<<ok<<std::endl;}
        else throw std::runtime_error("unknown test operation");
    }
}
int main(int argc,char** argv) {
    try {
        if(argc==2 && std::string(argv[1])=="--serve") {serve();return 0;}
        if(argc!=3) throw std::runtime_error("usage: probe BUILD_ID CONTROL_ABI | --serve");
        std::string expected=argv[1];
        require(expected.size()==32 && expected.find_first_not_of("0123456789abcdef")==std::string::npos,"invalid expected build ID");
        std::size_t n=0;unsigned long long abi=std::stoull(argv[2],&n,10);
        require(n==std::string(argv[2]).size() && abi>0 && abi<=0xffffffffULL,"invalid expected ABI");
        probe(expected,static_cast<uint32_t>(abi));return 0;
    } catch(const std::exception& error) {std::cerr<<"FAIL "<<error.what()<<"\n";return 1;}
}
