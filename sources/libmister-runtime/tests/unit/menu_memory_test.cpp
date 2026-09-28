// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/menu_memory.hpp"
#include "native/generated/fes_application.hpp"
#include <cassert>
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fcntl.h>
#include <sys/mman.h>
#include <unistd.h>
#include <vector>
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
class Operations : public MenuMemoryOperations {
public:
 struct Store {std::uint32_t* dest;std::size_t words;};
 bool reserved=true,mapped=false,barrier=false;
 unsigned maps=0, closes=0, unmaps=0;
 bool fail_map=false, empty_map=false;
 bool cancel_from_store=false,cancel=false;
 std::vector<Store> stores;
 std::vector<std::uint32_t> data=std::vector<std::uint32_t>(2*4194304/4,0xdeadbeef);
 Error VerifyReservation(std::uint64_t base,std::uint64_t bytes) override {assert(base==0x30000000&&bytes==0x10000000);return reserved?Error{}:Error{ErrorCode::io_failed,"not reserved"};}
 Error Open(int* fd) override {*fd=7;return {};}
 Error Map(int fd,std::uint64_t base,std::size_t bytes,void** out) override {assert(fd==7&&base==0x30000000&&bytes==8388608);++maps;if(empty_map){*out=nullptr;return {};}mapped=true;*out=data.data();return fail_map?Error{ErrorCode::io_failed,"mapping failed"}:Error{};}
 void Unmap(void*,std::size_t) override {mapped=false;++unmaps;}
 void Close(int fd) override {assert(fd==7);++closes;}
 void VisibilityBarrier() override {barrier=true;}
 void StoreAlignedWords(void* destination,const std::uint32_t* source,std::size_t words) override {
  auto* dest=static_cast<std::uint32_t*>(destination);
  assert(reinterpret_cast<std::uintptr_t>(dest)%4==0);
  assert(reinterpret_cast<std::uintptr_t>(source)%8==0);
  assert(words==FesApplicationMenuWidth);
  const auto index=static_cast<std::size_t>(dest-data.data());
  assert(index+words<=data.size());
  const bool slot1=index>=FesApplicationMenuSlotBytes/4;
  const auto slot_base=slot1?FesApplicationMenuSlotBytes/4:0;
  assert(index>=slot_base&&index+words<=slot_base+FesApplicationMenuFrameBytes/4);
  assert((index-slot_base)%FesApplicationMenuWidth==0);
  stores.push_back({dest,words});
  MenuMemoryOperations::StoreAlignedWords(destination,source,words);
  if(cancel_from_store)cancel=true;
 }
 bool MenuCopyCancelled() const override {return cancel;}
};
int main(){
 assert(!MenuMemory::VerifyExcludedSystemRam("00000000-3fffffff : System RAM\n").ok());
 assert(!MenuMemory::VerifyExcludedSystemRam("00000000-00000000 : System RAM\n").ok());
 assert(!MenuMemory::VerifyExcludedSystemRam("garbage\n").ok());
 assert(MenuMemory::VerifyExcludedSystemRam("00000000-1fefffff : System RAM\n1ff00000-3fffffff : reserved\n").ok());
 std::unique_ptr<MenuFrame> frame;assert(MenuFrame::Create(&frame).ok());assert(frame->fd()>=0);
 assert(!frame->ValidateImmutable(frame->fd()).ok());
 assert(ftruncate(frame->fd(),1)==0);assert(!frame->ValidateImmutable(frame->fd()).ok());assert(ftruncate(frame->fd(),3686400)==0);
 auto rgba=static_cast<unsigned char*>(mmap(nullptr,3686400,PROT_READ|PROT_WRITE,MAP_SHARED,frame->fd(),0));assert(rgba!=MAP_FAILED);
 memset(rgba,0,3686400);rgba[0]=0x11;rgba[1]=0x22;rgba[2]=0x33;rgba[3]=255;
 rgba[3686396]=0x44;rgba[3686397]=0x55;rgba[3686398]=0x66;rgba[3686399]=255;
 assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)<0);
 assert(munmap(rgba,3686400)==0);assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 const unsigned char* sealed_pixels=nullptr;
 assert(frame->ReadOnlyData(&sealed_pixels).ok());
 assert(sealed_pixels[0]==0x11&&sealed_pixels[3686399]==255);
 assert(frame->ValidateImmutable(frame->fd()).ok());std::unique_ptr<MenuFrame> other;assert(MenuFrame::Create(&other).ok());assert(!frame->ValidateImmutable(other->fd()).ok());
 Operations ops;
 { MenuMemory memory(ops);assert(memory.InitializeBlack().ok());assert(ops.maps==1&&ops.barrier);
   assert(ops.data[0]==0&&ops.data[3686400/4-1]==0&&ops.data[3686400/4]==0xdeadbeef);
   assert(ops.data[4194304/4]==0&&ops.data.back()==0xdeadbeef);
   ops.barrier=false;assert(memory.CopyRgba(1,*frame).ok());assert(ops.barrier);
   assert(ops.data[4194304/4]==0x00112233&&ops.data[(4194304+3686400)/4-1]==0x00445566);
   assert(ops.data[0]==0&&ops.data.back()==0xdeadbeef);assert(!memory.CopyRgba(2,*frame).ok());
   assert(!memory.CopyRgba(0,*other).ok());
 } assert(!ops.mapped);
 Operations denied;denied.reserved=false;MenuMemory missing(denied);assert(!missing.InitializeBlack().ok());assert(denied.maps==0);
 Operations failed;failed.fail_map=true;
 {MenuMemory memory(failed);assert(!memory.InitializeBlack().ok());assert(failed.closes==1&&failed.unmaps==1&&!failed.barrier);}
 assert(failed.closes==1&&failed.unmaps==1);
 Operations empty;empty.empty_map=true;
 {MenuMemory memory(empty);assert(!memory.InitializeBlack().ok());assert(empty.closes==1&&!empty.barrier);}
 assert(empty.closes==1&&empty.unmaps==0);
 std::unique_ptr<MenuFrame> short_frame;assert(MenuFrame::Create(&short_frame).ok());
 assert(ftruncate(short_frame->fd(),1)==0);assert(fcntl(short_frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 assert(!short_frame->ValidateImmutable(short_frame->fd()).ok());
 std::unique_ptr<MenuFrame> large_frame;assert(MenuFrame::Create(&large_frame).ok());
 assert(ftruncate(large_frame->fd(),3686401)==0);assert(fcntl(large_frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 assert(!large_frame->ValidateImmutable(large_frame->fd()).ok());
 const int released=large_frame->fd();large_frame.reset();assert(fcntl(released,F_GETFD)<0);
 std::unique_ptr<MenuFrame> pattern;assert(MenuFrame::Create(&pattern).ok());
 auto rgba_pattern=static_cast<unsigned char*>(mmap(nullptr,3686400,PROT_READ|PROT_WRITE,MAP_SHARED,pattern->fd(),0));
 assert(rgba_pattern!=MAP_FAILED);
 for(std::size_t i=0;i<3686400;i+=4){
  rgba_pattern[i]=static_cast<unsigned char>(i);
  rgba_pattern[i+1]=static_cast<unsigned char>(i>>8);
  rgba_pattern[i+2]=static_cast<unsigned char>(i>>16);
  rgba_pattern[i+3]=255;
 }
 assert(munmap(rgba_pattern,3686400)==0);
 assert(fcntl(pattern->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 Operations recorded;
 const auto started=std::chrono::steady_clock::now();
 {
  MenuMemory memory(recorded);
  assert(memory.CopyRgba(1,*pattern).ok());
  const double ms=std::chrono::duration<double,std::milli>(std::chrono::steady_clock::now()-started).count();
  std::fprintf(stderr,"menu copy host timing ms=%.3f stores=%zu\n",ms,recorded.stores.size());
  assert(recorded.stores.size()==FesApplicationMenuHeight);
  auto* slot=recorded.data.data()+FesApplicationMenuSlotBytes/4;
  for(std::size_t i=0;i<FesApplicationMenuFrameBytes/4;++i){
   const auto offset=i*4;
   const auto expect=std::uint32_t(static_cast<unsigned char>(offset>>16))|
    (std::uint32_t(static_cast<unsigned char>(offset>>8))<<8)|
    (std::uint32_t(static_cast<unsigned char>(offset))<<16);
   assert(slot[i]==expect);
  }
  assert(recorded.data[FesApplicationMenuFrameBytes/4-1]==0xdeadbeef);
  assert(recorded.data.back()==0xdeadbeef);
  recorded.stores.clear();recorded.cancel_from_store=true;
  assert(!memory.CopyRgba(0,*pattern).ok());
  assert(!recorded.stores.empty()&&recorded.stores.size()<=4);
  assert(recorded.data[4*FesApplicationMenuWidth]==0xdeadbeef);
  const auto writes=recorded.stores.size();
  memory.CancelCopies();
  assert(memory.CopyCancelled());
  assert(!memory.CopyRgba(1,*pattern).ok());
  assert(recorded.stores.size()==writes);
 }
 puts("menu memory reservation/seals/identity/pixels/slot bounds/barrier tests passed");
}
