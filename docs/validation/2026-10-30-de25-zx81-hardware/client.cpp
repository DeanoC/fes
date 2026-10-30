// SPDX-License-Identifier: GPL-3.0-or-later
// Explicit diagnostic: real De25Computer through a bounded JTAG register proxy.
#include "native/de25_computer.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include <array>
#include <chrono>
#include <cstring>
#include <fstream>
#include <iostream>
#include <iterator>
#include <stdexcept>
#include <sys/socket.h>
#include <sys/un.h>
#include <thread>
#include <unistd.h>
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
static void require(bool value, const char* what) { if (!value) throw std::runtime_error(what); }
static void okay(const Error& error) { if (!error.ok()) throw std::runtime_error(error.message); }
class Registers final : public Mmio, public Clock {
    int fd_ = -1;
    static void transfer(int fd, unsigned char* p, std::size_t bytes, bool writing) {
        while (bytes) {
            auto count = writing ? ::send(fd, p, bytes, MSG_NOSIGNAL) : ::read(fd, p, bytes);
            if (count < 0 && errno == EINTR) continue;
            if (count <= 0) throw std::runtime_error("bounded register proxy disconnected/timed out");
            p += count; bytes -= static_cast<std::size_t>(count);
        }
    }
    Error exchange(unsigned op, unsigned offset, unsigned value, unsigned* result) {
        try {
            std::array<unsigned char,12> request{};
            const unsigned words[] = {op,offset,value};
            for (unsigned n=0;n<3;++n) for (unsigned b=0;b<4;++b) request[n*4+b]=static_cast<unsigned char>(words[n]>>(b*8));
            transfer(fd_,request.data(),request.size(),true);
            std::array<unsigned char,8> reply{}; transfer(fd_,reply.data(),reply.size(),false);
            unsigned status=0; *result=0;
            for (unsigned b=0;b<4;++b) {status|=unsigned(reply[b])<<(b*8);*result|=unsigned(reply[4+b])<<(b*8);}
            if (status) return {ErrorCode::io_failed,"JTAG register transaction failed","de25-hardware-diagnostic"};
            return {};
        } catch (const std::exception& e) { return {ErrorCode::io_failed,e.what(),"de25-hardware-diagnostic"}; }
    }
public:
    unsigned reads=0,writes=0;
    explicit Registers(const char* path) {
        sockaddr_un address{};address.sun_family=AF_UNIX;
        require(std::strlen(path)<sizeof(address.sun_path),"socket path too long");std::strcpy(address.sun_path,path);
        fd_=::socket(AF_UNIX,SOCK_STREAM,0);require(fd_>=0,"socket creation failed");
        if (::connect(fd_,reinterpret_cast<sockaddr*>(&address),sizeof(address))) {::close(fd_);throw std::runtime_error("proxy connect failed");}
        timeval limit{};limit.tv_sec=2;
        if (::setsockopt(fd_,SOL_SOCKET,SO_RCVTIMEO,&limit,sizeof(limit)) || ::setsockopt(fd_,SOL_SOCKET,SO_SNDTIMEO,&limit,sizeof(limit))) {
            ::close(fd_);throw std::runtime_error("socket timeout setup failed");
        }
    }
    ~Registers() override {::close(fd_);}
    std::uint64_t NowMs() const override {return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now().time_since_epoch()).count();}
    Error Read32(std::uint32_t offset,std::uint32_t* value) override {++reads;return exchange(1,offset,0,value);}
    Error Write32(std::uint32_t offset,std::uint32_t value) override {++writes;unsigned ignored=0;return exchange(2,offset,value,&ignored);}
};
int main(int argc,char** argv) {
    try {
        require(argc==5,"expected socket, build ID, scenario, saved .p image");
        Registers io(argv[1]);De25Computer computer(io,io);De25ComputerIdentity identity;
        auto deadline=[&](){return io.NowMs()+60000;};
        const std::string scenario=argv[3];std::string selected=argv[2];
        require(selected.size()==32 && selected.find_first_not_of("0123456789abcdefABCDEF")==std::string::npos,"expected 128-bit hexadecimal build ID");
        require(scenario=="wrong-build" || scenario=="control" || scenario=="basic-keys" || scenario=="key" || scenario=="reset" || scenario=="media","unknown hardware scenario");
        if(scenario=="wrong-build") selected[0]=selected[0]=='0'?'1':'0';
        const auto identified=computer.Identify(selected,deadline(),&identity);
        if(scenario=="wrong-build") {
            require(!identified.ok() && computer.Poisoned(),"wrong build was not refused and poisoned");
            const auto before=io.writes;require(!computer.Run(deadline()).ok(),"poisoned client ran");require(io.writes==before,"poisoned client wrote again");
        } else {
            okay(identified);require(identity.build_id==selected,"selected build differs");
            if(scenario=="key") {
                std::string argument=argv[4];const bool shift=!argument.empty() && argument.back()=='s';if(shift)argument.pop_back();
                require(!argument.empty() && argument.find_first_not_of("0123456789abcdefABCDEF")==std::string::npos,"expected hexadecimal HID usage and optional s");
                const auto usage=std::stoul(argument,nullptr,16);require(usage>=4 && usage<128,"HID usage out of range");
                KeyboardHidRows rows{};rows[usage/16]=1u<<(usage%16);if(shift)rows[8]=2;
                okay(computer.SetKeyboardHid(rows,deadline()));std::this_thread::sleep_for(std::chrono::milliseconds(150));
                okay(computer.SetKeyboardHid({},deadline()));
            } else if(scenario=="media") {
                std::ifstream stream(argv[4],std::ios::binary);require(bool(stream),"saved image missing");
                std::vector<std::uint8_t> image((std::istreambuf_iterator<char>(stream)),{});
                require(image.size()==923,"expected frozen ROM SAVE image");okay(computer.LoadTape(image,deadline(),127));
            } else if(scenario=="reset") {
                okay(computer.Hold(deadline()));okay(computer.Run(deadline()));
            } else {
            okay(computer.Hold(deadline()));
            if(scenario=="control") {
                std::uint16_t response=0,rejected=0;
                okay(computer.Exchange(FesComputerOpcodeMenuInfo,0,0,deadline(),&response,&rejected));
                require(rejected==FesComputerErrorInvalidOpcode && !computer.Poisoned(),"known rejection damaged transport");
                const auto before=io.writes;
                require(!computer.LoadTape(std::vector<std::uint8_t>(FesDe25ComputerZx81TapeMaxBytes+1),deadline()).ok(),"oversized tape admitted");
                require(io.writes==before,"bounds refusal performed register writes");
                std::ifstream stream(argv[4],std::ios::binary);require(bool(stream),"saved image missing");
                std::vector<std::uint8_t> image((std::istreambuf_iterator<char>(stream)),{});
                require(image.size()==923,"expected frozen ROM SAVE image");
                okay(computer.LoadTape(image,deadline(),127));okay(computer.EjectTape(deadline()));
                okay(computer.Run(deadline()));KeyboardHidRows keys{};keys[1]=1u<<14;
                okay(computer.SetKeyboardHid(keys,deadline()));std::this_thread::sleep_for(std::chrono::milliseconds(100));
                okay(computer.SetKeyboardHid({},deadline()));okay(computer.Hold(deadline()));
            } else {
                okay(computer.Run(deadline()));std::this_thread::sleep_for(std::chrono::seconds(2));
                auto key=[&](unsigned usage) {KeyboardHidRows rows{};rows[usage/16]=1u<<(usage%16);okay(computer.SetKeyboardHid(rows,deadline()));std::this_thread::sleep_for(std::chrono::milliseconds(150));okay(computer.SetKeyboardHid({},deadline()));std::this_thread::sleep_for(std::chrono::milliseconds(150));};
                // Same HID sequence as the ROM-checked simulation; output needs observation.
                key(0x1e);key(0x13);key(0x24);key(0x28);key(0x15);key(0x28);
            }
        }
        }
        require(scenario=="wrong-build" || !computer.Poisoned(),"healthy session poisoned");
        std::cout<<"{\"scenario\":\""<<scenario<<"\",\"status\":\"passed\",\"register_reads\":"<<io.reads<<",\"register_writes\":"<<io.writes<<",\"basic_output_observed\":false}\n";
        return 0;
    } catch(const std::exception& e) {std::cerr<<e.what()<<'\n';return 1;}
}
