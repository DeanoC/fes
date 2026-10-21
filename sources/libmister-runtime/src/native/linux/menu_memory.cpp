// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/menu_memory.hpp"
#include "native/generated/fes_application.hpp"
#include <algorithm>
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
// Four 1280-pixel rows. A cancel observed just after a check finishes the
// current group and does not start the next one.
constexpr std::size_t kMenuCopyCancelRows=4;
Error Invalid(const char* text){return {ErrorCode::io_failed,text,"menu_memory"};}
Error Io(const char* text){return {ErrorCode::io_failed,std::string(text)+": "+std::strerror(errno),"menu_memory"};}
// Word stores only. The menu window is strongly ordered, so an unaligned or
// byte store faults, and one STM must not cross a 4KB boundary. Eight words
// are 32 bytes; both the 4MB slot and a 5120-byte row are multiples of 32,
// and 4096 is too, so a burst that starts 32-byte aligned stays inside a page.
void StoreMenuWords(void* destination,const std::uint32_t* source,std::size_t words)
{
#if defined(__arm__) && !defined(__thumb__)
 auto* dst=static_cast<std::uint32_t*>(destination);
 while(words>=8){
  register const std::uint32_t* in asm("r0")=source;
  register std::uint32_t* out asm("r1")=dst;
  __asm__ __volatile__(
   "ldmia r0!, {r2, r3, r4, r5, r6, r8, r9, r10}\n\t"
   "stmia r1!, {r2, r3, r4, r5, r6, r8, r9, r10}"
   : "+r"(in), "+r"(out)
   :
   : "r2","r3","r4","r5","r6","r8","r9","r10","memory");
  source=in;dst=out;words-=8;
 }
 auto* tail=static_cast<volatile std::uint32_t*>(dst);
 for(std::size_t i=0;i<words;++i)tail[i]=source[i];
#else
 auto* dst=static_cast<volatile std::uint32_t*>(destination);
 for(std::size_t i=0;i<words;++i)dst[i]=source[i];
#endif
}
}
MenuFrame::~MenuFrame(){if(mapping_)munmap(mapping_,kFrameBytes);if(fd_>=0)close(fd_);}
MenuFrame::MenuFrame(MenuFrame&& other) noexcept : fd_(other.fd_),mapping_(other.mapping_),preparation_(std::move(other.preparation_)),generation_(other.generation_),damage_(std::move(other.damage_)),has_damage_(other.has_damage_){other.fd_=-1;other.mapping_=nullptr;other.has_damage_=false;}
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
  // F_SEAL_WRITE forbids even a read-only shared mapping on the kit kernel.
  void* mapped=mmap(nullptr,kFrameBytes,PROT_READ,MAP_PRIVATE,fd_,0);
  if(mapped==MAP_FAILED)return Io("map immutable menu staging");
  mapping_=mapped;
 }
 *output=static_cast<const unsigned char*>(mapping_);return {};
}
namespace native {
void MenuMemoryOperations::StoreAlignedWords(void* destination,const std::uint32_t* source,std::size_t words)
{StoreMenuWords(destination,source,words);}
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
 DiscardShadow();
 Error error=EnsureMapped();if(!error.ok())return error;
 alignas(8) std::uint32_t zeros[FesApplicationMenuWidth]={};
 for(unsigned slot=0;slot<FesApplicationMenuSlotCount;++slot){
  auto row=reinterpret_cast<std::uint32_t*>(static_cast<unsigned char*>(mapping_)+slot*FesApplicationMenuSlotBytes);
  for(std::size_t y=0;y<FesApplicationMenuHeight;++y,row+=FesApplicationMenuWidth)
   operations_->StoreAlignedWords(row,zeros,FesApplicationMenuWidth);
 }
 operations_->VisibilityBarrier();return {};
}
void MenuMemory::DiscardShadow()
{std::lock_guard<std::mutex> lock(shadow_mutex_);shadow_valid_=false;shadow_sequence_=0;stale_[0].clear();stale_[1].clear();}
bool MenuMemory::HasShadow(std::uint32_t base_sequence) const
{std::lock_guard<std::mutex> lock(shadow_mutex_);return shadow_valid_&&base_sequence!=0&&shadow_sequence_==base_sequence;}
namespace {
constexpr std::uint16_t kMenuWidth=FesApplicationMenuWidth,kMenuHeight=FesApplicationMenuHeight;
// Slot stores are 32-byte bursts: widen a rect to whole 8-pixel groups.
MenuRect AlignedForStore(MenuRect r)
{
 const unsigned x0=r.x&~7u,x1=std::min<unsigned>((unsigned(r.x)+r.w+7u)&~7u,kMenuWidth);
 r.x=std::uint16_t(x0);r.w=std::uint16_t(x1-x0);return r;
}
MenuRect Bounding(const std::vector<MenuRect>& rects)
{
 unsigned x0=kMenuWidth,y0=kMenuHeight,x1=0,y1=0;
 for(const auto& r:rects){x0=std::min<unsigned>(x0,r.x);y0=std::min<unsigned>(y0,r.y);x1=std::max<unsigned>(x1,r.x+r.w);y1=std::max<unsigned>(y1,r.y+r.h);}
 return {std::uint16_t(x0),std::uint16_t(y0),std::uint16_t(x1-x0),std::uint16_t(y1-y0)};
}
void AddStale(std::vector<MenuRect>* stale,const std::vector<MenuRect>& rects)
{
 stale->insert(stale->end(),rects.begin(),rects.end());
 if(stale->size()>kMenuDamageMaxRects){const MenuRect all=Bounding(*stale);stale->assign(1,all);}
}
}
Error MenuMemory::CopyRgba(std::uint8_t slot,const MenuFrame& frame,std::uint32_t sequence)
{
 if(slot>=FesApplicationMenuSlotCount)return Invalid("invalid menu DDR slot");
 const unsigned char* bytes=nullptr;Error error=frame.ReadOnlyData(&bytes);if(!error.ok())return error;
 error=EnsureMapped();if(!error.ok())return error;
 std::unique_lock<std::mutex> lock(shadow_mutex_);
 const MenuDamage* damage=frame.damage();
 if(damage&&(!shadow_valid_||shadow_sequence_!=damage->base_sequence))
  return Invalid("menu damage base is not the shadowed frame");
 // Mark the shadow invalid for the duration: a failure or cancel below
 // leaves it, and the slot, unknown.
 shadow_valid_=false;
 if(shadow_.size()!=std::size_t(kMenuWidth)*kMenuHeight)shadow_.assign(std::size_t(kMenuWidth)*kMenuHeight,0);
 const std::vector<MenuRect> full{{0,0,kMenuWidth,kMenuHeight}};
 const std::vector<MenuRect>& changed=damage?damage->rects:full;
 for(const auto& r:changed){
  if(r.w==0||r.h==0||unsigned(r.x)+r.w>kMenuWidth||unsigned(r.y)+r.h>kMenuHeight)return Invalid("menu damage rect is outside the frame");
  for(unsigned y=r.y;y<unsigned(r.y)+r.h;++y){
   const unsigned char* source=bytes+(std::size_t(y)*kMenuWidth+r.x)*4;
   std::uint32_t* row=shadow_.data()+std::size_t(y)*kMenuWidth+r.x;
   for(unsigned x=0;x<r.w;++x,source+=4)
    // B,G,R,A bytes in DDR. Idle firmware ignores the top byte; a session
    // plane blends with it as premultiplied alpha.
    row[x]=std::uint32_t(source[2])|std::uint32_t(source[1])<<8|std::uint32_t(source[0])<<16|
     std::uint32_t(source[3])<<24;
  }
 }
 // Store what changed plus what this slot missed. Overlap is stored twice.
 std::vector<MenuRect> store;
 if(damage){store=changed;store.insert(store.end(),stale_[slot].begin(),stale_[slot].end());}
 else store=full;
 auto* base=static_cast<unsigned char*>(mapping_)+slot*FesApplicationMenuSlotBytes;
 std::size_t stored=0;unsigned rows=0;
 for(const auto& wanted:store){
  const MenuRect r=AlignedForStore(wanted);
  for(unsigned y=r.y;y<unsigned(r.y)+r.h;++y,++rows){
   if(rows%kMenuCopyCancelRows==0&&CopyCancelled())return Invalid("menu frame copy cancelled");
   auto* row=reinterpret_cast<std::uint32_t*>(base+(std::size_t(y)*kMenuWidth+r.x)*4);
   operations_->StoreAlignedWords(row,shadow_.data()+std::size_t(y)*kMenuWidth+r.x,r.w);
   stored+=r.w;
  }
 }
 operations_->VisibilityBarrier();
 stale_[slot].clear();
 if(damage)AddStale(&stale_[slot^1],changed);else stale_[slot^1]=full;
 shadow_valid_=sequence!=0;shadow_sequence_=sequence;last_stored_pixels_=stored;
 return {};
}
} }
