// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/de25_menu.hpp"
#include "native/hardware.hpp"
#include "native/de25_core_contract.hpp"
#include "native/generated/fes_application.hpp"
#include "native/generated/fes_de25_scanout.hpp"
#include <cerrno>
#include <algorithm>
#include <cstring>
#include <fcntl.h>
#include <limits>
#include <sstream>
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
constexpr std::uint32_t kAbi=3,kRevoked=1,kExposed=2,kUncertain=4,kPending=8,kNoSlot=~std::uint32_t(0),kBlackFrames=2;
constexpr std::uint32_t kDiagnosticsVersion=1,kDiagnosticsValid=1;
constexpr std::uint32_t kRetirementVersion=1,kRetirementValid=1;
static_assert(sizeof(De25MenuRetirement)==96,"retirement layout differs");
Error Invalid(const char* s){return {ErrorCode::invalid_request,s,"menu"};}
Error Failed(const char* s){return {ErrorCode::io_failed,s,"menu"};}
Error Unsupported(){return {ErrorCode::unsupported_interface,"DE25 allocated display has no blitter; reopening requires selected session control","menu"};}
void AppendFirstFault(Error& error,const De25MenuDiagnostics& v,std::uint64_t generation,
                      const std::array<std::uint32_t,4>& expected) {
 const auto state=v.first_fault_state,occupancy=v.first_fault_occupancy,gaps=v.first_fault_gaps;
 const auto h=(state>>FesDe25ScanoutFirstFaultHorizontalShift)&FesDe25ScanoutFirstFaultHorizontalMask;
 const auto y=(state>>FesDe25ScanoutFirstFaultVerticalShift)&FesDe25ScanoutFirstFaultVerticalMask;
 const auto relation=(state>>FesDe25ScanoutFirstFaultIndexRelationShift)&FesDe25ScanoutFirstFaultIndexRelationMask;
 if(v.version!=kDiagnosticsVersion||v.reserved||!v.generation||v.generation!=generation||v.build_id!=expected||
    v.flags!=kDiagnosticsValid||!(state&FesDe25ScanoutFirstFaultValidMask)||
    (occupancy&~FesDe25ScanoutFirstFaultOccupancyMask)||h>=FesDe25ScanoutFrameWidth||
    y>=FesDe25ScanoutFrameHeight||relation>FesDe25ScanoutFirstFaultIndexRelationGreater)return;
 const auto video=(state>>FesDe25ScanoutFirstFaultVideoStateShift)&FesDe25ScanoutFirstFaultVideoStateMask;
 const char* const states[]={"drain","start","fetch","display"};
 const char* const relations[]={"less","equal","greater"};
 std::ostringstream detail;
 detail<<"; cached first_underflow raw=0x"<<std::hex<<state<<"/0x"<<occupancy<<"/0x"<<gaps<<std::dec
  <<" h="<<h<<" v="<<y<<" video_state="<<states[video]
  <<" fifo="<<(occupancy&FesDe25ScanoutFirstFaultFifoCountMask)
  <<" accepted_outstanding="<<((occupancy>>FesDe25ScanoutFirstFaultOutstandingShift)&FesDe25ScanoutFirstFaultOutstandingMask)
  <<" lane="<<((occupancy>>FesDe25ScanoutFirstFaultLaneShift)&FesDe25ScanoutFirstFaultLaneMask)
  <<" relation="<<relations[relation]
  <<" pixel_valid="<<bool(state&FesDe25ScanoutFirstFaultPixelValidMask)
  <<" read="<<bool(state&FesDe25ScanoutFirstFaultReadMask)
  <<" waitrequest="<<bool(state&FesDe25ScanoutFirstFaultWaitrequestMask)
  <<" readdatavalid="<<bool(state&FesDe25ScanoutFirstFaultReaddatavalidMask)
  <<" response="<<(state&FesDe25ScanoutFirstFaultResponseMask)
  <<" max_response_gap_cycles="<<(gaps&FesDe25ScanoutFirstFaultResponseGapMask)
  <<" max_held_command_age_cycles="<<((gaps>>FesDe25ScanoutFirstFaultHeldCommandAgeShift)&FesDe25ScanoutFirstFaultHeldCommandAgeMask);
 error.message+=detail.str();
}
#if defined(__linux__)
static_assert(FES_DE25_DMA_ABI_VERSION==kAbi&&FES_DE25_DMA_REVOKED==kRevoked&&FES_DE25_DMA_EXPOSED==kExposed&&FES_DE25_DMA_UNCERTAIN==kUncertain&&FES_DE25_DMA_PENDING==kPending,"kernel UAPI differs");
static_assert(sizeof(fes_de25_dma_info)==104&&sizeof(fes_de25_dma_publish)==24,"kernel ioctl layout differs");
static_assert(FES_DE25_DMA_DIAGNOSTICS_VERSION==kDiagnosticsVersion&&FES_DE25_DMA_DIAGNOSTICS_VALID==kDiagnosticsValid&&sizeof(fes_de25_dma_diagnostics)==48,"kernel diagnostics layout differs");
static_assert(FES_DE25_DMA_RETIREMENT_VERSION==kRetirementVersion&&FES_DE25_DMA_RETIREMENT_VALID==kRetirementValid&&sizeof(fes_de25_dma_retirement)==96,"kernel retirement layout differs");
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
 Error OwnerInfo(bool* present,De25MenuGrant* out) override {
  fes_de25_dma_info raw{};
  if(ioctl(owner_,FES_DE25_DMA_OWNER_INFO,&raw)<0) {
   if(errno==ENODATA){*present=false;return {};}
   return Io("DE25 retained allocation observation");
  }
  *present=true;Decode(raw,out);return {};
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
 Error Diagnostics(int fd,std::uint64_t generation,De25MenuDiagnostics* out) override {
  fes_de25_dma_diagnostics raw{};raw.version=kDiagnosticsVersion;raw.generation=generation;
  if(ioctl(fd,FES_DE25_DMA_DIAGNOSTICS,&raw)<0)return Io("DE25 cached diagnostics");
  out->generation=raw.generation;out->version=raw.version;out->reserved=raw.reserved;
  for(unsigned i=0;i<4;++i)out->build_id[i]=raw.build_id[i];
  out->flags=raw.flags;out->first_fault_state=raw.first_fault_state;
  out->first_fault_occupancy=raw.first_fault_occupancy;out->first_fault_gaps=raw.first_fault_gaps;
  return {};
 }
 Error Publish(int fd,std::uint64_t generation,std::uint32_t sequence,std::uint32_t slot) override {
  fes_de25_dma_publish p{};p.version=kAbi;p.generation=generation;p.sequence=sequence;p.slot=slot;
  return ioctl(fd,FES_DE25_DMA_PUBLISH,&p)<0?Io("DE25 publish"):Error{};
 }
 Error Retirement(int fd,std::uint64_t generation,De25MenuRetirement* out) override {
  fes_de25_dma_retirement raw{};raw.version=kRetirementVersion;raw.generation=generation;
  if(ioctl(fd,FES_DE25_DMA_RETIREMENT,&raw)<0)return Io("DE25 retired grant receipt");
  static_assert(sizeof(raw)==sizeof(*out),"retirement copy layout");
  out->generation=raw.generation;out->version=raw.version;out->reserved=raw.reserved;
  std::copy(std::begin(raw.build_id),std::end(raw.build_id),out->build_id.begin());
#define FIELD(n) out->n=raw.n
  FIELD(flags);FIELD(allocation_bytes);FIELD(slot_bytes);FIELD(frame_bytes);FIELD(width);FIELD(height);FIELD(stride);FIELD(slots);
  FIELD(hardware_status);FIELD(hardware_errors);FIELD(underflows);FIELD(outstanding);
  FIELD(accepted_sequence);FIELD(displayed_sequence);FIELD(current_sequence);FIELD(slot_state);
#undef FIELD
  return {};
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
LinuxDe25Menu::LinuxDe25Menu(Clock& c,MenuDisplay& display,std::uint32_t ms):LinuxDe25Menu(c,ms){session_endpoint_=&display;}
LinuxDe25Menu::LinuxDe25Menu(Clock& c,De25MenuOperations& o,MenuDisplay& display,std::uint32_t ms):LinuxDe25Menu(c,o,ms){session_endpoint_=&display;}
Error LinuxDe25Menu::BindSession(const CoreDescriptor& descriptor,std::uint64_t d) {
 Error e=CheckDe25StandardComputerContract(descriptor);
 if(!e.ok())return e;
 if(!session_endpoint_)return Unsupported();
 std::array<std::uint32_t,4> expected{};e=De25BuildWords(descriptor.build.id,&expected);
 return e.ok()?Bind(expected,d,session_endpoint_):e;
}
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
  // Initial Configure still waits for
  // DISPLAY_READY. A live exact grant may wait through transmitter config;
  // hardware faults and full-frame slot ownership remain independently strict.
  const auto required=FesDe25ScanoutStatusConfigured|FesDe25ScanoutStatusRunning;
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
 Error e=Deadline(d);if(!e.ok()){configuration_timeout_=session_control_&&configuration_wait_&&submitted_!=completed_;return Fault(e);}
 if(grant_<0||faulted_)return Failed("DE25 MENU has no healthy allocation");
 De25MenuGrant next{};next.generation=info_.generation;next.version=kAbi;
 e=operations_->Poll(grant_,&next);
 if(!e.ok()&&clock_.NowMs()<d) {
  // INFO copies cached metadata without a register read, including after POLL
  // latches uncertainty. Keep the original failure and never adopt this tuple.
  De25MenuGrant cached{};
  const auto diagnostic=operations_->Info(grant_,&cached);
  if(diagnostic.ok()&&cached.version==kAbi&&cached.generation==info_.generation&&cached.build_id==expected_) {
   std::ostringstream detail;
   detail<<"; cached grant generation="<<cached.generation
    <<" state=0x"<<std::hex<<cached.state<<" hardware_status=0x"<<cached.hardware_status
    <<" hardware_errors=0x"<<cached.hardware_errors<<std::dec
    <<" underflows="<<cached.underflows<<" outstanding="<<cached.outstanding;
   e.message+=detail.str();
   // The optional ioctl copies the already authenticated kernel cache. An
   // old kernel, stale/malformed result or late reply changes no fault policy.
   if(clock_.NowMs()<d) {
    De25MenuDiagnostics first{};
    const auto observed=operations_->Diagnostics(grant_,info_.generation,&first);
    if(observed.ok()&&clock_.NowMs()<d)AppendFirstFault(e,first,info_.generation,expected_);
   }
  }
 }
 if(e.ok()) {
  const Error checked=Validate(next);
  const bool waiting=checked.ok()&&exposure_possible_&&(next.state&kPending)&&
   !(next.hardware_status&FesDe25ScanoutStatusDisplayReady)&&submitted_!=completed_;
  e=Deadline(d);
  if(!e.ok())configuration_timeout_=session_control_&&waiting;
  else {e=checked;configuration_wait_=waiting;}
 } else {configuration_wait_=false;configuration_timeout_=false;}
 if(!e.ok())return Fault(e);
 info_=next;
 if(exposure_possible_&&info_.displayed_sequence==submitted_) {completed_=submitted_;completed_slot_=submitted_slot_;}
 return {};
}
Error LinuxDe25Menu::Bind(const std::array<std::uint32_t,4>& expected,std::uint64_t d,MenuDisplay* session_control) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(session_control==this)return Invalid("DE25 session control must be a separate GP endpoint");
 return BindLocked(expected,d,session_control);
}
Error LinuxDe25Menu::BindLocked(const std::array<std::uint32_t,4>& expected,std::uint64_t d,MenuDisplay* session_control) {
 if(grant_>=0||revoke_failed_)return Failed("DE25 allocation must retire before another bind");
 if(!(expected[0]|expected[1]|expected[2]|expected[3]))return Invalid("DE25 MENU requires a selected nonzero build identity");
 Error e=Deadline(d);if(!e.ok())return e;
 std::uint32_t abi=0;e=operations_->OwnerAbi(&abi);if(e.ok())e=Deadline(d);
 if(!e.ok())return e;
 if(abi!=kAbi)return {ErrorCode::unsupported_interface,"DE25 owner ABI must be 3 before allocation","menu"};
 bool idle=false;e=operations_->Idle(&idle);if(e.ok())e=Deadline(d);
 if(!e.ok())return e;
 if(!idle)return {ErrorCode::busy,"DE25 owner retains another allocation or peer mapping","menu"};
 expected_=expected;session_control_=session_control;session_quiesced_=false;sequence_base_=0;
 info_={};submitted_=completed_=0;submitted_slot_=completed_slot_=0;
 activated_=bootstrapped_=exposure_possible_=reap_done_=faulted_=false;
 configuration_wait_=configuration_timeout_=configuration_retired_=false;
 e=operations_->Allocate(&grant_);if(e.ok())e=Deadline(d);
 if(!e.ok())return Fault(e);
 if(grant_<0)return Fault(Failed("DE25 owner returned no allocation"));
 e=operations_->Info(grant_,&info_);if(e.ok())e=Deadline(d);if(e.ok())e=Validate(info_);
 return Fault(e);
}
Error LinuxDe25Menu::Activate(bool clear,bool blitter) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(blitter||(!clear&&!session_control_))return Unsupported();
 if(!clear) {
  // Launch configured a cold grant before releasing execution. Its first
  // opening needs no retirement/allocation cycle and exposes no old frame.
  if(grant_>=0&&activated_&&bootstrapped_&&!exposure_possible_&&session_quiesced_&&!faulted_&&!revoke_failed_)
   return {};
  if(grant_>=0||!bootstrapped_||!session_quiesced_||faulted_||revoke_failed_) {
   std::ostringstream detail;
   detail<<"DE25 session reopen requires a successfully retired allocation"
    <<"; grant="<<(grant_>=0)<<" bootstrapped="<<bootstrapped_
    <<" quiesced="<<session_quiesced_<<" faulted="<<faulted_
    <<" revoke_failed="<<revoke_failed_<<" reaped="<<reap_done_;
   return {ErrorCode::io_failed,detail.str(),"menu"};
  }
  // OWNER_ALLOC clears hardware sequence/slot ownership. Preserve only the
  // logical completion count; storage and pixels belong to a fresh grant.
  const auto base=sequence_base_+completed_;
  const auto now=clock_.NowMs();
  if(now>std::numeric_limits<std::uint64_t>::max()-revoke_ms_)
   return Failed("DE25 session allocation deadline overflow");
  const auto expected=expected_;auto* control=session_control_;
  Error e=BindLocked(expected,now+revoke_ms_,control);if(!e.ok())return e;
  sequence_base_=base;bootstrapped_=true;
 }
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
 if(session_control_) {
  Error e=Refresh(d);if(e.ok())e=session_control_->Configure(d);if(e.ok())e=Deadline(d);
  if(!e.ok())return Fault(e);
  bootstrapped_=true;session_quiesced_=true;return {};
 }
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
 if(session_control_) {
  Error e=Refresh(d);if(e.ok()){session_quiesced_=false;e=session_control_->Enable(d);}
  if(e.ok())e=Deadline(d);
  return Fault(e);
 }
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
Error LinuxDe25Menu::SessionInfo(std::uint64_t d,MenuDisplayInfo* out) {
 MenuDisplayInfo observed;Error e=session_control_->ReadInfo(d,&observed);
 if(e.ok()&&(observed.displayed_sequence>submitted_||observed.underflows||observed.faulted))
  e=Failed("DE25 session control exceeds the kernel publication or has faulted");
 if(e.ok()) {
  e=Deadline(d);
  // Refresh authenticated the pending, physically invisible grant before
  // this healthy GP read consumed the last budget. Retirement still requires
  // the same exact clean kernel receipt; GP faults are never this deadline.
  if(!e.ok())configuration_timeout_=configuration_wait_&&submitted_!=completed_;
 }
 if(!e.ok())return Fault(e);
 observed.displayed_sequence+=sequence_base_;*out=observed;return {};
}
Error LinuxDe25Menu::ReadInfo(std::uint64_t d,MenuDisplayInfo* out) {
 std::lock_guard<std::mutex> lock(mutex_);if(!out)return Invalid("missing DE25 MENU observation output");
 Error e=Refresh(d);
 if(e.ok()) {if(session_control_)e=SessionInfo(d,out);else Observation(out);}
 return e;
}
Error LinuxDe25Menu::Submit(std::uint8_t slot,std::uint32_t sequence,std::uint64_t d) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(!bootstrapped_)return Failed("DE25 MENU is not configured");
 if(session_control_) {
  if(sequence<=sequence_base_)return Invalid("DE25 session requires an increasing logical sequence");
  return Publish(sequence-sequence_base_,slot,d);
 }
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
Error LinuxDe25Menu::ValidateRetirement(const De25MenuRetirement& r) const {
 const auto required=FesDe25ScanoutStatusConfigured|FesDe25ScanoutStatusDrained;
 if(r.generation!=info_.generation||r.version!=kRetirementVersion||r.reserved||r.build_id!=expected_||r.flags!=kRetirementValid||
    r.allocation_bytes!=FesDe25ScanoutAllocationBytes||r.slot_bytes!=FesDe25ScanoutSlotBytes||r.frame_bytes!=FesDe25ScanoutFrameBytes||
    r.width!=FesDe25ScanoutFrameWidth||r.height!=FesDe25ScanoutFrameHeight||r.stride!=FesDe25ScanoutFrameStride||r.slots!=FesDe25ScanoutSlotCount||
    (r.hardware_status&~FesDe25ScanoutStatusMask)||(r.hardware_status&required)!=required||
    (r.hardware_status&(FesDe25ScanoutStatusRunning|FesDe25ScanoutStatusPending|FesDe25ScanoutStatusFault|FesDe25ScanoutStatusClockFault))||
    r.hardware_errors||r.underflows||r.outstanding||r.accepted_sequence!=submitted_||
    (r.displayed_sequence!=completed_&&r.displayed_sequence!=submitted_)||
    (r.current_sequence!=completed_&&r.current_sequence!=submitted_)||
    (r.slot_state&~FesDe25ScanoutSlotsMask))return Failed("DE25 retired config receipt identity/health differs");
 const auto displayed=(r.slot_state>>FesDe25ScanoutDisplayedSlotShift)&FesDe25ScanoutSlotMask;
 const auto current=(r.slot_state>>FesDe25ScanoutCurrentSlotShift)&FesDe25ScanoutSlotMask;
 const auto pending=(r.slot_state>>FesDe25ScanoutPendingSlotShift)&FesDe25ScanoutSlotMask;
 if(displayed!=(r.displayed_sequence==completed_?completed_slot_:submitted_slot_)||
    current!=(r.current_sequence==completed_?completed_slot_:submitted_slot_)||pending!=submitted_slot_)
  return Failed("DE25 retired config receipt slot ownership differs");
 return {};
}
Error LinuxDe25Menu::Retire(std::uint64_t d) {
 if(revoke_failed_)return revoke_error_;
 if(grant_<0)return {};
 auto failed=[&](Error e){faulted_=revoke_failed_=true;revoke_error_=e;return e;};
 // OWNER_REAP clears the physical sequence after STOP/drain. Confirm every
 // healthy session publication first; no publisher can pass this mutex while
 // retirement runs. An uncertain publication is never replayed or adopted.
 if(session_control_&&!faulted_&&!reap_done_&&completed_!=submitted_) {
  Error e=Complete(d);if(!e.ok())return failed(e);
 }
 while(!reap_done_) {
  Error e=Deadline(d);if(!e.ok())return failed(e);
  e=operations_->Reap(&reap_done_);if(e.ok())e=Deadline(d);if(!e.ok())return failed(e);
  if(!reap_done_)operations_->WaitMs(1);
 }
 Error receipt_error;
 if(configuration_timeout_) {
  De25MenuRetirement retired;
  receipt_error=operations_->Retirement(grant_,info_.generation,&retired);
  if(receipt_error.ok())receipt_error=Deadline(d);
  if(receipt_error.ok())receipt_error=ValidateRetirement(retired);
  configuration_retired_=receipt_error.ok();
 }
 operations_->Close(grant_);grant_=-1;
 for(;;) {
  Error e=Deadline(d);if(!e.ok())return failed(e);
  bool idle=false;e=operations_->Idle(&idle);if(e.ok())e=Deadline(d);if(!e.ok())return failed(e);
  if(idle)break;
  operations_->WaitMs(1);
 }
 if(!receipt_error.ok())return failed(receipt_error);
 activated_=false;faulted_=false;return {};
}
Error LinuxDe25Menu::Revoke() {
 std::lock_guard<std::mutex> lock(mutex_);
 const auto now=clock_.NowMs();
 if(now>std::numeric_limits<std::uint64_t>::max()-revoke_ms_)return Failed("DE25 revocation deadline overflow");
 return Retire(now+revoke_ms_);
}
Error LinuxDe25Menu::RetireInherited(const CoreDescriptor& descriptor,std::uint64_t d) {
 std::lock_guard<std::mutex> lock(mutex_);
 if(revoke_failed_)return revoke_error_;
 if(grant_>=0||activated_||bootstrapped_||session_control_||faulted_)
  return Failed("inherited retirement requires a fresh allocation context");
 auto fail=[&](Error e){faulted_=revoke_failed_=true;revoke_error_=e;return e;};
 const auto now=clock_.NowMs();
 if(now>std::numeric_limits<std::uint64_t>::max()-revoke_ms_)
  return fail(Failed("DE25 inherited retirement deadline overflow"));
 d=std::min(d,now+revoke_ms_);
 Error e=descriptor.target.programming_profile=="de25-linux-menu-v1"?
  CheckDe25MenuContract(descriptor):CheckDe25StandardComputerContract(descriptor);
 std::array<std::uint32_t,4> expected{};
 if(e.ok())e=De25BuildWords(descriptor.build.id,&expected);
 if(e.ok())e=Deadline(d);
 std::uint32_t abi=0;if(e.ok())e=operations_->OwnerAbi(&abi);
 if(e.ok()&&abi!=kAbi)e=Failed("inherited retirement requires owner ABI3");
 bool present=false;De25MenuGrant retained;
 if(e.ok())e=operations_->OwnerInfo(&present,&retained);
 if(e.ok())e=Deadline(d);
 // REVOKED prevents old peers publishing again. File release can still be
 // draining the old exposure: cached EXPOSED/UNCERTAIN/PENDING bits are not a
 // drain proof or a reason to bypass the kernel's serialized REAP operation.
 if(e.ok()&&present&&(retained.version!=kAbi||!retained.generation||retained.reserved||
    retained.build_id!=expected||retained.allocation_bytes!=FesDe25ScanoutAllocationBytes||
    retained.slot_bytes!=FesDe25ScanoutSlotBytes||retained.frame_bytes!=FesDe25ScanoutFrameBytes||
    retained.width!=FesDe25ScanoutFrameWidth||retained.height!=FesDe25ScanoutFrameHeight||
    retained.stride!=FesDe25ScanoutFrameStride||retained.slots!=FesDe25ScanoutSlotCount||
    (retained.state&~(kRevoked|kExposed|kUncertain|kPending))||
    (retained.hardware_status&~FesDe25ScanoutStatusMask)||
    (retained.hardware_status&(FesDe25ScanoutStatusFault|FesDe25ScanoutStatusClockFault))||
    retained.hardware_errors||retained.underflows))
  e=Failed("retained allocation identity or health is uncertain");
 if(!e.ok())return fail(e);
 while(present) {
  bool done=false;e=Deadline(d);if(e.ok())e=operations_->Reap(&done);if(e.ok())e=Deadline(d);
  if(!e.ok())return fail(e);
  if(done)break;
  operations_->WaitMs(1);
 }
 for(;;) {
  bool idle=false;e=Deadline(d);if(e.ok())e=operations_->Idle(&idle);if(e.ok())e=Deadline(d);
  if(!e.ok())return fail(e);
  if(idle)return {};
  operations_->WaitMs(1);
 }
}
Error LinuxDe25Menu::OwnerIdle(std::uint64_t d,bool* idle) {
 if(!idle)return Failed("DE25 owner idle result is unavailable");
 std::lock_guard<std::mutex> lock(mutex_);
 Error e=Deadline(d);if(!e.ok())return e;
 e=operations_->Idle(idle);if(e.ok())e=Deadline(d);
 return e;
}
Error LinuxDe25Menu::Quiesce(std::uint64_t d,const CoreDescriptor* identity,MenuDisplayInfo* out) {
 std::lock_guard<std::mutex> lock(mutex_);Error e=Retire(d);
 if(!e.ok())return e;
 if(session_control_) {
  MenuDisplayInfo observed;e=session_control_->Quiesce(d,identity,&observed);
  if(e.ok())e=Deadline(d);
  // Successful OWNER_REAP already performed CLEAR. GP must observe zero;
  // the final logical completion was independently confirmed before retirement.
  // A typed configuration timeout is not a completion. Only the exact owned
  // pre-CLEAR receipt + successful REAP/close/OWNER_IDLE above permits dropping
  // its unpresented submission after GP independently confirms cold sequence0.
  if(e.ok()&&observed.displayed_sequence==0&&configuration_timeout_&&configuration_retired_) {
   submitted_=completed_;submitted_slot_=completed_slot_;
   configuration_timeout_=configuration_wait_=configuration_retired_=false;
  }
  if(e.ok()&&(observed.displayed_sequence!=0||completed_!=submitted_||
      completed_>std::numeric_limits<std::uint32_t>::max()-sequence_base_)) {
   std::ostringstream detail;
   detail<<"DE25 retired session counter was not cleared or publication was unconfirmed"
    <<"; observed="<<observed.displayed_sequence<<" completed="<<completed_
    <<" submitted="<<submitted_<<" base="<<sequence_base_;
   e={ErrorCode::io_failed,detail.str(),"menu"};
  }
  if(!e.ok())return Fault(e);
  session_quiesced_=true;
  observed.displayed_sequence=completed_+sequence_base_;if(out)*out=observed;
 } else if(out)Observation(out,true);
 return {};
}
} }
