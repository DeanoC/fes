// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/menu_memory.hpp"
#include "native/generated/fes_application.hpp"
#include <atomic>
#include <cerrno>
#include <cstring>
#include <fstream>
#include <sstream>
#include <limits>
#include <fcntl.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/utsname.h>
#include <unistd.h>
#if defined(__linux__)
#include <linux/memfd.h>
#include <sys/syscall.h>
#endif
namespace mister {
namespace {
using namespace native::generated;
constexpr std::size_t kFrameBytes=FesApplicationMenuFrameBytes;
constexpr std::size_t kMappedBytes=2*FesApplicationMenuSlotBytes;
Error Invalid(const char* text){return {ErrorCode::io_failed,text,"menu_memory"};}
Error Io(const char* text){return {ErrorCode::io_failed,std::string(text)+": "+std::strerror(errno),"menu_memory"};}
}
MenuFrame::~MenuFrame(){if(mapping_)munmap(mapping_,kFrameBytes);if(fd_>=0)close(fd_);}
MenuFrame::MenuFrame(MenuFrame&& other) noexcept : fd_(other.fd_),mapping_(other.mapping_){other.fd_=-1;other.mapping_=nullptr;}
Error MenuFrame::Create(std::unique_ptr<MenuFrame>* output)
{
 if(!output)return Invalid("missing staging output");
#if defined(__linux__)
 int fd=static_cast<int>(syscall(SYS_memfd_create,"fes-menu-frame",MFD_CLOEXEC|MFD_ALLOW_SEALING));
 if(fd<0)return Io("create menu staging");
 if(ftruncate(fd,kFrameBytes)<0){Error error=Io("size menu staging");close(fd);return error;}
 output->reset(new MenuFrame(fd));return {};
#else
 return Invalid("menu staging requires Linux sealable memfd");
#endif
}
Error MenuFrame::ValidateImmutable(int received_fd) const
{
#if defined(__linux__)
 const int required=F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL;
 const int seals=fcntl(fd_,F_GET_SEALS);
 if(seals<0||(seals&required)!=required)return Invalid("menu staging must be immutable and sealed");
 // Check seals first: file size and contents can no longer change during stat/map.
 struct stat original{},received{};
 if(fd_<0||received_fd<0||fstat(fd_,&original)<0||fstat(received_fd,&received)<0)return Invalid("invalid menu staging descriptor");
 if(!S_ISREG(original.st_mode)||original.st_size!=static_cast<off_t>(kFrameBytes)||
    original.st_dev!=received.st_dev||original.st_ino!=received.st_ino||received.st_size!=original.st_size)
  return Invalid("menu staging identity or size changed");
 return {};
#else
 (void)received_fd;return Invalid("menu staging requires Linux seals");
#endif
}
Error MenuFrame::ReadOnlyData(const unsigned char** output) const
{
 if(!output)return Invalid("missing staging mapping output");
 Error error=ValidateImmutable(fd_);if(!error.ok())return error;
 if(!mapping_){
  void* mapped=mmap(nullptr,kFrameBytes,PROT_READ,MAP_SHARED,fd_,0);
  if(mapped==MAP_FAILED)return Io("map immutable menu staging");
  mapping_=mapped;
 }
 *output=static_cast<const unsigned char*>(mapping_);return {};
}
namespace native {
namespace {
class PosixMenuMemory final : public MenuMemoryOperations {
public:
 Error VerifyReservation(std::uint64_t base,std::uint64_t bytes) override {
  if(base!=FesApplicationHpsDdrWindowBase||bytes!=FesApplicationHpsDdrWindowBytes)return Invalid("unexpected menu DDR reservation");
#if defined(__arm__) && defined(__linux__)
  struct utsname kernel{};
  if(uname(&kernel)<0||std::string(kernel.release)!="5.15.1-MiSTer")
   return Invalid("menu DDR mapping needs the qualified MiSTer kernel");
  std::ifstream input("/proc/iomem");if(!input)return Invalid("cannot verify Linux RAM exclusion");
  std::ostringstream contents;contents<<input.rdbuf();if(input.bad())return Invalid("cannot read Linux RAM exclusion");
  return MenuMemory::VerifyExcludedSystemRam(contents.str());
#else
  return Invalid("physical menu DDR mapping requires ARM Linux HPS");
#endif
 }
 Error Open(int* output) override {*output=open("/dev/mem",O_RDWR|O_SYNC|O_CLOEXEC);return *output<0?Io("open menu DDR"):Error{};}
 Error Map(int fd,std::uint64_t base,std::size_t bytes,void** output) override {
  *output=nullptr;void* mapped=mmap(nullptr,bytes,PROT_READ|PROT_WRITE,MAP_SHARED,fd,static_cast<off_t>(base));
  if(mapped==MAP_FAILED)return Io("map reserved menu DDR");
  *output=mapped;return {};
 }
 void Unmap(void* mapping,std::size_t bytes) override {(void)munmap(mapping,bytes);}
 void Close(int fd) override {(void)close(fd);}
 void VisibilityBarrier() override {
#if defined(__arm__)
  __asm__ __volatile__("dsb sy" ::: "memory");
#else
  std::atomic_thread_fence(std::memory_order_seq_cst);
#endif
 }
};
}
MenuMemory::MenuMemory():owned_(new PosixMenuMemory),operations_(owned_.get()){}
MenuMemory::MenuMemory(MenuMemoryOperations& operations):operations_(&operations){}
MenuMemory::~MenuMemory(){if(mapping_)operations_->Unmap(mapping_,kMappedBytes);if(fd_>=0)operations_->Close(fd_);}
Error MenuMemory::VerifyExcludedSystemRam(const std::string& input)
{
 std::istringstream stream(input);std::string line;bool found=false;
 const std::uint64_t base=FesApplicationHpsDdrWindowBase,end=base+FesApplicationHpsDdrWindowBytes;
 while(std::getline(stream,line)){
  const auto colon=line.find(':');if(colon==std::string::npos)continue;
  std::string label=line.substr(colon+1);const auto start=label.find_first_not_of(" \t");
  if(start==std::string::npos||label.substr(start)!="System RAM")continue;
  const auto dash=line.find('-');if(dash==std::string::npos||dash>colon)return Invalid("invalid System RAM range");
  std::uint64_t first=0,last=0;std::string extra;
  std::istringstream lower(line.substr(0,dash)),upper(line.substr(dash+1,colon-dash-1));
  if(!(lower>>std::hex>>first)||lower>>extra||!(upper>>std::hex>>last)||upper>>extra||last<=first)
   return Invalid("invalid or redacted System RAM range");
  if(first<end&&last>=base)return Invalid("Linux System RAM overlaps the FPGA DDR window");
  found=true;
 }
 return found?Error{}:Invalid("Linux System RAM exclusion evidence is absent");
}
Error MenuMemory::EnsureMapped()
{
 if(mapping_)return {};
 Error error=operations_->VerifyReservation(FesApplicationHpsDdrWindowBase,FesApplicationHpsDdrWindowBytes);
 if(!error.ok())return error;
 error=operations_->Open(&fd_);if(!error.ok())return error;
 error=operations_->Map(fd_,FesApplicationHpsDdrWindowBase,kMappedBytes,&mapping_);
 if(!error.ok()){if(mapping_)operations_->Unmap(mapping_,kMappedBytes);mapping_=nullptr;operations_->Close(fd_);fd_=-1;return error;}
 if(!mapping_){operations_->Close(fd_);fd_=-1;return Invalid("empty menu DDR mapping");}
 return {};
}
Error MenuMemory::InitializeBlack()
{
 Error error=EnsureMapped();if(!error.ok())return error;
 for(unsigned slot=0;slot<FesApplicationMenuSlotCount;++slot){
  auto pixels=reinterpret_cast<volatile std::uint32_t*>(static_cast<unsigned char*>(mapping_)+slot*FesApplicationMenuSlotBytes);
  for(std::size_t i=0;i<kFrameBytes/4;++i)pixels[i]=0;
 }
 operations_->VisibilityBarrier();return {};
}
Error MenuMemory::CopyRgba(std::uint8_t slot,const MenuFrame& frame)
{
 if(slot>=FesApplicationMenuSlotCount)return Invalid("invalid menu DDR slot");
 const unsigned char* bytes=nullptr;Error error=frame.ReadOnlyData(&bytes);if(!error.ok())return error;
 error=EnsureMapped();if(!error.ok())return error;
 auto pixels=reinterpret_cast<volatile std::uint32_t*>(static_cast<unsigned char*>(mapping_)+slot*FesApplicationMenuSlotBytes);
 for(std::size_t i=0;i<kFrameBytes/4;++i){const auto offset=i*4;pixels[i]=std::uint32_t(bytes[offset+2])|std::uint32_t(bytes[offset+1])<<8|std::uint32_t(bytes[offset])<<16;}
 operations_->VisibilityBarrier();return {};
}
} }
