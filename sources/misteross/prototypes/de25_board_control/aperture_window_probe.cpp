// SPDX-License-Identifier: GPL-3.0-or-later
// Actual Nano Avalon wrapper with an independently sequenced external AXI RAM.
#include "Vfes_de25_embedded.h"
#include <cstdint>
#include <deque>
#include <fstream>
#include <iostream>
#include <iterator>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

static void require(bool value,const char *message) { if(!value)throw std::runtime_error(message); }
struct Burst { uint32_t address;unsigned beats,index; };
struct Bench {
    Vfes_de25_embedded d;
    std::vector<std::vector<unsigned char>> frames;
    std::deque<Burst> queue;
    uint32_t base;
    unsigned selected=0,mode=0;
    uint64_t cycles=0,addresses=0,responses=0,frame_responses=0;
    uint32_t request_row=0,request_offset=0,requested=0;
    bool ar_held=false,r_held=false;
    uint32_t held_address=0;unsigned held_length=0;
    Bench(uint32_t start,const char *a,const char *b):base(start) {
        for(const auto *path:{a,b}) {
            std::ifstream input(path,std::ios::binary);require(input.good(),"missing actual renderer frame");
            frames.emplace_back(std::istreambuf_iterator<char>(input),std::istreambuf_iterator<char>());
            require(frames.back().size()==5120*720,"renderer frame geometry differs");
        }
        d.clk=0;d.reset_n=0;d.read=0;d.write=0;d.address=0;d.byteenable=15;d.writedata=0;
        d.ddr_arready=0;d.ddr_rvalid=0;d.ddr_rlast=0;d.ddr_rresp=0;d.ddr_rid=0;
        d.ddr_awready=1;d.ddr_wready=1;d.ddr_bvalid=0;d.ddr_bresp=0;d.ddr_bid=0;
        for(unsigned i=0;i<8;i++)d.ddr_rdata[i]=0;
        tick();tick();d.reset_n=1;tick();
    }
    uint32_t word(uint32_t address) const {
        require(address>=base,"AXI read before framebuffer");
        uint64_t offset=uint64_t(address)-base;require(offset+4<=frames[selected].size(),"AXI read beyond frame");
        const auto *p=&frames[selected][offset];
        return uint32_t(p[0])|uint32_t(p[1])<<8|uint32_t(p[2])<<16|uint32_t(p[3])<<24;
    }
    void tick() {
        require(++cycles<8000000,"model cycle bound");
        d.clk=0;
        d.ddr_arready=mode!=1 && cycles%7!=2;
        if(!r_held) {
            d.ddr_rvalid=mode!=2 && !queue.empty() && cycles%5!=1;
            d.ddr_rlast=0;d.ddr_rresp=0;d.ddr_rid=0;
            for(unsigned i=0;i<8;i++)d.ddr_rdata[i]=0;
            if(d.ddr_rvalid) {
                const auto &q=queue.front();d.ddr_rlast=q.index+1==q.beats;
                for(unsigned i=0;i<8;i++)d.ddr_rdata[i]=word(q.address+q.index*32+i*4);
                if(frame_responses==7) {
                    if(mode==3)d.ddr_rresp=2;
                    if(mode==4)d.ddr_rid=1;
                    if(mode==5)d.ddr_rlast=!d.ddr_rlast;
                    if(mode==6)d.ddr_rdata[0]^=0x100;
                }
            }
        }
        d.eval();
        require(!d.maintenance_reset_n,"window reader released maintenance masters");
        require(!d.ddr_awvalid && !d.ddr_wvalid && !d.ddr_wstrb,"window reader issued a write");
        if(ar_held)require(d.ddr_arvalid && d.ddr_araddr==held_address && d.ddr_arlen==held_length,
            "stalled AXI address disappeared or changed");
        bool ar=d.ddr_arvalid && d.ddr_arready,r=d.ddr_rvalid && d.ddr_rready;
        Burst offered{d.ddr_araddr,unsigned(d.ddr_arlen)+1,0};
        if(ar) {
            require(d.ddr_arsize==5 && d.ddr_arburst==1 && d.ddr_arid==0 && d.ddr_arprot==2 &&
                d.ddr_arcache==0 && !d.ddr_arlock && !d.ddr_arqos,"AXI read attributes differ");
            uint32_t expected=base+request_row*5120+request_offset;
            unsigned bytes=5120-request_offset,page=4096-(expected&4095);if(bytes>page)bytes=page;
            require(requested<5120*720 && offered.address==expected && offered.beats*32==bytes,
                "AXI request skipped, replayed or crossed the expected row/page");
            request_offset+=bytes;requested+=bytes;
            if(request_offset==5120){request_offset=0;++request_row;}
        }
        ar_held=d.ddr_arvalid && !d.ddr_arready;held_address=d.ddr_araddr;held_length=d.ddr_arlen;
        r_held=d.ddr_rvalid && !d.ddr_rready;
        d.clk=1;d.eval();
        if(r) {
            require(!queue.empty(),"model accepted an unsolicited response");
            ++responses;++frame_responses;if(++queue.front().index==queue.front().beats)queue.pop_front();
        }
        if(ar){queue.push_back(offered);++addresses;require(queue.size()<=4,"more than four external reads outstanding");}
        d.clk=0;d.eval();
    }
    bool access(bool write,uint32_t offset,uint32_t &value,uint32_t strobes=15) {
        require(offset<=255,"Avalon offset overflow");
        d.address=offset;d.read=!write;d.write=write;d.byteenable=strobes;d.writedata=value;d.eval();
        unsigned count=0;while(d.waitrequest && count++<32)tick();require(!d.waitrequest,"Avalon request bound");
        tick();d.read=d.write=0;count=0;
        while(!(write ? d.writeresponsevalid : d.readdatavalid) && count++<32)tick();
        require(write ? d.writeresponsevalid : d.readdatavalid,"Avalon response bound");
        bool ok=d.response==0;value=d.readdata;tick();return ok;
    }
    uint32_t read(uint32_t offset) {
        uint32_t value=0;require(access(false,offset,value),"model control observation rejected");return value;
    }
    bool write(uint32_t offset,uint32_t value,uint32_t strobes) {
        if(offset==0x80 && (read(0x90)&1)) {
            // No transfer is active when start-ready is true. Reset only this
            // independent request oracle, never the RTL ledger or statistics.
            request_row=request_offset=requested=0;frame_responses=0;
        }
        return access(true,offset,value,strobes);
    }
};
int main(int argc,char **argv) {
    try {
        require(argc==4,"usage: aperture-model BASE FRAME0 FRAME1");
        Bench b(uint32_t(std::stoul(argv[1],nullptr,0)),argv[2],argv[3]);
        std::string line;
        while(std::getline(std::cin,line)) {
            require(line.size()<128,"model command line bound");
            std::istringstream in(line);std::string op;uint32_t a=0,v=0,s=15;in>>op>>std::hex;
            bool ok=true;std::vector<uint64_t> values;
            if(op=="read") {require(bool(in>>a),"read syntax");ok=b.access(false,a,v);values.push_back(v);}
            else if(op=="write") {require(bool(in>>a>>v),"write syntax");if(in>>s){}ok=b.write(a,v,s);}
            else if(op=="tick") {require(bool(in>>a)&&a<=16384,"tick bound");for(uint32_t i=0;i<a;i++)b.tick();}
            else if(op=="mode") {require(bool(in>>a)&&a<=6,"mode bound");b.mode=a;}
            else if(op=="load") {require(bool(in>>a)&&a<2 && b.queue.empty() && !(b.read(0x90)&2),"memory changed before retirement");b.selected=a;}
            else if(op=="stats")values={b.addresses,b.responses,b.queue.size(),b.requested};
            else throw std::runtime_error("unknown model command");
            std::cout<<"R "<<(ok?1:0);for(auto value:values)std::cout<<" "<<std::hex<<value;
            std::cout<<std::dec<<"\n"<<std::flush;
        }
        return 0;
    }catch(const std::exception &e){std::cerr<<"window aperture: "<<e.what()<<"\n";return 1;}
}
