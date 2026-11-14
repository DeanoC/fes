// SPDX-License-Identifier: GPL-3.0-or-later
// Use the existing probe bench's real RTL and independent Avalon bus model.
#define main original_probe_cases
#include "probe_tb.cpp"
#undef main
#include "nano_probe_stop.h"

struct Fixture {
    Probe p;
    struct fes_nano_probe_stop stop{};
    std::string fault;
    unsigned reads=0, writes=0, waits=0, fail_read=0;
    bool owned=true;
    explicit Fixture(std::string f="", bool active=true):fault(std::move(f)) {
        stop.io={this,owns,read,write,wait};
        if(active) p.start(Descriptor{});
    }
    static int owns(void *context) {
        auto& f=*static_cast<Fixture*>(context);
        return !f.owned || f.fault=="scope" ? 0 : f.fault=="scope-two" ? 2 : 1;
    }
    static int read(void *context,uint32_t offset,uint32_t *value) {
        auto& f=*static_cast<Fixture*>(context);
        if(++f.reads==f.fail_read)return -5;
        *value=f.p.read(offset);
        if(f.fault=="read-positive")return 1;
        if(f.fault=="identity" && offset==r::FesDe25DdrProbeIdOffset)*value^=1;
        if(f.fault=="version" && offset==r::FesDe25DdrProbeVersionOffset)*value^=1;
        if(f.fault=="fault" && offset==r::FesDe25DdrProbeStatusOffset)*value|=8;
        if(f.fault=="status" && offset==r::FesDe25DdrProbeStatusOffset)*value=0;
        if(f.fault=="reserved-status" && offset==r::FesDe25DdrProbeStatusOffset)*value|=32;
        if(f.fault=="undrained-idle" && offset==r::FesDe25DdrProbeAcceptedBytesOffset)*value+=16;
        if(f.fault=="unstable" && offset==r::FesDe25DdrProbeAcceptedBytesOffset)*value=f.reads&1 ? 16 : 32;
        if(f.fault=="reset" && f.writes && offset==r::FesDe25DdrProbeActiveSequenceOffset)*value=0;
        if(f.fault=="owner-after-read" && f.reads==1)f.owned=false;
        return 0;
    }
    static int write(void *context,uint32_t offset,uint32_t value) {
        auto& f=*static_cast<Fixture*>(context);
        require(offset==r::FesDe25DdrProbeCommandOffset && value==r::FesDe25DdrProbeCommandCancel,
                "stop callback issued a command other than CANCEL");
        ++f.writes;f.p.write(offset,value);
        if(f.fault=="owner-after-write")f.owned=false;
        return f.fault=="write" ? -5 : 0;
    }
    static void wait(void *context) {
        auto& f=*static_cast<Fixture*>(context);
        ++f.waits;
        if(f.waits>=2 && f.fault!="held-forever")f.p.stall=false;
        if(f.waits>=4 && f.fault!="no-response")f.p.responses=true;
        f.p.tick();
    }
    void retired() {
        require(stop.failed && !stop.done,"failed callback retained a stop receipt");
        auto before=std::array<unsigned,3>{reads,writes,waits};
        require(fes_nano_probe_stop(&stop)<0,"failed callback replayed");
        require(before==std::array<unsigned,3>{reads,writes,waits},"retired callback contacted hardware");
        require(writes<=1,"CANCEL was replayed");
    }
};

int main() {
    try {
        unsigned cases=0, failure_points=0;
        for(bool completed:{false,true}) {
            Fixture f("",false);
            if(completed) {f.p.stall=false;f.p.responses=true;f.p.start(Descriptor{});f.p.wait_idle();}
            require(fes_nano_probe_stop(&f.stop)==0 && f.stop.done && !f.writes,"idle probe was mutated");
            require(fes_nano_probe_stop(&f.stop)==0 && !f.writes,"completed same-owner receipt was not observed");
            f.fault="fault";
            require(fes_nano_probe_stop(&f.stop)<0,"changed receipt was reused");f.retired();++cases;
        }
        for(bool accepted:{false,true}) {
            Fixture f;
            if(accepted) {f.p.stall=false;f.p.idle(3);}
            require(fes_nano_probe_stop(&f.stop)==0 && f.stop.done && f.writes==1,"active probe did not drain");
            require(f.stop.last.accepted==f.stop.last.completed && f.stop.last.accepted>0 &&
                    f.p.pending.empty() && !f.p.dut.avm_read,"stop lost accepted or held traffic");
            if(!accepted)failure_points=f.reads;
            ++cases;
        }
        for(const std::string fault:{"scope","scope-two","identity","version","fault","status","reserved-status",
                                    "read-positive","owner-after-read","owner-after-write","write","reset",
                                    "held-forever","no-response"}) {
            Fixture f(fault);
            require(fes_nano_probe_stop(&f.stop)<0,"accepted invalid stop: "+fault);f.retired();++cases;
            if(fault=="held-forever" || fault=="no-response")
                require(f.stop.first_error==-110 && f.stop.polls==128 && f.writes==1,"timeout fabricated drain or retried CANCEL");
        }
        for(const std::string fault:{"undrained-idle","unstable"}) {
            Fixture f(fault,false);
            require(fes_nano_probe_stop(&f.stop)<0,"accepted inconsistent idle state");f.retired();++cases;
        }
        for(unsigned at=1;at<=failure_points;at++) {
            Fixture f;f.fail_read=at;
            require(fes_nano_probe_stop(&f.stop)==-5 && f.reads==at,"read failure was lost");f.retired();++cases;
        }
        Fixture error;
        error.p.error_response=0;
        require(fes_nano_probe_stop(&error.stop)<0,"actual Avalon response fault accepted");error.retired();++cases;
        std::cout<<"{\"status\":\"passed\",\"cases\":"<<cases<<",\"read_failure_points\":"<<failure_points
                 <<",\"hardware_access\":false,\"bridge_authority\":false}\n";
        return 0;
    } catch(const std::exception& e) {std::cerr<<"probe stop: "<<e.what()<<"\n";return 1;}
}
