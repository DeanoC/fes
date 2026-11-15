// SPDX-License-Identifier: BSD-3-Clause
// Actual secure probe and bridge controller against the actual Avalon RTL.
#include "Vfes_de25_embedded.h"
#include "nano_identity.h"
#include <cassert>
#include <cstdio>
#include <stdexcept>
#include <cstdlib>
#include <utility>
#include <vector>

struct Bench {
    Vfes_de25_embedded d;
    fes_nano_bridges bridges{};
    fes_nano_identity_probe probe{};
    uint32_t reset=15;
    bool owner=true;
    unsigned reads=0,writes=0,enables=0,disables=0,contains=0;
    unsigned fail_read=0,fail_write=0,late_write=0,lose_write=0;
    unsigned fault=0,ready=1;
    std::vector<std::pair<uint32_t,uint32_t>> commands;
    static constexpr uint32_t build[4]={0xccddeeff,0x8899aabb,0x44556677,0x00112233};
    Bench() {
        d.reset_n=0;d.address=0;d.read=0;d.write=0;d.byteenable=15;d.writedata=0;
        tick();tick();d.reset_n=1;tick();
        fes_nano_bridge_ops ops{this,owns,read_reset,masters,stop,disable,enable};
        assert(!fes_nano_bridges_init(&bridges,ops,17,7,FES_NANO_FABRIC_ABSENT,1));
        fes_nano_identity_ops io{};
        io.context=this;io.configured=configured;io.read32=read32;
        io.contain=contain;io.write32=write32;
        assert(!fes_nano_identity_init(&probe,&bridges,io,0x10203040,build,1));
    }
    void tick() { d.clk=0;d.eval();d.clk=1;d.eval();d.clk=0;d.eval(); }
    int access(bool writing,uint32_t offset,uint32_t &value) {
        assert(offset<256 && offset%4==0 && reset==FES_NANO_BRIDGES_DATA);
        d.address=offset;d.read=!writing;d.write=writing;d.byteenable=15;d.writedata=value;d.eval();
        unsigned n=0;
        while(d.waitrequest && n++<16)tick();
        assert(!d.waitrequest);
        tick();d.read=0;d.write=0;n=0;
        while(!(writing?d.writeresponsevalid:d.readdatavalid) && n++<16)tick();
        assert(writing?d.writeresponsevalid:d.readdatavalid);
        int error=d.response?-5:0;value=d.readdata;tick();return error;
    }
    static int owns(void *p,uint64_t owner,uint64_t epoch) {
        return static_cast<Bench*>(p)->owner && owner==17 && epoch==7;
    }
    static int read_reset(void *p,uint32_t *v) { *v=static_cast<Bench*>(p)->reset;return 0; }
    static int masters(void *p) { return (static_cast<Bench*>(p)->reset&13)==13; }
    static int stop(void *p) { return masters(p)?0:-1; }
    static int enable(void *p,uint32_t mask,unsigned fabric) {
        auto &b=*static_cast<Bench*>(p);assert(mask==2 && fabric==FES_NANO_FABRIC_ABSENT);
        b.enables++;b.reset&=~mask;return 0;
    }
    static int disable(void *p,uint32_t mask,unsigned fabric) {
        auto &b=*static_cast<Bench*>(p);assert(mask==2 && fabric==FES_NANO_FABRIC_ABSENT);
        b.disables++;b.reset|=mask;return 0;
    }
    static int contain(void *p) {
        auto &b=*static_cast<Bench*>(p);b.contains++;b.reset|=2;b.owner=false;
        return b.reset==15?0:-5;
    }
    static int configured(void *p) { return static_cast<Bench*>(p)->ready; }
    static int read32(void *p,uint32_t offset,uint32_t *value) {
        auto &b=*static_cast<Bench*>(p);b.reads++;
        if(b.reads==b.fail_read)return -5;
        int status=b.access(false,offset,*value);
        if(b.fault==1 && b.writes>=6 && b.writes<9 && offset==FES_EMBEDDED_REG_ACK_COOKIE_LO)*value=7;
        if(b.fault==2 && b.writes>=9 && b.writes<12 && offset==FES_EMBEDDED_REG_STATUS)*value=FES_EMBEDDED_STATUS_DRAINED;
        if(b.fault==3 && b.writes==12 && offset==FES_EMBEDDED_REG_STATUS)*value=FES_EMBEDDED_STATUS_DRAINING;
        if(b.fault==4 && b.writes==12 && offset==FES_EMBEDDED_REG_ACK_COOKIE_LO)*value=8;
        if(b.fault==6 && offset==0x90)*value^=0x1000;
        if(b.fault==7 && !b.writes && offset==FES_EMBEDDED_REG_STATUS)*value=1;
        if(b.fault==8 && b.reads==30)b.owner=false;
        if(b.fault==9 && b.writes==12 && offset==FES_EMBEDDED_REG_ABI)*value^=1;
        if(b.fault==10 && b.writes>=6 && offset==FES_EMBEDDED_REG_PENDING)*value=1;
        if(b.fault==11 && b.reads==31)b.bridges.state.generation++;
        return status;
    }
    static int write32(void *p,uint32_t offset,uint32_t value) {
        auto &b=*static_cast<Bench*>(p);b.writes++;
        if(b.writes==b.fail_write)return -5;
        b.commands.emplace_back(offset,value);
        if(b.fault==5 && b.writes==11)value|=0x10;
        int status=b.access(true,offset,value);
        if(b.writes==b.lose_write)b.owner=false;
        if(b.fault==12 && b.writes==1)b.reset&=~4U;
        return b.writes==b.late_write?-5:status;
    }
    void call(uint64_t r[4],uint64_t generation=1) { fes_nano_reader_sip(&probe,17,generation,0,r); }
    void stopped(const uint64_t r[4]) {
        assert(r[0]==4 && !r[2] && !r[3]);
        assert(probe.failed && bridges.failed && !probe.active && probe.first_error<0);
        assert(contains==1 && (reset&2)==2);
        unsigned before_reads=reads,before_writes=writes;uint64_t again[4];
        call(again,bridges.state.generation);
        assert(again[0]==4 && !again[2] && !again[3] && before_reads==reads && before_writes==writes);
    }
};

#ifdef FES_TEST_STRONG_READER
static Bench *strong_model;
static unsigned strong_writes,strong_mutation;
extern "C" int fes_strong_reader_main(int,char **);
extern "C" uint32_t fes_test_reader_read(uint32_t offset) {
    uint32_t value=0;assert(!strong_model->access(false,offset,value));
    if(strong_mutation==75 && offset==0x90)value^=0x1000;
    if(strong_mutation==76 && strong_writes>=6 && strong_writes<9 && offset==FES_EMBEDDED_REG_ACK_COOKIE_LO)value=7;
    if(strong_mutation==79 && strong_writes==12 && offset==FES_EMBEDDED_REG_STATUS)value=FES_EMBEDDED_STATUS_DRAINING;
    return value;
}
extern "C" void fes_test_reader_write(uint32_t offset,uint32_t value) {
    strong_writes++;
    if(strong_mutation==77 && strong_writes==11)value|=0x10;
    assert(!strong_model->access(true,offset,value));
}
#endif

int main(int argc,char **argv) {
#ifdef FES_TEST_STRONG_READER
    if(argc==2) {
        Bench model;strong_model=&model;model.reset=13;
        strong_mutation=static_cast<unsigned>(std::strtoul(argv[1],nullptr,10));
        return fes_strong_reader_main(argc,argv);
    }
#else
    (void)argc;(void)argv;
#endif
    unsigned cases=0,success_reads=0;
    for(uint64_t generation:{1ULL,4ULL}) {
        Bench b;uint64_t r[4];b.bridges.state.generation=generation;b.call(r,generation);
        assert(!r[0] && r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_READER_TAG<<32|0x10203040U));
        assert(r[2]==0x0011223344556677ULL && r[3]==0x8899aabbccddeeffULL);
        assert(b.bridges.state.generation==generation+2 && b.reset==15 && b.probe.reader);
        assert(!b.probe.failed && !b.probe.active && b.probe.stage==FES_ID_COMPLETE && !b.contains);
        assert(b.writes==12 && b.enables==1 && b.disables==1 && b.reads>60 && b.reads<200);
        success_reads=b.reads;
        std::vector<uint32_t> offered;
        for(auto command:b.commands)if(command.first==0x80)offered.push_back(command.second);
        assert((offered==std::vector<uint32_t>{0x0c,0x05,0x07,0x07}));
        unsigned before=b.reads;fes_nano_identity_sip(&b.probe,17,generation+2,0,r);
        assert(r[0]==2 && b.reads==before && b.writes==12);cases++;
    }
    for(unsigned fail=1;fail<=success_reads;fail++) {
        Bench b;uint64_t r[4];b.fail_read=fail;b.call(r);b.stopped(r);
        assert(b.probe.first_error==-5);cases++;
    }
    for(unsigned fail=1;fail<=12;fail++) {
        for(bool late:{false,true}) {
            Bench b;uint64_t r[4];if(late)b.late_write=fail;else b.fail_write=fail;
            b.call(r);b.stopped(r);assert(b.writes==fail && b.probe.first_error==-5);cases++;
        }
        Bench b;uint64_t r[4];b.lose_write=fail;b.call(r);b.stopped(r);assert(b.writes==fail);cases++;
    }
    for(unsigned fault=1;fault<=12;fault++) {
        Bench b;uint64_t r[4];b.fault=fault;b.call(r);b.stopped(r);
        if(fault==1 || fault==6 || fault==7 || fault==8)assert(b.writes<=6);
        if(fault==3 || fault==4 || fault==10)assert(b.probe.first_error==-110);
        if(fault==12)assert(b.probe.cleanup_error<0);
        assert(b.reads<2000);cases++;
    }
    {
        Bench b;uint64_t r[4];b.probe.io.write32=nullptr;b.call(r);
        assert(r[0]==5 && !b.probe.attempted && !b.reads && !b.writes && !b.enables);
        fes_nano_identity_sip(&b.probe,17,1,0,r);
        assert(!r[0] && b.reads==30 && !b.writes);cases++;
    }
    for(unsigned invalid=0;invalid<5;invalid++) {
        Bench b;uint64_t r[4];
        if(invalid==0)fes_nano_reader_sip(&b.probe,18,1,0,r);
        if(invalid==1)fes_nano_reader_sip(&b.probe,17,2,0,r);
        if(invalid==2)fes_nano_reader_sip(&b.probe,17,1,1,r);
        if(invalid==3){b.bridges.device_lease=1;b.call(r);}
        if(invalid==4){b.bridges.state.generation=UINT64_MAX-1;b.call(r,UINT64_MAX-1);}
        assert(r[0] && !r[2] && !r[3] && !b.probe.attempted && !b.reads && !b.writes && !b.enables);cases++;
    }
    std::printf("PASS %u secure reader/actual Avalon RTL cases; %u successful reads; 12 one-shot writes\n",cases,success_reads);
}
