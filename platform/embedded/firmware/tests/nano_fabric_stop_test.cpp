/* SPDX-License-Identifier: BSD-3-Clause */
/* The inherited bench supplies its real Avalon RTL and memory bus model. */
#define main original_probe_cases
#include "probe_tb.cpp"
#undef main
#include "Vfes_de25_embedded.h"
#include "nano_fabric_stop.h"
#include "nano_ddr_read.h"
#include "generated/fes_embedded_control.h"
#include <algorithm>
#include <cstring>

struct Fixture {
#ifdef FES_WINDOW_STOP_TEST
    static constexpr bool window=true;
#else
    static constexpr bool window=false;
#endif
    Vfes_de25_embedded d;
    Probe p;
    struct fes_nano_fabric_stop stop{};
    struct Burst { unsigned left,index=0; };
    std::deque<Burst> pending;
    std::string fault;
    unsigned reads=0,probe_reads=0,writes=0,cancels=0,waits=0,fail_read=0;
    bool owned=true,stall=true,responses=false,golden=false;
    unsigned addresses=0,beats=0;
    uint64_t expected_stop_cookie=7;
    unsigned request_row=0,request_offset=0;
    static constexpr uint32_t build[4]={0xccddeeff,0x8899aabb,0x44556677,0x00112233};
    /* Independent little-endian vector from hps_context.reader_pattern(),
     * not the helper under test, supplies the external memory bus. */
    static constexpr uint32_t golden_data[8]={0x4e414e4f,0xd07637f6,0x722fbd3d,0x94e72364,
                                             0x369ca8ab,0x59542ed2,0xfb0d9419,0x1dc51a40};
    explicit Fixture(std::string f="",bool active=true):fault(std::move(f)) {
        stop.io={this,owns,read,write,wait};stop.probe.io={this,owns,probe_read,probe_write,wait};
        stop.abi=window?0x10203042:0x10203041;std::copy(std::begin(build),std::end(build),stop.build);
        stop.bus_base=0x95000000;stop.bytes=window?(4U<<20):8192;stop.cookie=7;stop.qualified=1;
        d.reset_n=0;d.address=0;d.read=0;d.write=0;d.byteenable=15;d.writedata=0;
        d.ddr_bvalid=0;d.ddr_bresp=0;d.ddr_bid=0;d.ddr_awready=0;d.ddr_wready=0;
        tick();tick();d.reset_n=1;tick();
        if(active) {
            csr_write(36,1);csr_write(40,0);csr_write(52,1);
            if(window) {csr_write(0x84,1);csr_write(0x88,0);csr_write(0x8c,1);csr_write(0x80,1);}
            else {csr_write(0x84,0x95000000);csr_write(0x88,1);csr_write(0x80,12);}
            p.start(Descriptor{});
        }
    }
    void tick() {
        d.clk=0;d.ddr_arready=!stall;
        bool extra=fault=="extra-response" && beats!=0;
        d.ddr_rvalid=responses && (!pending.empty() || extra);d.ddr_rlast=extra || (!pending.empty() && pending.front().left==1);
        d.ddr_rresp=fault=="response"?2:0;d.ddr_rid=fault=="response-id"?1:0;
        if(fault=="early-last")d.ddr_rlast=1;
        if(fault=="missing-last")d.ddr_rlast=0;
        for(unsigned i=0;i<8;i++)d.ddr_rdata[i]=golden ?
            golden_data[((pending.empty()?0:pending.front().index)*8+i)%8]:0x12345678;
        if(fault=="wrong-data")d.ddr_rdata[2]^=1;
        d.eval();bool ar=d.ddr_arvalid && d.ddr_arready,r=d.ddr_rvalid && d.ddr_rready;
        require(!d.ddr_awvalid && !d.ddr_wvalid && !d.ddr_wstrb,"reader issued a DDR write");
        unsigned count=d.ddr_arlen+1;
        if(ar) {
            uint32_t expected=window?0x95000000+request_row*5120+request_offset:0x95000000;
            unsigned bytes=window?std::min(5120-request_offset,4096-(expected&4095)):32;
            require(d.ddr_araddr==expected && d.ddr_arsize==5 && count*32==bytes,"reader address/length differs");
            if(window) {request_offset+=bytes;if(request_offset==5120){request_offset=0;request_row++;}}
            addresses++;
        }
        d.clk=1;d.eval();
        if(r) {
            beats++;
            if(!pending.empty()) {pending.front().index++;if(!--pending.front().left)pending.pop_front();}
            else require(extra,"unsolicited model response was not injected deliberately");
        }
        if(ar)pending.push_back({count});
        d.clk=0;d.eval();
    }
    uint32_t access(bool writing,uint32_t offset,uint32_t value=0) {
        d.address=offset;d.read=!writing;d.write=writing;d.writedata=value;d.eval();
        unsigned n=0;while(d.waitrequest && n++<16)tick();require(!d.waitrequest,"CSR stuck");
        tick();d.read=0;d.write=0;n=0;
        while(!(writing?d.writeresponsevalid:d.readdatavalid) && n++<16)tick();
        require((writing?d.writeresponsevalid:d.readdatavalid) && !d.response,"CSR error");
        uint32_t result=d.readdata;tick();return result;
    }
    void csr_write(uint32_t offset,uint32_t value) { access(true,offset,value); }
    static int owns(void *context) {
        auto& f=*static_cast<Fixture*>(context);
        return !f.owned || f.fault=="scope" ? 0 : f.fault=="scope-two"?2:1;
    }
    static int read(void *context,uint32_t offset,uint32_t *value) {
        auto& f=*static_cast<Fixture*>(context);
        if(++f.reads==f.fail_read)return -7;
        *value=f.access(false,offset);
        if(f.fault=="read-positive")return 5;
        if(f.fault=="identity" && offset==0)*value^=1;
        if(f.fault=="version" && offset==4)*value^=1;
        if(f.fault=="abi" && offset==8)*value^=1;
        if(f.fault=="build" && offset==12)*value^=1;
        if(f.fault=="base" && offset==0xb0)*value^=4096;
        if(f.fault=="span" && offset==0xb4)*value^=4096;
        if(f.fault=="maintenance" && offset==(window?0xc8U:0xb8U))*value=0;
        if(f.fault=="reserved" && offset==56)*value|=16;
        if(f.fault=="pending" && offset==60)*value|=8;
        if(f.fault=="queue" && offset==60)*value=5;
        if(f.fault=="unstable" && offset==0xb0)*value^=(f.reads/22)&1;
        if(f.fault=="draining" && offset==56)*value=2;
        if(f.fault=="stale-ack" && offset==72)*value=1;
        if(f.fault=="request" && offset==36)*value=2;
        if(f.fault=="late-generation" && f.writes && offset==28)*value=0;
        if(f.fault=="wrong-cookie" && f.writes && offset==72 && *value)*value=8;
        if(f.fault=="premature" && f.writes && offset==56)*value=4;
        if(f.fault=="late-restart" && f.probe_reads && offset==56)*value=1;
        if(f.fault=="owner-after-read" && f.reads==1)f.owned=false;
        return 0;
    }
    static int write(void *context,uint32_t offset,uint32_t value) {
        auto& f=*static_cast<Fixture*>(context);f.writes++;
        require((offset==44 && value==(uint32_t)f.expected_stop_cookie) ||
            (offset==48 && value==(uint32_t)(f.expected_stop_cookie>>32)) ||
            (offset==52 && value==2),"stop enabled or changed reader");
        if(f.fault=="write-before")return -7;
        f.csr_write(offset,value);
        if(f.fault=="owner-after-write")f.owned=false;
        return f.fault=="write-after"?-7:f.fault=="write-positive"?7:0;
    }
    static int probe_read(void *context,uint32_t offset,uint32_t *value) {
        auto& f=*static_cast<Fixture*>(context);f.probe_reads++;
        *value=f.p.read(offset);
        if(f.fault=="probe-identity" && offset==0)*value^=1;
        if(f.fault=="probe-owner" && f.probe_reads==1)f.owned=false;
        return f.fault=="probe-read"?-9:0;
    }
    static int probe_write(void *context,uint32_t offset,uint32_t value) {
        auto& f=*static_cast<Fixture*>(context);f.cancels++;
        require(offset==12 && value==2 && f.cancels==1,"probe command replay");
        f.p.write(offset,value);return f.fault=="probe-write"?-9:0;
    }
    static void wait(void *context) {
        auto& f=*static_cast<Fixture*>(context);f.waits++;
        if(f.waits>=2 && f.fault!="held")f.stall=false;
        if(f.waits>=4 && f.fault!="no-response")f.responses=true;
        f.tick();
        if(f.stop.probe.cancel_issued && f.fault!="probe-held")f.p.stall=false;
        if(f.stop.probe.cancel_issued && f.fault!="probe-no-response")f.p.responses=true;
        f.p.tick();
    }
    void retired() {
        require(stop.failed && !stop.done && stop.first_error<0,"failed stop retained receipt");
        auto before=std::array<unsigned,5>{reads,probe_reads,writes,cancels,waits};int error=stop.first_error;
        require(fes_nano_fabric_stop(&stop)==error && fes_nano_fabric_stopped(&stop)==error,"first error/replay");
        require(before==std::array<unsigned,5>{reads,probe_reads,writes,cancels,waits},"retired stop performed I/O");
        require(writes<=3 && cancels<=1,"command replay");
    }
};

#include "nano_ddr_read_test.inc"

#ifndef FES_WINDOW_STOP_TEST
int main() {
    try {
        unsigned cases=0,points=0;
        for(bool active:{false,true})for(bool accepted:{false,true}) {
            Fixture f("",active);
            if(accepted) { f.stall=false;f.tick();f.tick();f.p.stall=false;f.p.idle(3); }
            require(!fes_nano_fabric_stop(&f.stop) && f.stop.done,"complete stop failed");
            require(f.writes==(active?3U:0U) && f.cancels==(active?1U:0U),"idle mutated or active not stopped");
            require(f.pending.empty() && !f.d.ddr_arvalid && f.p.pending.empty() && !f.p.dut.avm_read,"traffic escaped receipt");
            if(active && !accepted)points=f.reads;
            unsigned writes=f.writes,cancels=f.cancels;
            require(!fes_nano_fabric_stopped(&f.stop) && !fes_nano_fabric_stop(&f.stop) &&
                    writes==f.writes && cancels==f.cancels,"readback replayed stop");
            f.fault="maintenance";require(fes_nano_fabric_stopped(&f.stop)<0,"late maintenance release accepted");f.retired();cases++;
        }
        Fixture drained;
        require(!fes_nano_fabric_stop(&drained.stop),"setup drain failed");
        auto saved=drained.stop;
        drained.stop={};drained.stop.io=saved.io;drained.stop.probe.io=saved.probe.io;
        drained.stop.abi=saved.abi;std::copy(std::begin(saved.build),std::end(saved.build),drained.stop.build);
        drained.stop.bus_base=saved.bus_base;drained.stop.bytes=saved.bytes;drained.stop.cookie=7;drained.stop.qualified=1;
        unsigned writes=drained.writes,cancels=drained.cancels;
        require(!fes_nano_fabric_stop(&drained.stop) && writes==drained.writes && cancels==drained.cancels,"already drained mutated");cases++;
        for(const std::string fault:{"scope","scope-two","identity","version","abi","build","base","span","maintenance", "reserved",
            "pending","queue","unstable","draining","stale-ack","request","late-generation","wrong-cookie","premature",
            "owner-after-read","owner-after-write","read-positive","write-before","write-after","write-positive","late-restart",
            "held","no-response","response","probe-identity","probe-owner","probe-read","probe-write","probe-held","probe-no-response"}) {
            Fixture f(fault);require(fes_nano_fabric_stop(&f.stop)<0,"accepted invalid stop: "+fault);f.retired();cases++;
        }
        for(unsigned point=1;point<=points;point++) {
            Fixture f;f.fail_read=point;
            require(fes_nano_fabric_stop(&f.stop)==-7 && f.reads==point,"lost read failure");f.retired();cases++;
        }
        for(unsigned missing=0;missing<7;missing++) {
            Fixture f;
            switch(missing) {
            case 0:f.stop.qualified=0;break;case 1:f.stop.cookie=0;break;case 2:f.stop.io.owns=nullptr;break;
            case 3:f.stop.io.read32=nullptr;break;case 4:f.stop.io.write32=nullptr;break;
            case 5:f.stop.io.wait=nullptr;break;case 6:f.stop.probe.io.write32=nullptr;break;
            }
            require(fes_nano_fabric_stop(&f.stop)==-22 && !f.reads && !f.probe_reads && !f.writes,"unqualified callback touched hardware");f.retired();cases++;
        }
        auto ddr=ddr_read_cases();
        std::cout<<"{\"status\":\"passed\",\"cases\":"<<cases<<",\"read_failure_points\":"<<points
                 <<",\"ddr_read_cases\":"<<ddr[0]<<",\"ddr_read_failure_points\":"<<ddr[1]
                 <<",\"hardware_access\":false,\"bridge_authority\":false,\"memory_released\":false}\n";
        return 0;
    } catch(const std::exception& e) {std::cerr<<"fabric stop: "<<e.what()<<"\n";return 1;}
}
#endif
