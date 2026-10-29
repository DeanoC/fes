// Executes the actual native runtime controller against a remote RTL model.
#include "native/de25_pr_control.hpp"
#include "native/generated/fes_de25_pr_control.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include <array>
#include <cerrno>
#include <cstdint>
#include <cstring>
#include <iostream>
#include <stdexcept>
#include <string>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
static void need(bool ok,const char* message) { if(!ok)throw std::runtime_error(message); }
static void encode(unsigned char* p,std::uint32_t v) { for(unsigned i=0;i<4;++i)p[i]=static_cast<unsigned char>(v>>(8*i)); }
static std::uint32_t word(const unsigned char* p) { return std::uint32_t(p[0])|(std::uint32_t(p[1])<<8)|(std::uint32_t(p[2])<<16)|(std::uint32_t(p[3])<<24); }
static void transfer(int fd,void* data,std::size_t size,bool writing) {
    auto* p=static_cast<unsigned char*>(data);
    while(size) { auto n=writing ? ::write(fd,p,size) : ::read(fd,p,size);if(n<0&&errno==EINTR)continue;need(n>0,"RTL socket transfer");p+=n;size-=n; }
}
class Transport final : public Mmio {
public:
    explicit Transport(const char* path) {
        sockaddr_un address{};address.sun_family=AF_UNIX;
        need(std::strlen(path)<sizeof(address.sun_path),"socket path too long");std::strcpy(address.sun_path,path);
        fd_=::socket(AF_UNIX,SOCK_STREAM,0);need(fd_>=0,"socket");
        need(::connect(fd_,reinterpret_cast<sockaddr*>(&address),sizeof(address))==0,"connect");
    }
    ~Transport() override { ::close(fd_); }
    std::uint32_t exchange(std::uint32_t op,std::uint32_t arg=0,std::uint32_t value=0) {
        std::array<unsigned char,12> request{};encode(request.data(),op);encode(request.data()+4,arg);encode(request.data()+8,value);
        transfer(fd_,request.data(),request.size(),true);
        std::array<unsigned char,16> response{};transfer(fd_,response.data(),response.size(),false);
        need(word(response.data())==0,"actual RTL reported bus error");return word(response.data()+4);
    }
    Error Read32(std::uint32_t offset,std::uint32_t* out) override { *out=exchange(1,offset);return {}; }
    Error Write32(std::uint32_t offset,std::uint32_t value) override {
        exchange(2,offset,value);
        if(fail_command&&offset==FesDe25PrControlCommandOffset) return {ErrorCode::io_failed,"injected ambiguous write","cosim"};
        return {};
    }
    bool fail_command=false;
private:int fd_=-1;
};
class TestClock final : public Clock { public:std::uint64_t NowMs() const override { return tick_++; }private:mutable std::uint64_t tick_=0; };
int main(int argc,char**argv) {
    try {
        need(argc==4,"expected socket, scenario and selected build identity");
        Transport io(argv[1]);TestClock clock;De25PrControl control(io,clock);De25PrIdentity identity;
        const std::string scenario=argv[2];
        std::string selected_build=argv[3];
        need(selected_build.size()==32,"expected 128-bit build identity");
        if(scenario=="wrong-build") selected_build[0]=selected_build[0]=='0'?'1':'0';
        auto result=control.Identify(selected_build,100000,&identity);
        unsigned callback_count=0;
        if(scenario=="wrong-build") {
            need(!result.ok()&&control.Poisoned(),"wrong build accepted");
            need(io.exchange(4)==0&&io.exchange(5)==0,"wrong build mutated endpoint");
        } else {
            need(result.ok()&&identity.frozen,"initial identity/freeze failed");
            auto configure=[&](std::uint32_t persona) { ++callback_count;io.exchange(3,persona);return Error{}; };
            if(scenario=="success") {
                for(auto persona:{FesDe25PrControlPersonaA,FesDe25PrControlPersonaB,FesDe25PrControlPersonaA}) {
                    need(control.Reconfigure(persona,[&] { return configure(persona); },100000).ok(),"reconfigure failed");
                    io.exchange(7,96);
                    need(io.exchange(1,FesDe25PrControlPersonaIdOffset)==persona,"wrong running persona");
                    need(io.exchange(1,FesDe25PrControlRetiredOffset)>0,"no actual RTL requests retired");
                }
                need(control.Freeze(100000).ok(),"final freeze failed");
                need(io.exchange(1,FesDe25PrControlStatusOffset)==(FesDe25PrControlStatusReady|FesDe25PrControlStatusFrozen),"not frozen at end");
                need(callback_count==3&&io.exchange(5)==3,"logical selection count differs");
            } else if(scenario=="program-failure") {
                result=control.Reconfigure(FesDe25PrControlPersonaA,[&] {++callback_count;return Error{ErrorCode::program_failed,"injected failure","cosim"};},100000);
                need(!result.ok()&&control.Poisoned(),"program failure did not poison");
                need(io.exchange(1,FesDe25PrControlStatusOffset)==(FesDe25PrControlStatusReady|FesDe25PrControlStatusFrozen),"program failure resumed");
            } else if(scenario=="wrong-persona") {
                result=control.Reconfigure(FesDe25PrControlPersonaA,[&] {return configure(FesDe25PrControlPersonaB);},100000);
                need(!result.ok()&&control.Poisoned(),"wrong actual persona accepted");
                need(io.exchange(1,FesDe25PrControlFaultOffset)&FesDe25PrControlFaultPersona,"actual RTL missed persona mismatch");
            } else if(scenario=="ambiguous-write") {
                io.fail_command=true;
                result=control.Reconfigure(FesDe25PrControlPersonaA,[&] {return configure(FesDe25PrControlPersonaA);},100000);
                need(!result.ok()&&control.Poisoned()&&callback_count==0,"ambiguous write admitted programming");
            } else if(scenario=="late-reset") {
                io.exchange(8);
                result=control.Reconfigure(FesDe25PrControlPersonaA,[&] {return configure(FesDe25PrControlPersonaA);},100000);
                need(!result.ok()&&control.Poisoned()&&callback_count==0,"late reset admitted programming");
            } else throw std::runtime_error("unknown scenario");
            if(scenario!="success") {
                auto before=io.exchange(4);
                need(!control.Reconfigure(FesDe25PrControlPersonaA,[&]{return configure(FesDe25PrControlPersonaA);},100000).ok(),"poisoned controller retried");
                need(io.exchange(4)==before,"poisoned retry mutated registers");
            }
        }
        std::cout<<"{\"scenario\":\""<<scenario<<"\",\"result\":\"passed\",\"writes\":"<<io.exchange(4)<<",\"logical_selections\":"<<io.exchange(5)<<"}\n";
        return 0;
    } catch(const std::exception& e) {std::cerr<<e.what()<<"\n";return 1;}
}
