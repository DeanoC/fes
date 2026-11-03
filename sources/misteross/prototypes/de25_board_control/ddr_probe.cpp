// SPDX-License-Identifier: GPL-3.0-or-later
// Drive the actual board aperture and external AXI bus, never a synthetic ACK.
#include "Vfes_de25_embedded.h"
#include <cstdint>
#include <iomanip>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

static void require(bool value,const char *message) { if (!value) throw std::runtime_error(message); }
struct Bench {
    Vfes_de25_embedded d;
    unsigned addresses=0, responses=0;
    uint32_t accepted_address=0;
    unsigned accepted_length=0;
    Bench() {
        d.clk=0; d.reset_n=0; d.address=0; d.read=0; d.write=0;
        d.byteenable=15; d.writedata=0; d.ddr_arready=0; d.ddr_rvalid=0;
        d.ddr_rresp=0; d.ddr_rid=0; d.ddr_rlast=0;
        d.ddr_awready=d.ddr_wready=1; d.ddr_bvalid=0; d.ddr_bresp=0; d.ddr_bid=0;
        for (unsigned i=0;i<8;i++) d.ddr_rdata[i]=0;
        tick(); tick(); d.reset_n=1; tick();
    }
    void tick() {
        d.clk=0; d.eval();
        require(!d.ddr_awvalid && !d.ddr_wvalid && !d.ddr_wstrb,"DDR reader issued a write");
        bool ar=d.ddr_arvalid && d.ddr_arready, r=d.ddr_rvalid && d.ddr_rready;
        bool b=d.ddr_bvalid && d.ddr_bready;
        if (ar) {
            require(d.ddr_arsize==5 && d.ddr_arburst==1 && d.ddr_arid==0 &&
                    d.ddr_arprot==2 && d.ddr_arlock==0 && d.ddr_arcache==0 && d.ddr_arqos==0,
                    "AXI read attributes differ");
            ++addresses; accepted_address=d.ddr_araddr; accepted_length=d.ddr_arlen;
        }
        if (r) ++responses;
        d.clk=1; d.eval();
        if (r) d.ddr_rvalid=0; // The external source advances only on handshake.
        if (b) d.ddr_bvalid=0;
        d.clk=0; d.eval();
    }
    bool access(bool write,uint32_t offset,uint32_t &value,uint32_t strobes=15) {
        d.address=offset; d.read=!write; d.write=write; d.byteenable=strobes; d.writedata=value; d.eval();
        unsigned bound=32;
        while (d.waitrequest && bound--) tick();
        require(!d.waitrequest,"Avalon request bound");
        tick(); d.read=d.write=0;
        bound=32;
        while (!(write ? d.writeresponsevalid : d.readdatavalid) && bound--) tick();
        require(write ? d.writeresponsevalid : d.readdatavalid,"Avalon response bound");
        bool ok=d.response==0; value=d.readdata; tick(); return ok;
    }
    void write(uint32_t a,uint32_t v) { require(access(true,a,v),"Avalon write rejected"); }
    bool rejected(uint32_t a,uint32_t v) { return !access(true,a,v); }
    uint32_t read(uint32_t a) { uint32_t v=0; require(access(false,a,v),"Avalon read rejected"); return v; }
    void enable() { write(0x24,1); write(0x28,0); write(0x34,1); }
    void disable() { write(0x2c,7); write(0x30,0); write(0x34,2); }
    void stage(uint32_t address,unsigned length) { write(0x84,address); write(0x88,length); }
    void response(uint32_t word,bool last,unsigned code=0,unsigned id=0) {
        require(!d.ddr_rvalid,"source overwrote a stalled AXI response");
        for (unsigned i=0;i<8;i++) d.ddr_rdata[i]=word+i;
        d.ddr_rlast=last; d.ddr_rresp=code; d.ddr_rid=id; d.ddr_rvalid=1; d.eval();
    }
    void beat(uint32_t word,bool last,unsigned code=0,unsigned id=0) {
        response(word,last,code,id);
        for(unsigned i=0;i<8 && d.ddr_rvalid;i++)tick();
        require(!d.ddr_rvalid,"response was not accepted");
        tick(); // The accepted wide response still owes its upper reader half.
    }
    void drained() {
        // The ACK is registered after the final accepted response. Poll its
        // actual state; the bus model must not invent a same-cycle ACK.
        uint32_t status=0;
        for (unsigned i=0;i<8;i++) {
            status=read(0x38);
            if (status==4) break;
            require(status==2,"reader faulted while awaiting drain");
        }
        require(status==4 && read(0x3c)==0 && read(0x40)==1 && read(0x44)==0 &&
                read(0x48)==7 && read(0x4c)==0,"exact drain acknowledgement absent");
    }
};
static std::string identity(Bench &b) {
    std::ostringstream s;
    for (int i=3;i>=0;--i) s<<std::hex<<std::setw(8)<<std::setfill('0')<<b.read(12+4*i);
    return s.str();
}
int main(int argc,char **argv) {
    try {
        require(argc==5,"usage: model BUILD_ID ABI BUS_BASE BYTES");
        const std::string id=argv[1];
        const auto abi=static_cast<uint32_t>(std::stoul(argv[2],nullptr,0));
        const auto base=static_cast<uint32_t>(std::stoul(argv[3],nullptr,0));
        const auto span=static_cast<uint32_t>(std::stoul(argv[4],nullptr,0));
        std::vector<std::string> cases;
        Bench b;
        require(b.read(8)==abi && abi==0x10203041,"DDR control ABI mismatch");
        require(identity(b)==id,"DDR live build identity mismatch");
        require(b.read(0xb0)==base && b.read(0xb4)==span,"compiled DDR window differs");
        require(!b.d.maintenance_reset_n && b.read(0xb8)==1,"maintenance masters are not held in reset");
        require(!b.d.ddr_arvalid && b.read(0x40)==0 && b.read(0x48)==0,"reset grants traffic or drain");
        cases.push_back("distinct-identity-and-closed-reset");
        b.enable(); b.stage(base+32,3); b.write(0x80,12);
        require(b.d.ddr_arvalid && b.d.ddr_araddr==base+32 && b.d.ddr_arlen==1,"DDR address not offered");
        // Staging another descriptor cannot redirect an already held address.
        b.stage(base+64,0);
        for (unsigned i=0;i<8;i++) {
            b.tick(); require(b.d.ddr_arvalid && b.d.ddr_araddr==base+32 && b.d.ddr_arlen==1,"stalled address changed");
        }
        cases.push_back("held-address-and-descriptor-snapshot");
        require(b.rejected(0x80,12),"second outstanding diagnostic burst accepted");
        b.disable();
        require(b.d.ddr_arvalid && b.read(0x38)==2 && b.read(0x48)==0,"held address was withdrawn or acknowledged");
        for (unsigned i=0;i<64;i++) b.tick();
        require(b.read(0x38)==2 && b.read(0x48)==0,"elapsed cycles invented drain");
        cases.push_back("disable-preserves-stalled-address");
        b.d.ddr_arready=1; b.tick();
        require(b.addresses==1 && b.accepted_address==base+32 && b.accepted_length==1,"DDR accepted wrong descriptor");
        b.beat(0x10203040,false);
        require(b.read(0x38)==2 && b.read(0x3c)==1 && b.read(0x48)==0,"partial burst acknowledged drain");
        cases.push_back("partial-burst-does-not-drain");
        b.beat(0xaabbcc00,true); b.drained();
        require(b.read(0x94)==64 && b.read(0xa0)==0xaabbcc04 && b.read(0xac)==0xaabbcc07,"real DDR data missing");
        cases.push_back("final-data-and-exact-drain");

        Bench stalled; stalled.enable(); stalled.stage(base+128,3); stalled.d.ddr_arready=1;
        stalled.write(0x80,8); stalled.response(0x55550000,false);
        for (unsigned i=0;i<16;i++) { stalled.tick(); require(stalled.d.ddr_rvalid && !stalled.d.ddr_rready,"reader backpressure lost"); }
        require(stalled.read(0x94)==0,"unaccepted data counted");
        stalled.disable(); require(!stalled.d.ddr_rvalid && stalled.responses==1,"disable failed to drain held response");
        require(stalled.read(0x48)==0,"first held response falsely drained");
        stalled.beat(0x77770000,true); stalled.drained();
        cases.push_back("response-backpressure-and-disable");

        struct Descriptor { const char *name; uint32_t address; unsigned length; bool accepted; };
        const Descriptor descriptors[]={{"first-full-beat",base,1,true},{"last-full-beat",base+span-32,1,true},
            {"narrow-beat",base,0,false},{"half-aligned",base+16,1,false},
            {"whole-page",base,255,true},{"below-window",base-16,0,false},
            {"past-window",base+span,0,false},{"burst-past-window",base+span-16,1,false},
            {"unaligned",base+1,0,false},{"cross-page",base+4080,1,false},
            {"address-wrap",0xfffffff0U,1,false}};
        for (const auto &test:descriptors) {
            Bench n; n.enable(); n.stage(test.address,test.length);
            auto v=uint32_t(12); bool accepted=n.access(true,0x80,v);
            require(accepted==test.accepted,test.name);
            if (!accepted) require(!n.d.ddr_arvalid && n.addresses==0,"rejected descriptor reached DDR");
            cases.push_back(test.name);
        }
        for (uint32_t command:{1U,2U,5U,7U,0x17U,0x20U}) {
            Bench n; n.enable(); n.stage(base,0);
            require(n.rejected(0x80,command) && !n.d.ddr_arvalid,"synthetic-memory command reached DDR");
        }
        cases.push_back("synthetic-response-commands-rejected");
        Bench partial; partial.enable(); auto value=uint32_t(12);
        require(!partial.access(true,0x80,value,1) && !partial.d.ddr_arvalid,"partial request command accepted");
        require(partial.rejected(0x88,256),"oversized length accepted");
        cases.push_back("partial-command-and-length-rejected");

        Bench page; page.enable(); page.stage(base,255); page.d.ddr_arready=1; page.write(0x80,12);
        uint32_t checksum=0;
        for (unsigned beat=0;beat<128;beat++) {
            uint32_t word=0x12340000U+beat*7U;
            for(unsigned lane=0;lane<8;lane++)checksum^=word+lane;
            page.beat(word,beat==127);
        }
        require(page.responses==128 && page.read(0x94)==4096 && page.read(0x98)==checksum,"whole-page data mismatch");
        page.disable(); page.drained(); cases.push_back("maximum-burst-data-and-drain");

        for (unsigned fault=0;fault<4;fault++) {
            Bench n; n.enable(); n.stage(base,3); n.d.ddr_arready=1; n.write(0x80,12);
            n.beat(1,fault==2,fault==0 ? 2 : 0,fault==1 ? 1 : 0);
            if (fault==3) n.beat(2,false); else n.beat(2,true);
            require((n.read(0x38)&8) && !(n.read(0x38)&4) && n.read(0x48)==0,"AXI fault supplied drain");
            require(n.rejected(0x34,2) && n.rejected(0x80,12),"faulted reader restarted");
            cases.push_back(std::string("sticky-bus-fault-")+std::to_string(fault));
        }
        Bench unsolicited; unsolicited.response(1,true); unsolicited.tick(); unsolicited.tick();
        require((unsolicited.read(0x38)&8) && unsolicited.read(0x48)==0,"unsolicited DDR response ignored");
        Bench write_response; write_response.d.ddr_bvalid=1; write_response.tick();
        require((write_response.read(0x38)&8) && write_response.read(0x48)==0,"unsolicited write response ignored");
        Bench active_write_response; active_write_response.enable(); active_write_response.stage(base,1);
        active_write_response.d.ddr_arready=1; active_write_response.write(0x80,12); active_write_response.disable();
        active_write_response.d.ddr_bvalid=1; active_write_response.tick();
        require((active_write_response.read(0x38)&8) && active_write_response.read(0x48)==0,
                "write response acknowledged an outstanding read");
        cases.push_back("unsolicited-response-fails-closed");
        std::cout<<"{\"status\":\"passed\",\"backend\":\"hps-ddr\",\"build_id\":\""<<id
                 <<"\",\"control_abi\":"<<abi<<",\"bus_base\":"<<base<<",\"bytes\":"<<span<<",\"cases\":[";
        for (unsigned i=0;i<cases.size();i++) std::cout<<(i ? "," : "")<<'"'<<cases[i]<<'"';
        std::cout<<"],\"physical_ddr\":false,\"runtime_admission\":false}"<<std::endl;
        return 0;
    } catch (const std::exception &error) { std::cerr<<"FAIL "<<error.what()<<'\n'; return 1; }
}
