// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/de25_menu.hpp"
#include "native/hardware.hpp"
#include "native/generated/fes_de25_scanout.hpp"
#include <cassert>
#include <limits>
#include <utility>
#include <vector>
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
namespace {
const std::array<std::uint32_t,4> build{{0x76543210,0xfedcba98,0x89abcdef,0x01234567}};
struct TestClock final : Clock {
 std::uint64_t now=0;
 std::uint64_t NowMs() const override{return now;}
};
struct Operations final : De25MenuOperations {
 TestClock clock;
 De25MenuGrant state;
 std::uint32_t abi=3;
 int abi_calls=0,allocations=0,polls=0,reaps=0,closes=0,shares=0;
 int drain_waits=0,peer_waits=0,peers=0,ready_waits=0,pending_polls=0;
 bool allocated=false,grant_open=false,ready=true,wrong_build=false;
 bool never_complete=false,reap_error=false,late_publish=false;
 std::vector<std::pair<std::uint32_t,std::uint32_t>> published;
 Operations(){Reset();}
 void Reset() {
  const auto generation=state.generation+1;state={};state.generation=generation;
  state.version=3;state.allocation_bytes=FesDe25ScanoutAllocationBytes;
  state.slot_bytes=FesDe25ScanoutSlotBytes;state.frame_bytes=FesDe25ScanoutFrameBytes;
  state.width=FesDe25ScanoutFrameWidth;state.height=FesDe25ScanoutFrameHeight;
  state.stride=FesDe25ScanoutFrameStride;state.slots=2;state.back_slot=1;
  state.pending_slot=~std::uint32_t(0);state.build_id=build;
  state.hardware_status=FesDe25ScanoutStatusDrained|(ready?FesDe25ScanoutStatusDisplayReady:0);
 }
 Error OwnerAbi(std::uint32_t* out) override {++abi_calls;*out=abi;return {};}
 Error Allocate(int* out) override {assert(!allocated&&!grant_open);++allocations;allocated=grant_open=true;Reset();*out=100;return {};}
 Error Info(int fd,De25MenuGrant* out) override {assert(fd==100&&grant_open);*out=state;if(wrong_build)++out->build_id[2];return {};}
 Error Poll(int fd,De25MenuGrant* out) override {
  assert(fd==100&&grant_open&&out->version==3&&out->generation==state.generation);++polls;
  if(!never_complete&&(state.state&8)&&++pending_polls>=3) {
   state.displayed_sequence=state.accepted_sequence;state.displayed_slot=state.pending_slot;
   state.pending_slot=~std::uint32_t(0);state.pending_sequence=0;state.back_slot=state.displayed_slot^1;
   state.state=2;state.hardware_status&=~FesDe25ScanoutStatusPending;
  }
  *out=state;if(wrong_build)++out->build_id[2];return {};
 }
 Error Publish(int fd,std::uint64_t generation,std::uint32_t sequence,std::uint32_t slot) override {
  assert(fd==100&&grant_open&&ready&&generation==state.generation&&state.back_slot==slot);
  published.emplace_back(sequence,slot);state.accepted_sequence=sequence;
  state.pending_sequence=sequence;state.pending_slot=slot;state.back_slot=~std::uint32_t(0);
  state.state=2|8;pending_polls=0;
  state.hardware_status=FesDe25ScanoutStatusConfigured|FesDe25ScanoutStatusRunning|FesDe25ScanoutStatusPending|FesDe25ScanoutStatusDisplayReady;
  if(late_publish)clock.now+=1000;
  return {};
 }
 Error Reap(bool* done) override {
  ++reaps;if(reap_error)return {ErrorCode::io_failed,"uncertain kernel drain","menu"};
  if(drain_waits){--drain_waits;*done=false;return {};}
  *done=true;allocated=false;return {};
 }
 Error Idle(bool* idle) override {
  if(!allocated&&!grant_open&&peer_waits){--peer_waits;*idle=false;return {};}
  *idle=!allocated&&!grant_open&&!peers;return {};
 }
 Error Duplicate(int fd,int* out) override {assert(fd==100&&grant_open);++shares;++peers;*out=200;return {};}
 void Close(int fd) override {++closes;if(fd==100)grant_open=false;else {assert(fd==200&&peers);--peers;}}
 void WaitMs(std::uint32_t ms) override {
  clock.now+=ms;
  if(ready_waits&&!--ready_waits){ready=true;state.hardware_status|=FesDe25ScanoutStatusDisplayReady;}
 }
};
void Start(LinuxDe25Menu& m,Operations& o) {
 assert(m.Bind(build,o.clock.now+100).ok());assert(m.Activate(true,false).ok());
 assert(m.Configure(o.clock.now+100).ok());assert(m.Enable(o.clock.now+100).ok());
}
void Complete(LinuxDe25Menu& m,Operations& o,std::uint32_t sequence) {
 MenuDisplayInfo info;
 do {assert(m.ReadInfo(o.clock.now+100,&info).ok());} while(info.pending);
 assert(info.displayed_sequence==sequence&&info.enabled&&info.configured&&!info.quiesced&&!info.faulted);
}
void AbiAndIdentityBeforeExposure() {
 Operations o;LinuxDe25Menu m(o.clock,o);
 assert(!m.Bind({},100).ok()&&o.abi_calls==0);
 o.abi=2;assert(m.Bind(build,100).code==ErrorCode::unsupported_interface&&o.allocations==0);
 o.abi=3;o.wrong_build=true;assert(m.Bind(build,100).code==ErrorCode::core_mismatch);
 assert(o.allocations==1&&o.published.empty()&&o.shares==0);
 assert(m.Revoke().ok()&&o.reaps==1&&!o.grant_open);
}
void ConfirmBlackAndReuseOneAllocation() {
 Operations o;LinuxDe25Menu m(o.clock,o);int fd=-1;
 assert(m.FrameMapping()&&!m.Share(&fd).ok());Start(m,o);
 assert(o.allocations==1&&(o.published==decltype(o.published){{1,1},{2,0}}));
 MenuDisplayInfo initial;assert(m.ReadInfo(100,&initial).ok()&&initial.displayed_sequence==0);
 assert(m.Share(&fd).ok()&&fd==200);o.Close(fd);
 for(std::uint32_t sequence=1;sequence<=3;++sequence) {
  auto slot=std::uint8_t(sequence&1U);assert(m.Clean(slot,{}).ok());
  assert(m.Submit(slot,sequence,100).ok());
  assert(!m.Clean(slot,{}).ok());Complete(m,o,sequence);
 }
 assert(o.published.size()==5&&o.published.back()==std::make_pair(5U,1U));
 assert(!m.Submit(0,0,100).ok());assert(!m.Submit(0,std::numeric_limits<std::uint32_t>::max(),100).ok());
 assert(o.published.size()==5);
 o.drain_waits=2;o.peer_waits=2;const auto start=o.clock.now;
 MenuDisplayInfo retired;assert(m.Quiesce(100,nullptr,&retired).ok());
 assert(o.clock.now>=start+4&&o.reaps==3&&!o.grant_open&&retired.quiesced&&!retired.enabled);
 assert(retired.displayed_sequence==3);
 assert(m.Bind(build,100).ok()&&o.allocations==2);assert(m.Revoke().ok());
}
void WaitForReadyBeforeAnyDma() {
 Operations o;o.ready=false;o.ready_waits=3;LinuxDe25Menu m(o.clock,o);
 assert(m.Bind(build,100).ok());assert(m.Activate(true,false).ok());
 assert(o.published.empty());assert(m.Configure(100).ok());assert(o.clock.now>=3&&o.published.size()==2);
 assert(m.Revoke().ok());
 Operations stuck;stuck.ready=false;LinuxDe25Menu n(stuck.clock,stuck);
 assert(n.Bind(build,100).ok()&&n.Activate(true,false).ok());assert(!n.Configure(5).ok());
 assert(stuck.clock.now==5&&stuck.published.empty());assert(n.Revoke().ok());
}
void RetainOnIdentityFaultAndUseKernelRetirement() {
 Operations o;LinuxDe25Menu m(o.clock,o);Start(m,o);o.wrong_build=true;
 MenuDisplayInfo info;assert(!m.ReadInfo(100,&info).ok());int fd=-1;
 assert(!m.Share(&fd).ok()&&fd==-1&&o.shares==0);
 assert(!m.Submit(1,1,100).ok()&&o.published.size()==2);
 o.drain_waits=2;assert(m.Revoke().ok()&&o.reaps==3);
}
void LatePublicationDoesNotAcknowledgeOrReplay() {
 Operations o;LinuxDe25Menu m(o.clock,o);Start(m,o);o.late_publish=true;
 assert(!m.Submit(1,1,o.clock.now+10).ok());const auto count=o.published.size();
 assert(!m.Submit(1,1,o.clock.now+10).ok()&&o.published.size()==count);
 assert(m.Revoke().ok());
}
void FailedRetirementRemainsSticky() {
 Operations o;
 {
  LinuxDe25Menu m(o.clock,o,5);Start(m,o);o.reap_error=true;
  assert(!m.Revoke().ok()&&o.reaps==1&&o.grant_open&&o.allocated);
  o.reap_error=false;assert(!m.Revoke().ok()&&o.reaps==1);
  assert(!m.Bind(build,100).ok()&&o.allocations==1);
 }
 assert(o.reaps==1&&o.allocated&&!o.grant_open); // Kernel owner retains uncertain storage.
}
void PeerReferencesCannotBecomeIdleByClosingRuntimeFd() {
 Operations o;LinuxDe25Menu m(o.clock,o,5);Start(m,o);int peer=-1;assert(m.Share(&peer).ok());
 assert(!m.Revoke().ok()&&!o.grant_open&&o.peers==1&&o.reaps==1);
 o.Close(peer);assert(!m.Revoke().ok()&&o.reaps==1); // Native lifecycle also keeps this failure sticky.
}
void UnsupportedPathsAndBounds() {
 Operations o;LinuxDe25Menu m(o.clock,o);assert(!m.Activate(false,false).ok());assert(!m.Activate(true,true).ok());
 assert(!o.allocations);Start(m,o);
 assert(!m.Clean(0,{}).ok());assert(!m.Clean(1,{{1279,0,2,1}}).ok());
 assert(!m.CleanBlit(0,1,{}).ok()&&!m.RunBlit(0,1,0,100).ok());
 assert(o.published.size()==2);assert(m.Revoke().ok());
}
}
int main(){AbiAndIdentityBeforeExposure();ConfirmBlackAndReuseOneAllocation();WaitForReadyBeforeAnyDma();RetainOnIdentityFaultAndUseKernelRetirement();LatePublicationDoesNotAcknowledgeOrReplay();FailedRetirementRemainsSticky();PeerReferencesCannotBecomeIdleByClosingRuntimeFd();UnsupportedPathsAndBounds();}
