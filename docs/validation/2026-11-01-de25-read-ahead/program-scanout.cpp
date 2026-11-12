#include "native/linux/de25_fpga_manager.hpp"
#include "native/artifacts.hpp"
#include "native/sha256.hpp"
#include <chrono>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <sys/file.h>
#include <fcntl.h>
#include <unistd.h>
using namespace mister; using namespace mister::native;
static void require(bool value,const char* why) {if(!value)throw std::runtime_error(why);}
static std::string text(const char* path) {
 std::ifstream f(path);require(bool(f),"identity unavailable");std::string s;std::getline(f,s);return s;
}
struct Time final:Clock {std::uint64_t NowMs()const override{return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now().time_since_epoch()).count();}};
int main(int argc,char** argv) {
 try {
#if !defined(__linux__) || !defined(__aarch64__)
  (void)argc;(void)argv;throw std::runtime_error("requires actual AArch64 Linux");
#else
  require(argc==4,"usage: program-scanout BOOT RBF SHA256");
  require(text("/proc/sys/kernel/random/boot_id")==argv[1],"boot differs");
  require(text("/etc/fes-board")=="de25-nano","board differs");
  int lease=open("/run/fes-de25-computer.lock",O_CREAT|O_RDWR|O_CLOEXEC|O_NOFOLLOW,0600);
  require(lease>=0&&flock(lease,LOCK_EX|LOCK_NB)==0,"maintenance lease busy");
  PosixArtifactOpener opener;Artifact artifact;auto e=OpenRBFArtifact(argv[2],opener,&artifact);
  if(!e.ok())throw std::runtime_error(e.message);
  require(artifact.size()==1736704,"selected source-bound RBF size differs");
  require(std::string(argv[3])=="774798756c26480c37ea996c832b60343c61e312f4db41bb154e79eea1586f9c",
          "requested artifact differs from selected source-bound RBF");
  Sha256 hash;unsigned char buffer[8192];std::uint64_t offset=0;
  while(offset<artifact.size()) {
   auto n=pread(artifact.fd(),buffer,sizeof(buffer),offset);require(n>0,"RBF read failed");
   hash.Update(buffer,n);offset+=n;
  }
  require(Sha256Hex(hash.Final())==argv[3],"selected source-bound RBF hash differs");
  Time clock;LinuxDe25FpgaManager manager(clock,lease,De25OverlayProfile::allocated_scanout);
  auto r=manager.Program(artifact,ProgrammingProfile::de25_hps_first_v1,clock.NowMs()+60000);
  if(!r.error.ok())throw std::runtime_error(r.error.phase+": "+r.error.message);
  require(!manager.Poisoned(),"programmer uncertain");
  std::cout<<"DE25_SCANOUT_PROGRAMMED allocation_requested=false owned_overlay_retained=true\n";
  close(lease);
#endif
  return 0;
 }catch(const std::exception& e){std::cerr<<"program-scanout: "<<e.what()<<'\n';return 1;}
}
