// Source-bound DE25 shared-lifecycle hardware diagnostic, never installed.
#include "native/linux/de25_fpga_manager.hpp"
#include "native/linux/mmio.hpp"
#include "native/linux/input.hpp"
#include "native/de25_core_driver.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/artifacts.hpp"
#include "native/core_package.hpp"
#include "native/input.hpp"
#include <chrono>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <thread>
#include <fcntl.h>
#include <sys/file.h>
#include <unistd.h>
using namespace mister; using namespace mister::native; using namespace mister::native::generated;
void require(bool value,const char* text) {if(!value)throw std::runtime_error(text);}
void okay(const Error& e) {if(!e.ok())throw std::runtime_error(e.phase+": "+e.message);}
std::string read(const char* path) {
 std::ifstream file(path,std::ios::binary);require(bool(file),"required board identity unavailable");
 std::string value((std::istreambuf_iterator<char>(file)),{});
 while(!value.empty()&&(value.back()=='\n'||value.back()=='\0')) { value.pop_back(); }
 return value;
}
struct Time final:Clock {std::uint64_t NowMs() const override {return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now().time_since_epoch()).count();}};
struct Log final:LogSink {void Write(const LogRecord& r) override {std::cout<<"phase="<<r.phase<<" error="<<r.error.message<<std::endl;}};
struct Registers final:Mmio {
 LinuxMmio real;
 Error Read32(std::uint32_t offset,std::uint32_t* result) override {
  if(offset>FesDe25ComputerVideoStatusOffset||(offset&3))return {ErrorCode::io_failed,"invalid read"};
  __sync_synchronize();auto e=real.Read32(0x20010300+offset,result);__sync_synchronize();return e;
 }
 Error Write32(std::uint32_t offset,std::uint32_t value) override {
  if(offset!=FesDe25ComputerGpoOffset)return {ErrorCode::io_failed,"invalid write"};
  __sync_synchronize();auto e=real.Write32(0x20010300+offset,value);__sync_synchronize();return e;
 }
};
struct NoIdle final:VideoBringup {
 VideoQuiesceResult Quiesce(std::uint64_t) override {return {{ErrorCode::unsupported_interface,"idle not under test"},false};}
 VideoResult BringUp(const IdleRecipe&,std::uint64_t) override {VideoResult r;r.error={ErrorCode::unsupported_interface,"idle not under test"};return r;}
};
int main(int argc,char** argv) {
 try {
#if !defined(__linux__) || !defined(__aarch64__)
  (void)argc;(void)argv;throw std::runtime_error("diagnostic requires AArch64 Linux");
#else
  require(argc==5||argc==6,"usage: lifecycle BOOT PACKAGE PACKAGE_ID RBF_SHA256 [TAPE]");
  require(geteuid()==0,"root required");
  require(read("/etc/fes-board")=="de25-nano","FES board identity differs");
  require(read("/sys/firmware/devicetree/base/model")=="SoCFPGA Agilex5 Terasic DE25-Nano","board model differs");
  require(read("/proc/sys/kernel/random/boot_id")==argv[1],"boot epoch differs");
  int lease=open("/run/fes-de25-computer.lock",O_CREAT|O_RDWR|O_CLOEXEC|O_NOFOLLOW,0600);
  require(lease>=0&&flock(lease,LOCK_EX|LOCK_NB)==0,"exclusive control lease unavailable");
  OpenedCorePackage inspected;okay(OpenCorePackage({"/tmp"},argv[2],argv[3],&inspected));
  require(inspected.descriptor.core.id=="zx81"&&inspected.descriptor.payload.sha256==argv[4],"selected known ZX81 payload differs");
  Time clock;Log log;Registers registers;De25Computer client(registers,clock);
  De25ComputerCoreDriver driver(client);De25VideoBringup video(client);NoIdle idle;
  PosixArtifactOpener opener;LinuxDe25FpgaManager programmer(clock,lease);
  LinuxInput device(clock);NativeInputSession input(device,clock,10000);
  NativeHardware hardware(opener,programmer,idle,video,input,FogCastGamepadIdentity(),clock,log,
   "",{},nullptr,{"/tmp"},SplashIdle(),nullptr,nullptr,nullptr,NativePlatform::de25_nano,&driver);
  for(unsigned cycle=1;cycle<=2;++cycle) {
   std::unique_ptr<AdmittedCorePackage> admitted;
   okay(hardware.AdmitCorePackage(argv[2],argv[3],&admitted));
   auto result=hardware.LoadCore(std::move(admitted),cycle);okay(result.error);
   require(result.observed_core=="zx81","selected driver did not identify ZX81");
   okay(hardware.SetKeyboardHid({}));
   std::cout<<"CORE_RUNNING cycle="<<cycle<<std::endl;
   std::this_thread::sleep_for(std::chrono::seconds(5));
   if(argc==6) {
    okay(hardware.LoadComputerMediaLive(argv[5]));
    auto key=[&](unsigned usage,bool shift=false) {
     KeyboardHidRows modifiers{};if(shift)modifiers[8]=2;
     if(shift)okay(hardware.SetKeyboardHid(modifiers));
     auto rows=modifiers;rows[usage/16]=std::uint16_t(1u<<(usage%16));
     okay(hardware.SetKeyboardHid(rows));std::this_thread::sleep_for(std::chrono::milliseconds(180));
     okay(hardware.SetKeyboardHid(modifiers));
     if(shift)okay(hardware.SetKeyboardHid({}));
     std::this_thread::sleep_for(std::chrono::milliseconds(180));
    };
    key(0x0d);key(0x13,true);key(0x13,true);key(0x28);
    std::this_thread::sleep_for(std::chrono::seconds(2));
    key(0x15);key(0x28);
    std::this_thread::sleep_for(std::chrono::seconds(3));
    std::cout<<"BASIC_RUN_REQUESTED cycle="<<cycle<<std::endl;
   }
   std::cout<<"DE25_SHARED_LIFECYCLE_CYCLE_PASS cycle="<<cycle<<" production_factory=false"<<std::endl;
  }
  okay(client.Hold(clock.NowMs()+10000));
  require(!client.Poisoned()&&!programmer.Poisoned(),"diagnostic context uncertain");
  std::cout<<"DE25_SHARED_LIFECYCLE_PASS final_held=true owned_overlay_retained=true"<<std::endl;
  // Successful overlay cleanup remains an explicit host operation after exit.
  close(lease);
#endif
  return 0;
 } catch(const std::exception& e) {std::cerr<<"lifecycle: "<<e.what()<<std::endl;return 1;}
}
