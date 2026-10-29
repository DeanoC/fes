// SPDX-License-Identifier: GPL-3.0-or-later
#include "wire.hpp"
#include "native/de25_computer.hpp"
#include "native/de25_pr_control.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/generated/fes_de25_pr_control.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include <cstring>
#include <iostream>
#include <sys/socket.h>
#include <sys/un.h>
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
static void okay(const Error&e,const char*what){if(!e.ok())throw std::runtime_error(std::string(what)+": "+e.message);}
class Link final:public Clock{
    int fd;
    wire::Reply last;
public:
    explicit Link(const char*path){
        sockaddr_un a{};a.sun_family=AF_UNIX;wire::need(std::strlen(path)<sizeof(a.sun_path),"socket path");std::strcpy(a.sun_path,path);
        fd=::socket(AF_UNIX,SOCK_STREAM,0);wire::need(fd>=0&&::connect(fd,reinterpret_cast<sockaddr*>(&a),sizeof(a))==0,"connect");
    }
    ~Link(){::close(fd);}
    wire::Reply call(unsigned op,unsigned arg=0,unsigned value=0){
        timeval timeout{};timeout.tv_sec=op==wire::Read||op==wire::Write||op==combined::PrRead||op==combined::PrWrite?2:900;
        wire::need(::setsockopt(fd,SOL_SOCKET,SO_RCVTIMEO,&timeout,sizeof(timeout))==0,"socket timeout");
        std::array<unsigned char,16> request{};wire::encode(request.data(),op);wire::encode(request.data()+4,arg);wire::encode(request.data()+8,value);
        wire::need(wire::transfer(fd,request.data(),request.size(),true),"send");std::array<unsigned char,32> r{};
        wire::need(wire::transfer(fd,r.data(),r.size(),false),"receive");
        last.status=wire::decode(r.data());last.value=wire::decode(r.data()+4);
        last.cycles=wire::decode(r.data()+8)|(uint64_t(wire::decode(r.data()+12))<<32);
        last.retired=wire::decode(r.data()+16)|(uint64_t(wire::decode(r.data()+20))<<32);
        last.writes=wire::decode(r.data()+24);last.reads=wire::decode(r.data()+28);return last;
    }
    uint64_t NowMs()const override{return last.cycles/100000;}
    uint64_t deadline()const{return NowMs()+60000;}
    unsigned state(unsigned f){return call(wire::State,f).value;}
};
class Registers final:public Mmio{
    Link& link;bool pr;
public:
    bool fail_write=false;
    Registers(Link&l,bool p):link(l),pr(p){}
    Error Read32(uint32_t a,uint32_t*out)override{auto r=link.call(pr?unsigned(combined::PrRead):unsigned(wire::Read),a);*out=r.value;return r.status?Error{ErrorCode::io_failed,"register read","combined"}:Error{};}
    Error Write32(uint32_t a,uint32_t v)override{auto r=link.call(pr?unsigned(combined::PrWrite):unsigned(wire::Write),a,v);return r.status||fail_write?Error{ErrorCode::io_failed,"ambiguous applied write","combined"}:Error{};}
};
static void key(De25Computer& computer,Link&io,KeyboardHidRows rows){
    okay(computer.SetKeyboardHid(rows,io.deadline()),"HID");io.call(wire::Advance,100100);
}
static void type_load(De25Computer& computer,Link&io){
    KeyboardHidRows j{};j[0]=1u<<13;key(computer,io,j);io.call(wire::WaitToken,0xef,1);key(computer,io,{});io.call(wire::WaitEditor,160000000);
    KeyboardHidRows quote{};quote[1]=1u<<3;quote[8]=2;
    for(unsigned n=1;n<=2;++n){key(computer,io,quote);io.call(wire::WaitToken,0x0b,n);key(computer,io,{});io.call(wire::WaitEditor,160000000);}
    io.call(wire::WaitPicture,2);
    KeyboardHidRows enter{};enter[2]=1u<<8;key(computer,io,enter);
}
int main(int argc,char**argv){
    try{
        wire::need(argc==4,"expected socket scenario build-id");Link io(argv[1]);Registers computer_io(io,false),pr_io(io,true);
        De25Computer computer(computer_io,io);De25PrControl pr(pr_io,io);De25PrIdentity identity;
        std::string scenario=argv[2],build=argv[3],selected=build;
        if(scenario=="wrong-build")selected[0]=selected[0]=='0'?'1':'0';
        auto identified=pr.Identify(selected,io.deadline(),&identity);
        unsigned callbacks=0,loaded=0,ramtop_a=0,ramtop_b=0;
        auto configure=[&](unsigned persona){++callbacks;io.call(combined::Select,persona);return Error{};};
        if(scenario=="wrong-build"){
            wire::need(!identified.ok()&&pr.Poisoned()&&io.state(combined::PrWrites)==0,"wrong build mutated PR endpoint");
        }else{
            okay(identified,"PR identity");De25ComputerIdentity computer_identity;okay(computer.Identify(build,io.deadline(),&computer_identity),"computer identity");
            // A successful mailbox RUN cannot bypass an unselected/frozen PR
            // resource. Keep RUN requested in negative cases to test that gate.
            okay(computer.Run(io.deadline()),"RUN while PR frozen");
            auto held_retired=io.call(wire::State,wire::MachineReset).retired;
            auto held_writes=io.state(combined::MemoryWrites);io.call(wire::Advance,4096);
            wire::need(io.state(wire::MachineReset)&&io.call(wire::State,wire::MachineReset).retired==held_retired&&io.state(combined::MemoryWrites)==held_writes,"mailbox RUN bypassed PR freeze");
            if(scenario=="session"){
                okay(computer.Hold(io.deadline()),"HOLD before initial selection");
                okay(pr.Reconfigure(FesDe25PrControlPersonaA,[&]{return configure(FesDe25PrControlPersonaA);},io.deadline()),"empty selection");
                io.call(wire::Advance,4096);
                wire::need(io.state(wire::MachineReset)&&io.call(wire::State,wire::MachineReset).retired==held_retired,"PR RUNNING bypassed mailbox HOLD");
                okay(computer.Run(io.deadline()),"empty RUN");std::cerr<<"BOOT empty\n";ramtop_a=io.call(combined::WaitRamtop,0x4400).value;
                okay(computer.Hold(io.deadline()),"HOLD before RAM selection");
                io.call(wire::PixelClock,0);
                okay(pr.Reconfigure(FesDe25PrControlPersonaB,[&]{return configure(FesDe25PrControlPersonaB);},io.deadline()),"RAM selection with stopped PIX");
                okay(computer.Run(io.deadline()),"RAM RUN");io.call(wire::PixelClock,1);
                std::cerr<<"BOOT 16K\n";ramtop_b=io.call(combined::WaitRamtop,0x8000).value;
                unsigned size=io.call(wire::CaptureNew).value;std::vector<unsigned char> image;
                for(unsigned at=0;at<size;++at)image.push_back(io.call(wire::PeekRam,0x4009+at).value);
                io.call(wire::PixelClock,0);okay(computer.LoadTape(image,io.deadline(),127),"CRC tape upload");
                auto before=io.call(wire::State,wire::MachineReset).retired;io.call(wire::Advance,500000);
                wire::need(io.call(wire::State,wire::MachineReset).retired>before,"pixel stop blocked CPU");io.call(wire::PixelClock,1);
                std::cerr<<"LOAD full\n";io.call(wire::BeginLoad);type_load(computer,io);loaded=io.call(wire::WaitLoaded).value;
                wire::need(loaded==size,"external LOAD incomplete");key(computer,io,{});
                // A loaded NEW snapshot is not an editor-return contract. Boot
                // a fresh RAM selection before exercising another real LOAD.
                okay(computer.Hold(io.deadline()),"HOLD before second RAM boot");
                okay(pr.Reconfigure(FesDe25PrControlPersonaB,[&]{return configure(FesDe25PrControlPersonaB);},io.deadline()),"fresh RAM selection");
                okay(computer.Run(io.deadline()),"fresh RAM RUN");
                wire::need(io.call(combined::WaitRamtop,0x8000).value==ramtop_b,"RAM re-selection did not reboot/re-size");
                okay(computer.LoadTape(image,io.deadline(),127),"second CRC tape upload");
                // Interrupt a second real LOAD with the PR freeze request.
                std::cerr<<"LOAD interrupted\n";io.call(wire::BeginLoad);type_load(computer,io);io.call(combined::WaitSomeLoad,8);
                okay(pr.Freeze(io.deadline()),"freeze during real loader writes");
                unsigned writes=io.state(combined::MemoryWrites);io.call(wire::Advance,2048);
                wire::need(io.state(combined::MemoryWrites)==writes&&io.state(wire::MachineReset),"freeze did not fence loader writes");
                okay(computer.Hold(io.deadline()),"HOLD frozen machine");okay(computer.EjectTape(io.deadline()),"eject");
                okay(pr.Reconfigure(FesDe25PrControlPersonaA,[&]{return configure(FesDe25PrControlPersonaA);},io.deadline()),"return empty");
                okay(computer.Run(io.deadline()),"empty reboot RUN");wire::need(io.call(combined::WaitRamtop,0x4400).value==ramtop_a,"empty did not reboot/re-size");
                okay(computer.Hold(io.deadline()),"final HOLD");okay(pr.Freeze(io.deadline()),"final freeze");
            }else{
                Error result;
                if(scenario=="late-reset"){
                    io.call(wire::Reset);result=pr.Reconfigure(FesDe25PrControlPersonaA,[&]{return configure(FesDe25PrControlPersonaA);},io.deadline());
                    wire::need(!computer.Run(io.deadline()).ok()&&computer.Poisoned(),"reset did not poison computer client");
                }else if(scenario=="ambiguous-write"){
                    pr_io.fail_write=true;result=pr.Reconfigure(FesDe25PrControlPersonaA,[&]{return configure(FesDe25PrControlPersonaA);},io.deadline());
                    wire::need(callbacks==0,"ambiguous command admitted selection");
                }else if(scenario=="program-failure"){
                    result=pr.Reconfigure(FesDe25PrControlPersonaB,[&]{configure(FesDe25PrControlPersonaB);return Error{ErrorCode::program_failed,"applied selection then injected failure","combined"};},io.deadline());
                }else if(scenario=="wrong-persona"){
                    result=pr.Reconfigure(FesDe25PrControlPersonaA,[&]{return configure(FesDe25PrControlPersonaB);},io.deadline());
                }else if(scenario=="missing-response"){
                    result=pr.Reconfigure(FesDe25PrControlPersonaA,[&]{configure(FesDe25PrControlPersonaA);io.call(combined::DropResponse,1);return Error{};},io.deadline());
                }else throw std::runtime_error("unknown scenario");
                wire::need(!result.ok()&&pr.Poisoned(),"uncertainty did not poison PR control");
                unsigned writes=io.state(combined::PrWrites),count=callbacks;
                wire::need(!pr.Reconfigure(FesDe25PrControlPersonaA,[&]{return configure(FesDe25PrControlPersonaA);},io.deadline()).ok(),"poisoned controller retried");
                wire::need(io.state(combined::PrWrites)==writes&&callbacks==count,"poisoned controller issued more writes/selection");
                wire::need(!io.state(combined::Running)&&io.state(wire::MachineReset),"failed persona state allowed CPU execution");
            }
        }
        auto final=io.call(wire::Done);
        std::cout<<"{\"scenario\":\""<<scenario<<"\",\"result\":\"passed\",\"callbacks\":"<<callbacks<<",\"loaded_bytes\":"<<loaded<<",\"ramtop_empty\":"<<ramtop_a<<",\"ramtop_16k\":"<<ramtop_b<<",\"writes\":"<<final.writes<<"}\n";
    }catch(const std::exception&e){std::cerr<<"FAIL combined client: "<<e.what()<<"\n";return 1;}
}
