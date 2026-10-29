// Native Verilator endpoint for the injected runtime MMIO test. No device IO.
#include "Vfes_link_sim.h"
#include "verilated.h"
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

static void need(bool ok, const char* message) { if (!ok) throw std::runtime_error(message); }
static bool receive(int fd, void* out, std::size_t bytes) {
    auto* p=static_cast<unsigned char*>(out);
    while(bytes) { auto n=::read(fd,p,bytes); if(n<0&&errno==EINTR)continue; if(n<=0)return false; p+=n;bytes-=n; }
    return true;
}
static void send_all(int fd,const void* in,std::size_t bytes) {
    auto* p=static_cast<const unsigned char*>(in);
    while(bytes) { auto n=::write(fd,p,bytes); if(n<0&&errno==EINTR)continue; need(n>0,"socket write");p+=n;bytes-=n; }
}
static std::uint32_t word(const unsigned char* p) {
    return std::uint32_t(p[0])|(std::uint32_t(p[1])<<8)|(std::uint32_t(p[2])<<16)|(std::uint32_t(p[3])<<24);
}
static void encode(unsigned char* p,std::uint32_t v) {
    for(unsigned i=0;i<4;++i)p[i]=static_cast<unsigned char>(v>>(8*i));
}
int main(int argc,char**argv) {
    try {
        need(argc==2,"expected private Unix socket path");
        Verilated::commandArgs(argc,argv);
        sockaddr_un address{}; address.sun_family=AF_UNIX;
        need(std::strlen(argv[1])<sizeof(address.sun_path),"socket path too long");
        std::strcpy(address.sun_path,argv[1]);
        int listener=::socket(AF_UNIX,SOCK_STREAM,0);need(listener>=0,"socket");
        need(::bind(listener,reinterpret_cast<sockaddr*>(&address),sizeof(address))==0,"bind");
        need(::listen(listener,1)==0,"listen");
        Vfes_link_sim d;
        d.clk=0;d.reset_n=0;d.persona_b=0;d.address=0;d.read=0;d.write=0;
        d.byteenable=15;d.writedata=0;d.drop_response=0;d.inject_response=0;
        std::uint64_t cycles=0;std::uint32_t writes=0,programs=0;
        auto tick=[&] { d.clk=0;d.eval();d.clk=1;d.eval();++cycles; };
        for(unsigned i=0;i<8;++i)tick();d.reset_n=1;for(unsigned i=0;i<16;++i)tick();
        need(d.frozen&&d.slot_reset,"cold endpoint not safely frozen");
        int client=::accept(listener,nullptr,nullptr);need(client>=0,"accept");
        std::array<unsigned char,12> request{};
        while(receive(client,request.data(),request.size())) {
            std::uint32_t op=word(request.data()),arg=word(request.data()+4),value=word(request.data()+8);
            std::uint32_t status=0,result=0;
            if(op==1||op==2) {
                need(arg<64&&arg%4==0,"invalid test register offset");
                d.address=arg;d.writedata=value;d.byteenable=15;d.read=op==1;d.write=op==2;
                for(unsigned i=0;d.waitrequest&&i<32;++i)tick();
                need(!d.waitrequest,"test bus never accepted transaction");tick();
                need(op==1?d.readdatavalid:d.writeresponsevalid,"missing actual RTL response");
                status=d.response;result=d.readdata;
                if(op==2)++writes;
                d.read=0;d.write=0;tick();
                // Advance simulated clock time without making the controller
                // spend a million IPC round trips on a lost response.
                if(op==1&&d.drop_response)for(unsigned i=0;i<1024;++i)tick();
            } else if(op==3) {
                // This is a logical persona-selection model, not SDM execution.
                need(d.frozen&&d.slot_reset&&!d.running,"program callback before freeze/drain");
                need(arg==0xc001a001u||arg==0xc002b002u,"unknown test persona");
                d.persona_b=arg==0xc002b002u;++programs;for(unsigned i=0;i<8;++i)tick();
            } else if(op==4) { result=writes; }
            else if(op==5) { result=programs; }
            else if(op==6) { d.drop_response=arg!=0;tick(); }
            else if(op==7) { for(unsigned i=0;i<arg;++i)tick(); }
            else if(op==8) { d.reset_n=0;tick();d.reset_n=1;for(unsigned i=0;i<4;++i)tick(); }
            else if(op==9) { break; }
            else throw std::runtime_error("unknown test request");
            std::array<unsigned char,16> response{};
            encode(response.data(),status);encode(response.data()+4,result);
            encode(response.data()+8,static_cast<std::uint32_t>(cycles));
            encode(response.data()+12,static_cast<std::uint32_t>(cycles>>32));
            send_all(client,response.data(),response.size());
        }
        ::close(client);::close(listener);::unlink(argv[1]);
        std::cout<<"PASS: actual RTL MMIO, writes="<<writes<<", logical selections="<<programs<<"\n";
        return 0;
    } catch(const std::exception& e) { std::cerr<<e.what()<<"\n";return 1; }
}
