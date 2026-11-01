// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/de25_menu.hpp"
#include "native/hardware.hpp"
#include "native/generated/fes_application.hpp"
#include "native/generated/fes_de25_scanout.hpp"
#include <cerrno>
#include <cstring>
#include <fcntl.h>
#include <limits>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>
#if defined(__linux__)
#include <sys/ioctl.h>
#include "native/linux/de25_dma.h"
#endif
namespace mister { namespace native {
namespace {
using namespace generated;
constexpr std::uint32_t kAbi=3,kExposed=2,kPending=8,kNoSlot=~std::uint32_t(0),kBlackFrames=2;
Error Invalid(const char* s){return {ErrorCode::invalid_request,s,"menu"};}
Error Failed(const char* s){return {ErrorCode::io_failed,s,"menu"};}
Error Unsupported(){return {ErrorCode::unsupported_interface,"DE25 allocated MENU has no blitter or retained session plane","menu"};}
#if defined(__linux__)
static_assert(FES_DE25_DMA_ABI_VERSION==kAbi&&FES_DE25_DMA_EXPOSED==kExposed&&FES_DE25_DMA_PENDING==kPending,"kernel UAPI differs");
static_assert(sizeof(fes_de25_dma_info)==104&&sizeof(fes_de25_dma_publish)==24,"kernel ioctl layout differs");
Error Io(const char* s){return {ErrorCode::io_failed,std::string(s)+": "+std::strerror(errno),"menu"};}
class KernelOperations final : public De25MenuOperations {
 int owner_=-1;
 Error Open() {
  if(owner_>=0)return {};
  const int fd=open("/dev/fes-de25-scanout",O_RDWR|O_CLOEXEC|O_NOFOLLOW);
  if(fd<0)return Io("open DE25 owner");
  struct stat st{};
  if(fstat(fd,&st)<0||!S_ISCHR(st.st_mode)||st.st_uid||st.st_gid||(st.st_mode&07777)!=0600) {
   close(fd);return Failed("DE25 owner is not the root-only character device");
  }
  owner_=fd;return {};
 }
 static void Decode(const fes_de25_dma_info& raw,De25MenuGrant* out) {
#define FIELD(n) out->n=raw.n
  FIELD(generation);FIELD(version);FIELD(allocation_bytes);FIELD(slot_bytes);FIELD(frame_bytes);
  FIELD(width);FIELD(height);FIELD(stride);FIELD(slots);FIELD(state);FIELD(reserved);
  FIELD(accepted_sequence);FIELD(displayed_sequence);FIELD(displayed_slot);
  FIELD(pending_sequence);FIELD(pending_slot);FIELD(back_slot);
  FIELD(hardware_status);FIELD(hardware_errors);FIELD(underflows);FIELD(outstanding);
#undef FIELD
  for(unsigned i=0;i<4;++i)out->build_id[i]=raw.build_id[i];
 }
public:
 ~KernelOperations() override {if(owner_>=0)close(owner_);}
 Error OwnerAbi(std::uint32_t* out) override {
  Error e=Open();if(!e.ok())return e;
  const int result=ioctl(owner_,FES_DE25_DMA_OWNER_ABI,0UL);
  if(result<0)return Io("DE25 owner ABI");
  *out=std::uint32_t(result);return {};
 }
 Error Allocate(int* fd) override {
  *fd=ioctl(owner_,FES_DE25_DMA_OWNER_ALLOC,0UL);return *fd<0?Io("DE25 allocation"):Error{};
 }
 Error Info(int fd,De25MenuGrant* out) override {
  fes_de25_dma_info raw{};if(ioctl(fd,FES_DE25_DMA_INFO,&raw)<0)return Io("DE25 grant info");
  Decode(raw,out);return {};
 }
 Error Poll(int fd,De25MenuGrant* out) override {
  fes_de25_dma_info raw{};raw.version=kAbi;raw.generation=out->generation;
  if(ioctl(fd,FES_DE25_DMA_POLL,&raw)<0)return Io("DE25 grant poll");
  Decode(raw,out);return {};
 }
 Error Publish(int fd,std::uint64_t generation,std::uint32_t sequence,std::uint32_t slot) override {
  fes_de25_dma_publish p{};p.version=kAbi;p.generation=generation;p.sequence=sequence;p.slot=slot;
  return ioctl(fd,FES_DE25_DMA_PUBLISH,&p)<0?Io("DE25 publish"):Error{};
 }
 Error Reap(bool* done) override {
  const int result=ioctl(owner_,FES_DE25_DMA_OWNER_REAP,0UL);
  if(result<0&&errno!=EAGAIN)return Io("DE25 owner drain");
  *done=result==0;return {};
 }
 Error Idle(bool* idle) override {
  const int result=ioctl(owner_,FES_DE25_DMA_OWNER_IDLE,0UL);
  if(result<0&&errno!=EBUSY)return Io("DE25 owner reference check");
  *idle=result==0;return {};
 }
 Error Duplicate(int fd,int* out) override {
  *out=fcntl(fd,F_DUPFD_CLOEXEC,0);return *out<0?Io("share DE25 grant"):Error{};
 }
 void Close(int fd) override {close(fd);}
 void WaitMs(std::uint32_t ms) override {timespec t{};t.tv_sec=ms/1000;t.tv_nsec=(ms%1000)*1000000;(void)nanosleep(&t,nullptr);}
};
#else
class KernelOperations final : public De25MenuOperations {
public:
 Error OwnerAbi(std::uint32_t*) override {return Unsupported();}
 Error Allocate(int*) override {return Unsupported();}
 Error Info(int,De25MenuGrant*) override {return Unsupported();}
 Error Poll(int,De25MenuGrant*) override {return Unsupported();}
 Error Publish(int,std::uint64_t,std::uint32_t,std::uint32_t) override {return Unsupported();}
 Error Reap(bool*) override {return Unsupported();}
 Error Idle(bool*) override {return Unsupported();}
 Error Duplicate(int,int*) override {return Unsupported();}
 void Close(int) override {}
 void WaitMs(std::uint32_t) override {}
};
#endif
}
LinuxDe25Menu::LinuxDe25Menu(Clock& c,std::uint32_t ms):clock_(c),owned_(new KernelOperations),operations_(owned_.get()),revoke_ms_(ms){}
LinuxDe25Menu::LinuxDe25Menu(Clock& c,De25MenuOperations& o,std::uint32_t ms):clock_(c),operations_(&o),revoke_ms_(ms){}
LinuxDe25Menu::~LinuxDe25Menu(){(void)Revoke();if(grant_>=0)operations_->Close(grant_);}
Error LinuxDe25Menu::Deadline(std::uint64_t d) const {
 return clock_.NowMs()>=d?Failed("DE25 MENU operation exceeded its deadline"):Error{};
}
Error LinuxDe25Menu::Fault(Error e){if(!e.ok())faulted_=true;return e;}
Error LinuxDe25Menu::CheckOwnerAbi(std::uint64_t d) {
 std::lock_guard<std::mutex> lock(mutex_);
 Error e=Deadline(d);if(!e.ok())return e;
 std::uint32_t abi=0;e=operations_->OwnerAbi(&abi);if(e.ok())e=Deadline(d);
 if(!e.ok())return e;
 return abi==kAbi?Error{}:Error{ErrorCode::unsupported_interface,"DE25 owner ABI must be 3 before allocation","menu"};
}
Error LinuxDe25Menu::Validate(const De25MenuGrant& v) const {
 using namespace generated;
 if(v.version!=kAbi||!v.generation||v.generation!=info_.generation||v.reserved||v.build_id!=expected_||
    v.allocation_bytes!=FesDe25ScanoutAllocationBytes||v.slot_bytes!=FesDe25ScanoutSlotBytes||
    v.frame_bytes!=FesDe25ScanoutFrameBytes||v.width!=FesDe25ScanoutFrameWidth||
    v.height!=FesDe25ScanoutFrameHeight||v.stride!=FesDe25ScanoutFrameStride||v.slots!=FesDe25ScanoutSlotCount||
    (v.state&~(kExposed|kPending))||(v.hardware_status&~FesDe25ScanoutStatusMask)||
    (v.hardware_status&(FesDe25ScanoutStatusFault|FesDe25ScanoutStatusClockFault))||v.hardware_errors||v.underflows)
  return {ErrorCode::core_mismatch,"DE25 grant identity, geometry or health differs","menu"};
 if(!exposure_possible_) {
  if((v.hardware_status&~FesDe25ScanoutStatusDisplayReady)!=FesDe25ScanoutStatusDrained||v.state||
     v.accepted_sequence||v.displayed_sequence||v.displayed_slot||v.pending_sequence||
     v.pending_slot!=kNoSlot||v.back_slot!=1||v.outstanding)return Failed("DE25 allocation is not cold and drained");
 } else {
  const auto required=FesDe25ScanoutStatusConfigured|FesDe25ScanoutStatusRunning|FesDe25ScanoutStatusDisplayReady;
  if(!(v.state&kExposed)||(v.hardware_status&required)!=required||(v.hardware_status&FesDe25ScanoutStatusDrained)||
     v.accepted_sequence!=submitted_)return Failed("DE25 submit is not observed in the same running allocation");
  if(v.displayed_sequence==submitted_) {
   if(v.state&kPending||v.hardware_status&FesDe25ScanoutStatusPending||v.pending_sequence||
      v.pending_slot!=kNoSlot||v.displayed_slot!=submitted_slot_||v.back_slot!=(submitted_slot_^1U))
    return Failed("DE25 full-frame completion does not own its slot");
  } else if(v.displayed_sequence!=completed_||v.displayed_slot!=completed_slot_||!(v.state&kPending)||
     !(v.hardware_status&FesDe25ScanoutStatusPending)||v.pending_sequence!=submitted_||
     v.pending_slot!=submitted_slot_||v.back_slot!=kNoSlot)return Failed("DE25 pending frame ordering differs");
 }
 return {};
}
Error LinuxDe25Menu::Refresh(std::uint64_t d) {
 Error e=Deadline(d);if(!e.ok())return Fault(e);
 if(grant_<0||faulted_)return Failed("DE25 MENU has no healthy allocation");
 De25MenuGrant next{};next.generation=info_.generation;next.version=kAbi;
 e=operations_->Poll(grant_,&next);if(e.ok())e=Deadline(d);if(e.ok())e=Validate(next);
 if(!e.ok())return Fault(e);
 info_=next;
 if(exposure_possible_&&info_.displayed_sequence==submitted_) {completed_=submitted_;completed_slot_=submitted_slot_;}
 return {};
}
Error LinuxDe25Menu::Bind(const std::array<std::uint32_t,4>& expected,std::uint64_t d) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(grant_>=0||revoke_failed_)return Failed("DE25 allocation must retire before another bind");
 if(!(expected[0]|expected[1]|expected[2]|expected[3]))return Invalid("DE25 MENU requires a selected nonzero build identity");
 Error e=Deadline(d);if(!e.ok())return e;
 std::uint32_t abi=0;e=operations_->OwnerAbi(&abi);if(e.ok())e=Deadline(d);
 if(!e.ok())return e;
 if(abi!=kAbi)return {ErrorCode::unsupported_interface,"DE25 owner ABI must be 3 before allocation","menu"};
 bool idle=false;e=operations_->Idle(&idle);if(e.ok())e=Deadline(d);
 if(!e.ok())return e;
 if(!idle)return {ErrorCode::busy,"DE25 owner retains another allocation or peer mapping","menu"};
 expected_=expected;info_={};submitted_=completed_=0;submitted_slot_=completed_slot_=0;
 activated_=bootstrapped_=exposure_possible_=reap_done_=faulted_=false;
 e=operations_->Allocate(&grant_);if(e.ok())e=Deadline(d);
 if(!e.ok())return Fault(e);
 if(grant_<0)return Fault(Failed("DE25 owner returned no allocation"));
 e=operations_->Info(grant_,&info_);if(e.ok())e=Deadline(d);if(e.ok())e=Validate(info_);
 return Fault(e);
}
Error LinuxDe25Menu::Activate(bool clear,bool blitter) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(!clear||blitter)return Unsupported();
 if(grant_<0||activated_||faulted_)return Failed("DE25 MENU activation needs one freshly bound allocation");
 activated_=true;return {};
}
Error LinuxDe25Menu::Publish(std::uint32_t sequence,std::uint8_t slot,std::uint64_t d) {
 Error e=Refresh(d);if(!e.ok())return e;
 if((info_.state&kPending)||slot>=2||info_.back_slot!=slot||sequence<=submitted_)
  return Invalid("DE25 submission does not own its back slot or increasing sequence");
 if(!(info_.hardware_status&FesDe25ScanoutStatusDisplayReady))return Failed("DE25 HDMI is not ready");
 submitted_=sequence;submitted_slot_=slot;exposure_possible_=true;
 e=operations_->Publish(grant_,info_.generation,sequence,slot);if(e.ok())e=Deadline(d);
 if(!e.ok())return Fault(e);
 return Refresh(d);
}
Error LinuxDe25Menu::Complete(std::uint64_t d) {
 for(;;) {
  Error e=Refresh(d);if(!e.ok())return e;
  if(completed_==submitted_)return {};
  operations_->WaitMs(1);
 }
}
Error LinuxDe25Menu::Configure(std::uint64_t d) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(!activated_||bootstrapped_||faulted_)return Failed("DE25 MENU configure requires a fresh activation");
 for(;;) {
  Error e=Refresh(d);if(!e.ok())return e;
  if(info_.hardware_status&FesDe25ScanoutStatusDisplayReady)break;
  operations_->WaitMs(1);
 }
 // The allocation is kernel-cleared. Confirm black frames in slots1 then0
 // using actual complete-frame acknowledgements. Hardware sequence2 becomes
 // logical initial sequence0; all later sequences add that explicit base.
 for(std::uint32_t sequence=1;sequence<=kBlackFrames;++sequence) {
  Error e=Publish(sequence,std::uint8_t(sequence&1U),d);if(e.ok())e=Complete(d);if(!e.ok())return e;
 }
 bootstrapped_=true;return {};
}
Error LinuxDe25Menu::Enable(std::uint64_t d) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(!bootstrapped_)return Failed("DE25 MENU needs confirmed safe black before enabling");
 // SUBMIT starts refresh on this endpoint. Observe that state instead of
 // inventing a separate enable register or acknowledging an undisplayed frame.
 return Refresh(d);
}
void LinuxDe25Menu::Observation(MenuDisplayInfo* out,bool retired) const {
 *out={};out->geometry={FesDe25ScanoutFrameWidth,FesDe25ScanoutFrameHeight,FesDe25ScanoutFrameStride,FesDe25ScanoutFrameBytes,FesDe25ScanoutSlotBytes};
 out->pixel_format=FesApplicationMenuPixelFormat;
 out->configured=!retired&&(info_.hardware_status&FesDe25ScanoutStatusConfigured);
 out->enabled=!retired&&(info_.hardware_status&FesDe25ScanoutStatusRunning);
 out->pending=!retired&&(info_.hardware_status&FesDe25ScanoutStatusPending);
 out->quiesced=retired||(info_.hardware_status&FesDe25ScanoutStatusDrained);
 out->faulted=faulted_&&!retired;
 out->displayed_sequence=bootstrapped_?completed_-kBlackFrames:0;
 out->underflows=info_.underflows;
}
Error LinuxDe25Menu::ReadInfo(std::uint64_t d,MenuDisplayInfo* out) {
 std::lock_guard<std::mutex> lock(mutex_);if(!out)return Invalid("missing DE25 MENU observation output");
 Error e=Refresh(d);if(e.ok())Observation(out);return e;
}
Error LinuxDe25Menu::Submit(std::uint8_t slot,std::uint32_t sequence,std::uint64_t d) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(!bootstrapped_)return Failed("DE25 MENU is not configured");
 if(!sequence||sequence>std::numeric_limits<std::uint32_t>::max()-kBlackFrames)return Invalid("DE25 MENU sequence exhausted; retire this allocation");
 return Publish(sequence+kBlackFrames,slot,d);
}
Error LinuxDe25Menu::Share(int* out) const {
 std::lock_guard<std::mutex> lock(mutex_);
 if(!out)return Invalid("missing DE25 grant descriptor output");
 *out=-1;
 if(!bootstrapped_||faulted_||grant_<0)return Failed("DE25 MENU cannot share an unready allocation");
 return operations_->Duplicate(grant_,out);
}
Error LinuxDe25Menu::Clean(std::uint8_t slot,const std::vector<MenuRect>& rects) const {
 std::lock_guard<std::mutex> lock(mutex_);
 if(!bootstrapped_||faulted_||grant_<0||slot>=2||info_.back_slot!=slot||rects.size()>kMenuWrittenMaxRects)
  return Invalid("DE25 clean does not own an available slot");
 for(const auto& r:rects)if(!r.w||!r.h||std::uint32_t(r.x)+r.w>FesDe25ScanoutFrameWidth||std::uint32_t(r.y)+r.h>FesDe25ScanoutFrameHeight)
  return Invalid("DE25 written rectangle exceeds its frame");
 // dma_alloc_attrs storage is coherent; the kernel PUBLISH orders CPU writes.
 // No physical window, PL310 operation or runtime frame copy is involved.
 return {};
}
Error LinuxDe25Menu::CleanBlit(std::uint32_t,std::uint32_t,const std::vector<MenuSpan>&) const {return Unsupported();}
Error LinuxDe25Menu::RunBlit(std::uint32_t,std::uint32_t,std::uint8_t,std::uint64_t){return Unsupported();}
Error LinuxDe25Menu::ReadBlit(std::uint64_t,MenuBlitState*){return Unsupported();}
Error LinuxDe25Menu::Retire(std::uint64_t d) {
 if(revoke_failed_)return revoke_error_;
 if(grant_<0)return {};
 auto failed=[&](Error e){faulted_=revoke_failed_=true;revoke_error_=e;return e;};
 while(!reap_done_) {
  Error e=Deadline(d);if(!e.ok())return failed(e);
  e=operations_->Reap(&reap_done_);if(e.ok())e=Deadline(d);if(!e.ok())return failed(e);
  if(!reap_done_)operations_->WaitMs(1);
 }
 operations_->Close(grant_);grant_=-1;
 for(;;) {
  Error e=Deadline(d);if(!e.ok())return failed(e);
  bool idle=false;e=operations_->Idle(&idle);if(e.ok())e=Deadline(d);if(!e.ok())return failed(e);
  if(idle)break;
  operations_->WaitMs(1);
 }
 activated_=false;faulted_=false;return {};
}
Error LinuxDe25Menu::Revoke() {
 std::lock_guard<std::mutex> lock(mutex_);
 const auto now=clock_.NowMs();
 if(now>std::numeric_limits<std::uint64_t>::max()-revoke_ms_)return Failed("DE25 revocation deadline overflow");
 return Retire(now+revoke_ms_);
}
Error LinuxDe25Menu::Quiesce(std::uint64_t d,const CoreDescriptor*,MenuDisplayInfo* out) {
 std::lock_guard<std::mutex> lock(mutex_);Error e=Retire(d);if(e.ok()&&out)Observation(out,true);return e;
}
} }
