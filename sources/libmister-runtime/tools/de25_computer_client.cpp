// SPDX-License-Identifier: GPL-3.0-or-later
// Explicit maintenance control of an already configured DE25 computer.
// Bus stalls cannot be interrupted by this client's software deadlines.
#include "native/de25_computer.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include <chrono>
#include <fstream>
#include <iostream>
#include <iterator>
#include <stdexcept>
#include <sys/file.h>
#include <fcntl.h>
#include <thread>
#include <unistd.h>
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
namespace {
void require(bool value,const char* message) {if(!value)throw std::runtime_error(message);}
void okay(const Error& error) {if(!error.ok())throw std::runtime_error(error.message);}
#if defined(__aarch64__) && defined(__linux__)
std::string read(const char* path) {
    std::ifstream file(path,std::ios::binary);require(bool(file),"required board identity unavailable");
    std::string text((std::istreambuf_iterator<char>(file)),{});
    while(!text.empty() && (text.back()=='\n' || text.back()=='\0'))text.pop_back();
    return text;
}
#endif
class Registers final : public Mmio,public Clock {
    LinuxMmio physical_;
    int lease_=-1;
    std::uint32_t base_;
public:
    unsigned reads=0,writes=0;
    Registers(std::uint32_t base,const std::string& boot):base_(base) {
#if !defined(__aarch64__) || !defined(__linux__)
        (void)boot;throw std::runtime_error("maintenance hardware access requires AArch64 Linux");
#else
        require(geteuid()==0,"maintenance hardware access requires root");
        require(read("/etc/fes-board")=="de25-nano","FES DE25 rootfs identity differs");
        require(read("/sys/firmware/devicetree/base/model")=="SoCFPGA Agilex5 Terasic DE25-Nano","device-tree model differs");
        require(read("/proc/sys/kernel/random/boot_id")==boot,"selected boot epoch differs");
        require(base_==0x20000000u+FesDe25ComputerPeripheralOffset,"explicit CPU resource base differs");
        lease_=open("/run/fes-de25-computer.lock",O_CREAT|O_RDWR|O_CLOEXEC|O_NOFOLLOW,0600);
        require(lease_>=0,"maintenance lock unavailable");
        if(flock(lease_,LOCK_EX|LOCK_NB)) {close(lease_);lease_=-1;throw std::runtime_error("computer already owned");}
#endif
    }
    ~Registers() override {if(lease_>=0)close(lease_);}
    std::uint64_t NowMs() const override {return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now().time_since_epoch()).count();}
    Error Read32(std::uint32_t offset,std::uint32_t* value) override {
        if(offset>FesDe25ComputerVideoStatusOffset || (offset&3u))return {ErrorCode::io_failed,"outside computer read registers"};
        __sync_synchronize();auto e=physical_.Read32(base_+offset,value);__sync_synchronize();++reads;return e;
    }
    Error Write32(std::uint32_t offset,std::uint32_t value) override {
        if(offset!=FesDe25ComputerGpoOffset)return {ErrorCode::io_failed,"outside computer command register"};
        __sync_synchronize();auto e=physical_.Write32(base_+offset,value);__sync_synchronize();++writes;return e;
    }
};
}
int main(int argc,char** argv) {
    try {
        if(argc==2 && std::string(argv[1])=="--help") {
            std::cout<<"de25-computer BOOT_UUID BUILD_ID CPU_BASE probe|hold|run|reset|keys|media [ARGUMENT]\nExplicit maintenance of an already loaded image; no FPGA programming or daemon admission.\n";return 0;
        }
        require(argc==5 || argc==6,"expected boot UUID, build ID, CPU base, command and optional argument");
        const std::string boot=argv[1],build=argv[2],command=argv[4];
        require(boot.size()==36 && boot.find_first_not_of("0123456789abcdef-")==std::string::npos,"invalid boot UUID");
        require(build.size()==32 && build.find_first_not_of("0123456789abcdefABCDEF")==std::string::npos && build.find_first_not_of('0')!=std::string::npos,"invalid selected build ID");
        std::size_t used=0;const auto parsed=std::stoull(argv[3],&used,0);
        require(used==std::string(argv[3]).size() && parsed==0x20000000u+FesDe25ComputerPeripheralOffset,"invalid explicit CPU resource base");
        require(command=="probe" || command=="hold" || command=="run" || command=="reset" || command=="keys" || command=="media","unknown maintenance command");
        require(argc==((command=="keys" || command=="media")?6:5),"unexpected or missing command argument");
        Registers io(static_cast<std::uint32_t>(parsed),boot);
        std::uint32_t id=0,version=0;okay(io.Read32(FesDe25ComputerIdOffset,&id));okay(io.Read32(FesDe25ComputerVersionOffset,&version));
        require(id==FesDe25ComputerIdentity && version==FesDe25ComputerVersion,"physical endpoint identity/version differs");
        if(command!="probe") {
            De25Computer computer(io,io);De25ComputerIdentity identity;auto deadline=[&](){return io.NowMs()+60000;};
            okay(computer.Identify(build,deadline(),&identity));
            if(command=="hold" || command=="reset")okay(computer.Hold(deadline()));
            if(command=="run" || command=="reset")okay(computer.Run(deadline()));
            if(command=="media") {
                std::ifstream file(argv[5],std::ios::binary|std::ios::ate);require(bool(file),"tape unavailable");
                auto length=file.tellg();require(length>0 && length<=FesDe25ComputerZx81TapeMaxBytes,"tape length outside DE25 limits");
                file.seekg(0);std::vector<std::uint8_t> bytes(static_cast<std::size_t>(length));file.read(reinterpret_cast<char*>(bytes.data()),length);require(bool(file),"tape read failed");
                okay(computer.LoadTape(bytes,deadline()));
            }
            if(command=="keys") {
                std::string arguments=argv[5];std::size_t begin=0;
                do {
                    const auto end=arguments.find(',',begin);auto key=arguments.substr(begin,end-begin);const bool shift=!key.empty() && key.back()=='s';if(shift)key.pop_back();
                    require(!key.empty() && key.find_first_not_of("0123456789abcdefABCDEF")==std::string::npos,"invalid HID usage");
                    const auto usage=std::stoul(key,nullptr,16);require(usage>=4 && usage<128,"HID usage outside ordinary key range");
                    KeyboardHidRows modifiers{};if(shift)modifiers[8]=2;
                    if(shift)okay(computer.SetKeyboardHid(modifiers,deadline()));
                    auto rows=modifiers;rows[usage/16]=1u<<(usage%16);okay(computer.SetKeyboardHid(rows,deadline()));
                    std::this_thread::sleep_for(std::chrono::milliseconds(150));okay(computer.SetKeyboardHid(modifiers,deadline()));
                    if(shift)okay(computer.SetKeyboardHid({},deadline()));
                    std::this_thread::sleep_for(std::chrono::milliseconds(150));
                    if(end==std::string::npos)break;
                    begin=end+1;
                } while(true);
            }
        }
        std::cout<<"{\"command\":\""<<command<<"\",\"status\":\"passed\",\"register_reads\":"<<io.reads<<",\"register_writes\":"<<io.writes<<",\"physical_cpu_mmio\":true,\"production_runtime_admission\":false}\n";return 0;
    } catch(const std::exception& error) {std::cerr<<"de25-computer: "<<error.what()<<'\n';return 1;}
}
