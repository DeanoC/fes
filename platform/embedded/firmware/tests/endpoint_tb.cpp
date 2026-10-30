// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vfes_embedded_control.h"
#include "contained_path.hpp"
#include "tfa_stubs.h"
#include <deque>
#include <iostream>
#include <stdexcept>
#include <string>
using namespace fes::embedded;
static void check(bool b,const char* s){if(!b)throw std::runtime_error(s);}
struct World {
    bool held=false, control=true, disable_error=false, cancel_error=false, short_reply=false, async_error=false, bad_profile=false;
    bool native_rpc=false;
    unsigned starts=0, data_enables=0, control_enables=0, bytes=0, completed=0;
    bool seen[16]{};
    std::deque<unsigned> responses;
} world;
extern "C" int socfpga_bridges_disable(uint32_t){if(world.disable_error)return -1;world.held=true;world.control=false;return 0;}
extern "C" int socfpga_bridges_enable(uint32_t mask){
    if(mask==2){check(world.held,"control probe lost data containment");world.control=true;++world.control_enables;}
    else{world.held=false;world.control=true;++world.data_enables;}return 0;
}
extern "C" uint32_t mmio_read_32(uintptr_t){return 0;}
extern "C" void mmio_write_32(uintptr_t address,uint32_t value){check((address==2||address==3)&&value==DRAM_BASE,"unexpected remapper write");}
extern "C" void mailbox_clear_response(void){}
extern "C" int mailbox_send_cmd(int,int command,const uint32_t*,unsigned,int,uint32_t* response,unsigned* count){
    check(world.held,"CANCEL/RECONFIG before containment");
    if(command==MBOX_CMD_CANCEL)return world.cancel_error?-1:0;
    check(command==MBOX_RECONFIG,"unexpected mailbox command");
    response[0]=2;response[1]=world.native_rpc?4096:8;*count=world.short_reply?1:2;return 0;
}
extern "C" int mailbox_send_cmd_async(uint32_t*,int command,const uint32_t* a,unsigned count,int){
    check(world.held&&command==MBOX_RECONFIG_DATA&&count==3,"unsafe data submission");
    unsigned index=0;
    if(world.native_rpc){
        check(a[1]==0x16000000U+world.bytes&&a[2]>0&&a[2]<=4096,"wrong or replayed native subblock");
        world.bytes+=a[2];
    }else{
        check(a[1]>=0x16000000&&a[1]<0x16000080&&a[2]==8,"wrong SDM-relative subblock");
        index=(a[1]-0x16000000)/8;
        check(!world.seen[index],"accepted-BUSY buffer replayed");world.seen[index]=true;world.bytes+=8;
    }
    if(!world.async_error)world.responses.push_back(index);
    return world.async_error?-1:0;
}
extern "C" int mailbox_read_response(uint32_t*,uint32_t*,unsigned* n){
    *n=0;if(world.responses.empty())return MBOX_NO_RESPONSE;
    world.responses.pop_front();return 0;
}
extern "C" uint32_t intel_mailbox_get_config_status(int,bool){check(world.held,"status outside containment");return 0;}

struct Bench {
    Vfes_embedded_control d;
    Control control;
    Bench():control(IO{this,read,write}) {world=World{};fes_tfa_reset();reset();}
    void tick(){d.clk=0;d.eval();d.clk=1;d.eval();d.clk=0;d.eval();}
    void reset(){
        d.reset_n=0;d.csr_valid=0;d.csr_write=0;d.csr_address=0;d.csr_wdata=0;d.csr_wstrb=15;d.csr_response_ready=1;
        d.reader_valid=0;d.reader_address=0;d.reader_length=0;d.reader_rready=1;
        d.m_arready=0;d.m_rvalid=0;d.m_rlast=0;d.m_rresp=0;d.m_rid=0;
        for(unsigned i=0;i<4;i++)d.m_rdata[i]=0;
        tick();tick();d.reset_n=1;tick();
    }
    bool access(bool w,uint32_t o,uint32_t& value){
        if(!world.control)return false;
        d.csr_valid=1;d.csr_write=w;d.csr_address=o;d.csr_wdata=value;d.csr_wstrb=15;d.eval();
        check(d.csr_ready,"CSR was not ready");tick();
        check(d.csr_response_valid,"CSR response absent");
        bool ok=!d.csr_error;value=d.csr_rdata;d.csr_valid=0;tick();return ok;
    }
    static bool read(void* p,uint32_t o,uint32_t* v){*v=0;return static_cast<Bench*>(p)->access(false,o,*v);}
    static bool write(void* p,uint32_t o,uint32_t v){return static_cast<Bench*>(p)->access(true,o,v);}
    uint32_t reg(uint32_t o){uint32_t v=0;check(read(this,o,&v),"CSR read failed");return v;}
    void offer(uint32_t address,unsigned len,bool accept=true){
        d.reader_valid=1;d.reader_address=address;d.reader_length=len;d.eval();
        check(d.reader_ready,"reader not ready");tick();d.reader_valid=0;
        d.m_arready=accept;tick();d.m_arready=0;d.eval();
    }
    void beat(bool last,unsigned response=0,unsigned id=0){
        d.m_rvalid=1;d.m_rlast=last;d.m_rresp=response;d.m_rid=id;d.eval();
        check(d.m_rready,"memory response not consumed");tick();d.m_rvalid=0;tick();
    }
    static bool contain(void*){return socfpga_bridges_disable(~0U)==0;}
    static bool contract(void*){uint64_t r[4]{};fes_tfa_query(r);return !world.bad_profile&&r[0]==0&&r[1]==FesEmbeddedFirmwareMagic&&r[2]==FesEmbeddedFirmwareVersion&&r[3]==FesEmbeddedFirmwareFeatures;}
    static Result start(void*){
        ++world.starts;
        if(fes_tfa_start(0)!=0)return Result::fault;
        for(unsigned i=0;i<4;i++){
            int status=fes_tfa_write(0x96000000U+i*32,32);
            check(status==(i==3?1:0),"unexpected actual TF-A WRITE status");
        }
        return Result::ok;
    }
    static Result poll(void* p){
        uint32_t addresses[3]{};int status=fes_tfa_completed(addresses);
        check(status==0||status==1,"actual TF-A completion failed");
        for(auto addr:addresses)if(addr){check(addr==0x16000000U+world.completed*32,"wrong completion namespace/order");++world.completed;}
        if(world.completed!=4)return Result::busy;
        if(fes_tfa_isdone()!=0)return Result::fault;
        check(world.data_enables==0,"ISDONE automatically released bridges");
        static_cast<Bench*>(p)->reset();return Result::ok;
    }
    static bool control_only(void*){return socfpga_bridges_enable(2)==0;}
    static bool release(void*){return socfpga_bridges_enable(~0U)==0;}
    ProgramOps ops(){return {this,contract,contain,start,poll,control_only,release};}
};
const Identity identity{1,{0xccddeeff,0x8899aabb,0x44556677,0x00112233}};

static void held_address_and_reload(){
    Bench b;check(b.control.enable(5)==Result::ok,"enable");
    b.offer(0x1000,3,false);
    ContainedPath path(b.control,b.ops());
    check(path.release(0)==Result::invalid,"early release accepted");
    check(path.begin(identity,5,7,identity,0,1000)==Result::ok,"begin");
    for(unsigned i=1;i<4;i++){
        check(path.poll(i)==Result::busy,"held AR falsely drained");
        check(b.d.m_arvalid&&b.d.m_araddr==0x1000&&b.d.m_arlen==3,"AR changed on disable");
        check(world.starts==0,"program began before AR acceptance/drain");
    }
    b.d.m_arready=1;b.tick();b.d.m_arready=0;b.d.reader_rready=0;
    for(unsigned i=0;i<3;i++){b.beat(false);check(path.poll(5+i)==Result::busy,"partial burst drained");}
    b.beat(true);
    check(path.poll(10)==Result::busy&&world.starts==1,"program did not start after drain");
    for(unsigned now=11;path.phase()==Phase::programming&&now<100;now++)path.poll(now);
    check(path.phase()==Phase::verified&&world.held&&world.data_enables==0,"verified state lost containment");
    check(world.bytes==128&&world.completed==4&&world.control_enables==1,"actual firmware buffers lost/replayed");
    check(path.release(100)==Result::ok&&world.data_enables==1,"explicit verified release failed");
    check(path.release(101)==Result::invalid&&world.data_enables==1,"duplicate release accepted");
}
static void faults_and_stale(){
    for(unsigned fault=0;fault<5;fault++){
        Bench b;check(b.control.enable(10)==Result::ok,"enable fault test");b.offer(0x2000,fault==4?0:1);
        check(b.control.disable(identity,10,9)==Result::ok,"disable fault test");
        if(fault==0)b.beat(true); // Early RLAST for a two-beat request.
        if(fault==1)b.beat(false,2);
        if(fault==2)b.beat(false,0,1);
        if(fault==3){b.d.reset_n=0;b.tick();b.d.reset_n=1;b.tick();}
        if(fault==4)b.beat(false); // Missing terminal RLAST.
        check(b.control.drained(identity,10,9)!=Result::ok,"fault/reset claimed old drain");
        if(fault!=3)check(b.control.enable(11)!=Result::ok,"sticky read fault cleared by enable");
    }
    Bench b;check(b.control.enable(10)==Result::ok,"enable cookie test");
    check(b.control.disable(identity,9,1)==Result::identity,"wrong generation accepted");
    check(b.control.disable(identity,10,3)==Result::ok,"disable cookie test");
    check(b.control.drained(identity,10,2)!=Result::ok,"stale cookie accepted");
    check(b.control.drained(identity,10,3)==Result::ok,"matching drain absent");
    check(b.control.enable(11)==Result::ok,"reopen with fresh generation failed");
    check(b.control.disable(identity,11,3)==Result::io,"cookie reuse accepted");
}
static void contained_failures(){
    for(unsigned failure=0;failure<6;failure++){
        Bench b;check(b.control.enable(1)==Result::ok,"enable failure test");
        Identity expected=identity;if(failure==0)expected.build[3]^=1;
        ContainedPath path(b.control,b.ops());
        check(path.begin(identity,1,1,expected,0,100)==Result::ok,"begin failure test");
        if(failure==1)world.disable_error=true;
        if(failure==2)world.cancel_error=true;
        if(failure==3)world.short_reply=true;
        if(failure==4)world.async_error=true;
        if(failure==5){check(path.poll(100)==Result::deadline,"deadline ignored");}
        else for(unsigned now=1;path.phase()!=Phase::failed&&now<=100;now++)path.poll(now);
        check(path.phase()==Phase::failed&&world.data_enables==0,"failed path released data bridge");
        unsigned starts=world.starts;
        check(path.poll(101)==Result::invalid&&path.release(101)==Result::invalid&&world.starts==starts,"failed path retried/released");
    }
    Bench b;
    check(fes_tfa_start(1)==2&&world.control,"partial configuration bypass accepted");
    check(fes_tfa_start(2)==2&&world.control,"authentication bypass accepted");
    check(fes_tfa_translate(0x96000000)==0x16000000,"remap translation changed");
}
static void queue_and_csr(){
    Bench b;check(b.control.enable(1)==Result::ok,"enable queue test");
    for(unsigned i=0;i<4;i++)b.offer(0x3000+i*64,0);
    check(!b.d.reader_ready,"queue allowed fifth outstanding request");
    check(b.control.disable(identity,1,1)==Result::ok,"disable full queue");
    for(unsigned i=0;i<4;i++)b.beat(true);
    check(b.control.drained(identity,1,1)==Result::ok,"four outstanding bursts did not drain");
    b.d.csr_response_ready=0;b.d.csr_valid=1;b.d.csr_write=0;b.d.csr_address=FesEmbeddedRegMagic;b.tick();
    auto value=b.d.csr_rdata;
    b.d.csr_address=FesEmbeddedRegVersion;
    for(unsigned i=0;i<3;i++){b.tick();check(!b.d.csr_ready&&b.d.csr_response_valid&&b.d.csr_rdata==value,"CSR response changed under backpressure");}
    b.d.csr_valid=0;b.d.csr_response_ready=1;b.tick();
    uint32_t zero=0;check(!b.access(true,FesEmbeddedRegBuild0,zero),"identity was writable");
}
static void forwarding_and_admission(){
    Bench b;check(b.control.enable(1)==Result::ok,"forward enable");b.offer(0x4000,0);
    b.d.reader_rready=0;b.d.m_rvalid=1;b.d.m_rlast=1;b.d.m_rdata[0]=0x12345678;b.d.eval();
    for(unsigned i=0;i<3;i++){check(b.d.reader_rvalid&&!b.d.m_rready&&b.d.reader_rdata[0]==0x12345678,"reader backpressure/data changed");b.tick();}
    b.d.reader_rready=1;b.tick();b.d.m_rvalid=0;b.tick();
    check(b.reg(FesEmbeddedRegStatus)==FesEmbeddedStatusEnabled&&b.reg(FesEmbeddedRegPending)==0,"forwarding lost burst accounting");
    world.bad_profile=true;ContainedPath path(b.control,b.ops());
    check(path.begin(identity,1,1,identity,0,100)==Result::identity&&world.starts==0&&b.reg(FesEmbeddedRegStatus)==FesEmbeddedStatusEnabled,"unknown firmware mutated old display");
    world.bad_profile=false;
    // A new AR and its zero-latency response may appear together. Do not
    // consume the response until the accepted request is accounted for.
    b.offer(0x5000,0,false);b.d.m_arready=1;b.d.m_rvalid=1;b.d.m_rlast=1;b.tick();
    b.d.m_arready=0;b.tick();b.d.m_rvalid=0;b.tick();
    check(b.reg(FesEmbeddedRegStatus)==FesEmbeddedStatusEnabled&&b.reg(FesEmbeddedRegPending)==0,"simultaneous AR/response accounting failed");
    check(b.control.disable(identity,1,1)==Result::ok&&b.control.drained(identity,1,1)==Result::ok,"forwarded reader failed to drain");
    b.d.m_rvalid=1;b.d.m_rlast=1;b.tick();b.d.m_rvalid=0;b.tick();
    check(b.control.drained(identity,1,1)==Result::fault,"unsolicited late response left a valid drain acknowledgement");
}
static void simultaneous_and_maximum_burst(){
    Bench b;check(b.control.enable(1)==Result::ok,"simultaneous enable");
    b.offer(0x7000,0);b.offer(0x8000,0,false);
    b.d.m_arready=1;b.d.m_rvalid=1;b.d.m_rlast=1;b.tick();
    b.d.m_arready=0;b.d.m_rvalid=0;b.tick();
    check(b.reg(FesEmbeddedRegPending)==1,"simultaneous AR/RLAST lost outstanding request");
    check(b.control.disable(identity,1,1)==Result::ok&&b.control.drained(identity,1,1)==Result::busy,"simultaneous traffic falsely drained");
    b.beat(true);check(b.control.drained(identity,1,1)==Result::ok,"simultaneous final response failed");
    check(b.control.enable(2)==Result::ok,"maximum-burst enable");b.offer(0x9000,255);
    check(b.control.disable(identity,2,2)==Result::ok,"maximum-burst disable");
    for(unsigned i=0;i<255;i++)b.beat(false);
    check(b.control.drained(identity,2,2)==Result::busy,"256-beat burst drained early");
    b.beat(true);check(b.control.drained(identity,2,2)==Result::ok,"256-beat burst failed to drain");
}
int main(){
    try{
        held_address_and_reload();std::cout<<"PASS actual-TFA-contained-reload\n";
        faults_and_stale();std::cout<<"PASS AXI-fault-reset-generation-cookie\n";
        contained_failures();std::cout<<"PASS identity-firmware-deadline-failures\n";
        queue_and_csr();std::cout<<"PASS outstanding-queue-and-CSR-backpressure\n";
        forwarding_and_admission();std::cout<<"PASS forwarding-profile-and-late-response\n";
        simultaneous_and_maximum_burst();std::cout<<"PASS simultaneous-AR-RLAST-and-256-beat-burst\n";
    }catch(const std::exception& e){std::cerr<<"FAIL "<<e.what()<<"\n";return 1;}
    return 0;
}
