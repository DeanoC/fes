// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/linux/de25_menu.hpp"
#include "native/hardware.hpp"
#include "native/generated/fes_de25_scanout.hpp"
#include <cassert>
#include <utility>
#include <vector>
namespace mister_test { namespace de25_menu {
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
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
} }
