// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/menu_window.hpp"
#include "native/generated/fes_application.hpp"
#include <cerrno>
#include <cstring>
#include <fcntl.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <unistd.h>
#if defined(__linux__)
#include "native/linux/fes_window.h"
#endif
namespace mister {
namespace native {
namespace {
using namespace generated;
constexpr std::size_t kSlotBytes=FesApplicationMenuSlotBytes;
constexpr std::size_t kGrantBytes=FesApplicationMenuSlotCount*kSlotBytes;
// The blitter's lists and heap lie above the slots in the same window.
constexpr std::size_t kBlitterGrantBytes=std::size_t(FesApplicationMenuBlitHeapOffset)+FesApplicationMenuBlitHeapBytes;
static_assert(kGrantBytes<=FesApplicationMenuBlitListOffset&&
 FesApplicationMenuBlitListOffset+FesApplicationMenuBlitListBytes<=FesApplicationMenuBlitHeapOffset&&
 FesApplicationMenuBlitCommandCount*FesApplicationMenuBlitCommandBytes==FesApplicationMenuBlitListBytes,
 "the blitter layout lies above the slots without overlap");
Error Invalid(const char* text){return {ErrorCode::io_failed,text,"menu_window"};}
Error Io(const char* text){return {ErrorCode::io_failed,std::string(text)+": "+std::strerror(errno),"menu_window"};}
class DriverOperations final : public MenuWindowOperations {
public:
 Error Grant(std::uint64_t offset,std::uint64_t bytes,int* fd) override {
#if defined(__linux__)
  const int device=open("/dev/fes-window",O_RDWR|O_CLOEXEC);
  if(device<0)return Io("open fes-window");
  fes_window_grant grant{};grant.offset=offset;grant.length=bytes;
  const int result=ioctl(device,FES_WINDOW_GRANT,&grant);
  const Error error=result<0?Io("grant menu window"):Error{};
  close(device);
  if(error.ok())*fd=grant.fd;
  return error;
#else
  (void)offset;(void)bytes;(void)fd;return Invalid("menu window grants require the Linux fes-window driver");
#endif
 }
 Error Map(int fd,std::size_t bytes,void** mapping) override {
  void* mapped=mmap(nullptr,bytes,PROT_READ|PROT_WRITE,MAP_SHARED,fd,0);
  if(mapped==MAP_FAILED)return Io("map menu window");
  *mapping=mapped;return {};
 }
 void Unmap(void* mapping,std::size_t bytes) override {(void)munmap(mapping,bytes);}
 Error Clean(int fd,std::uint64_t offset,std::uint32_t width,std::uint32_t height,std::uint32_t stride) override {
#if defined(__linux__)
  fes_window_clean clean{};clean.offset=offset;clean.width=width;clean.height=height;clean.stride=stride;
  return ioctl(fd,FES_WINDOW_CLEAN,&clean)<0?Io("clean menu window"):Error{};
#else
  (void)fd;(void)offset;(void)width;(void)height;(void)stride;return Invalid("menu window cleans require Linux");
#endif
 }
 Error Revoke(int fd) override {
#if defined(__linux__)
  return ioctl(fd,FES_WINDOW_REVOKE)<0?Io("revoke menu window"):Error{};
#else
  (void)fd;return Invalid("menu window revoke requires Linux");
#endif
 }
 Error Duplicate(int fd,int* copy) override {
  *copy=fcntl(fd,F_DUPFD_CLOEXEC,0);return *copy<0?Io("share menu window"):Error{};
 }
 void Close(int fd) override {(void)close(fd);}
};
}
MenuWindow::MenuWindow():owned_(new DriverOperations),operations_(owned_.get()){}
DiskWindow::DiskWindow():owned_(new DriverOperations),operations_(owned_.get()){}
DiskWindow::DiskWindow(MenuWindowOperations& operations):operations_(&operations){}
Error DiskWindow::Access(std::uint64_t offset,std::size_t bytes,const std::function<Error(unsigned char*)>& use)
{
 int grant=-1;Error error=operations_->Grant(offset,bytes,&grant);
 if(!error.ok())return error;
 void* mapping=nullptr;error=operations_->Map(grant,bytes,&mapping);
 if(error.ok()) {
  error=use(static_cast<unsigned char*>(mapping));
  operations_->Unmap(mapping,bytes);
 }
 const Error revoked=operations_->Revoke(grant);
 operations_->Close(grant);
 return error.ok()?revoked:error;
}
MenuWindow::MenuWindow(MenuWindowOperations& operations):operations_(&operations){}
MenuWindow::~MenuWindow(){Revoke();}
Error MenuWindow::Activate(bool clear,bool blitter)
{
 Revoke();
 Error error;
 if(clear) {
  // The fill has its own grant. Revoking it writes back and invalidates
  // every slot line, which a clean would leave valid in the PL310.
  int fill=-1;error=operations_->Grant(0,kGrantBytes,&fill);
  if(!error.ok())return error;
  void* mapping=nullptr;error=operations_->Map(fill,kGrantBytes,&mapping);
  if(error.ok()) {
   // Black is all-zero XRGB. The frame rows are contiguous within a slot.
   for(unsigned slot=0;slot<FesApplicationMenuSlotCount;++slot)
    std::memset(static_cast<unsigned char*>(mapping)+slot*kSlotBytes,0,FesApplicationMenuFrameBytes);
   operations_->Unmap(mapping,kGrantBytes);
  }
  const Error revoked=operations_->Revoke(fill);
  operations_->Close(fill);
  if(error.ok())error=revoked;
  if(!error.ok())return error;
 }
 int grant=-1;error=operations_->Grant(0,blitter?kBlitterGrantBytes:kGrantBytes,&grant);
 if(!error.ok())return error;
 std::lock_guard<std::mutex> lock(mutex_);grant_=grant;blitter_=blitter;return {};
}
void MenuWindow::Revoke()
{
 int grant;
 {std::lock_guard<std::mutex> lock(mutex_);grant=grant_;grant_=-1;blitter_=false;}
 if(grant<0)return;
 // A failed revoke cannot be retried meaningfully; closing still drops
 // the runtime's reference, and the driver writes back on the last close.
 (void)operations_->Revoke(grant);
 operations_->Close(grant);
}
Error MenuWindow::Clean(std::uint8_t slot,const std::vector<MenuRect>& rects) const
{
 std::lock_guard<std::mutex> lock(mutex_);
 if(grant_<0)return {ErrorCode::busy,"menu window is not granted","menu_window"};
 const std::uint64_t base=std::uint64_t(slot)*kSlotBytes;
 if(rects.empty())return operations_->Clean(grant_,base,FesApplicationMenuFrameBytes,1,FesApplicationMenuStride);
 for(const auto& r:rects) {
  const Error error=operations_->Clean(grant_,base+std::uint64_t(r.y)*FesApplicationMenuStride+std::uint64_t(r.x)*4u,
   std::uint32_t(r.w)*4u,r.h,FesApplicationMenuStride);
  if(!error.ok())return error;
 }
 return {};
}
Error MenuWindow::CleanBlit(std::uint32_t first,std::uint32_t count,const std::vector<MenuSpan>& uploaded) const
{
 std::lock_guard<std::mutex> lock(mutex_);
 if(grant_<0)return {ErrorCode::busy,"menu window is not granted","menu_window"};
 if(!blitter_)return Invalid("menu window grant has no blitter");
 if(!count||first>=FesApplicationMenuBlitCommandCount||count>FesApplicationMenuBlitCommandCount-first)
  return Invalid("menu blit list is outside the list region");
 constexpr std::uint64_t heap_end=std::uint64_t(FesApplicationMenuBlitHeapOffset)+FesApplicationMenuBlitHeapBytes;
 for(const auto& span:uploaded)
  if(!span.bytes||span.offset<FesApplicationMenuBlitHeapOffset||span.bytes>heap_end-span.offset)
   return Invalid("menu upload is outside the texture heap");
 Error error=operations_->Clean(grant_,FesApplicationMenuBlitListOffset+std::uint64_t(first)*FesApplicationMenuBlitCommandBytes,
  count*FesApplicationMenuBlitCommandBytes,1,0);
 for(const auto& span:uploaded) {
  if(!error.ok())break;
  error=operations_->Clean(grant_,span.offset,span.bytes,1,0);
 }
 return error;
}
Error MenuWindow::Share(int* fd) const
{
 if(!fd)return Invalid("missing menu window output");
 std::lock_guard<std::mutex> lock(mutex_);
 if(grant_<0)return {ErrorCode::busy,"menu window is not granted","menu_window"};
 return operations_->Duplicate(grant_,fd);
}
} }
