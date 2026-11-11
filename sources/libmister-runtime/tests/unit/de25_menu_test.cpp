// SPDX-License-Identifier: GPL-3.0-or-later
#include "de25_menu_operations.hpp"
#include "de25_standard_descriptor.hpp"
#include "native/de25_core_driver.hpp"
#include "native/de25_core_contract.hpp"
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
using namespace mister_test::de25_menu;
void Start(LinuxDe25Menu& m,Operations& o) {
 assert(m.Bind(build,o.clock.now+100).ok());assert(m.Activate(true,false).ok());
 assert(m.Configure(o.clock.now+100).ok());assert(m.Enable(o.clock.now+100).ok());
}
void Complete(LinuxDe25Menu& m,Operations& o,std::uint32_t sequence) {
 MenuDisplayInfo info;
 do {assert(m.ReadInfo(o.clock.now+100,&info).ok());} while(info.pending);
 assert(info.displayed_sequence==sequence&&info.enabled&&info.configured&&!info.quiesced&&!info.faulted);
}
void ConfigurationIntervalPreservesExactRunningGrant() {
 Operations o;LinuxDe25Menu m(o.clock,o);Start(m,o);
 assert(m.Submit(1,1,o.clock.now+100).ok());
 const auto generation=o.state.generation;
 o.never_complete=true;o.state.hardware_status&=~FesDe25ScanoutStatusDisplayReady;
 for(unsigned n=0;n<4;++n){MenuDisplayInfo info;assert(m.ReadInfo(o.clock.now+100,&info).ok());
  assert(info.pending&&info.enabled&&info.configured&&!info.faulted&&info.displayed_sequence==0);}
 assert(o.state.generation==generation&&o.allocations==1&&o.reaps==0&&o.published.size()==3);
 o.state.hardware_status|=FesDe25ScanoutStatusDisplayReady;o.never_complete=false;
 Complete(m,o,1);assert(o.allocations==1&&o.reaps==0);assert(m.Revoke().ok());
 for(unsigned fault=0;fault<7;++fault){
  Operations x;LinuxDe25Menu display(x.clock,x);Start(display,x);
  x.state.hardware_status&=~FesDe25ScanoutStatusDisplayReady;
  if(fault==0)x.wrong_build=true;
  if(fault==1)x.wrong_generation=true;
  if(fault==2)x.state.hardware_status|=FesDe25ScanoutStatusFault;
  if(fault==3)x.state.hardware_status|=FesDe25ScanoutStatusClockFault;
  if(fault==4)x.state.hardware_errors=0x80;
  if(fault==5)x.state.underflows=1;
  if(fault==6)x.state.hardware_status&=~FesDe25ScanoutStatusRunning;
  MenuDisplayInfo info;assert(!display.ReadInfo(x.clock.now+100,&info).ok());
 }
}
void ConfigurationTimeoutRequiresExactCleanRetirementReceipt() {
 for(unsigned fault=0;fault<16;++fault){
  Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o,5);MenuDisplayInfo info;
  assert(m.Bind(build,100,&gp).ok()&&m.Activate(true,false).ok()&&m.Configure(100).ok()&&m.Enable(100).ok());
  assert(m.Submit(1,1,100).ok());o.never_complete=true;o.ready=false;
  o.state.hardware_status&=~FesDe25ScanoutStatusDisplayReady;
  assert(m.ReadInfo(100,&info).ok()&&info.pending&&info.displayed_sequence==0);
  o.clock.now=101;assert(!m.ReadInfo(100,&info).ok());
  if(fault==1)o.retirement_missing=true;
  if(fault>=2&&fault<=13)o.retirement_mutation=fault-1;
  if(fault==14)o.reap_error=true; // Existing epoch/transport authority fails.
  if(fault==15)o.on_reap=[&]{o.state.hardware_errors=0x8;o.state.underflows=1;};
  const auto result=m.Quiesce(200,nullptr,&info);
  if(fault==0){
   assert(result.ok()&&info.displayed_sequence==0&&info.quiesced);
   assert(o.retirement_queries==1&&!o.grant_open&&!o.allocated&&o.published.size()==1);
   o.ready=true;o.never_complete=false;
   assert(m.Activate(false,false).ok()&&o.allocations==2);
   assert(m.ReadInfo(200,&info).ok()&&info.displayed_sequence==0);
   assert(m.Enable(200).ok()&&m.Submit(1,1,200).ok());
   Complete(m,o,1);assert(o.published.size()==2);assert(m.Revoke().ok());
  }else{
   assert(!result.ok()&&!m.Activate(false,false).ok()&&o.allocations==1&&o.published.size()==1);
   if(fault==14)assert(o.allocated&&o.retirement_queries==0);
   else assert(!o.allocated&&!o.grant_open&&o.retirement_queries==1);
  }
 }
}
void ConfigurationDeadlineDuringSessionInfoRequiresCleanRetirement() {
 // Refresh succeeds before the deadline; the independent GP observation
 // consumes the last budget. A late healthy read is a deadline, not an ACK.
 for(unsigned fault=0;fault<4;++fault) {
  Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o,5);MenuDisplayInfo info;
  assert(m.Bind(build,100,&gp).ok()&&m.Activate(true,false).ok()&&m.Configure(100).ok()&&m.Enable(100).ok());
  assert(m.Submit(1,1,100).ok());o.never_complete=true;o.ready=false;
  o.state.hardware_status&=~FesDe25ScanoutStatusDisplayReady;
  bool observed=false;
  gp.on_info=[&](MenuDisplayInfo* value) {
   observed=true;assert(o.clock.now<100);o.clock.now=101;
   if(fault==1)value->underflows=1;
   if(fault==2)value->faulted=true;
  };
  const auto error=m.ReadInfo(100,&info);gp.on_info={};
  assert(observed&&!error.ok());
  if(fault==3)o.on_reap=[&]{o.state.hardware_errors=8;o.state.underflows=1;};
  const auto result=m.Quiesce(200,nullptr,&info);
  if(fault==0) {
   assert(result.ok()&&info.displayed_sequence==0&&o.retirement_queries==1&&!o.allocated);
   o.ready=true;o.never_complete=false;
   assert(m.Activate(false,false).ok()&&m.Enable(200).ok());
   assert(m.Submit(1,1,200).ok());Complete(m,o,1);assert(m.Revoke().ok());
  } else {
   assert(!result.ok()&&!m.Activate(false,false).ok()&&o.allocations==1);
   assert(o.retirement_queries==(fault==3?1u:0u));
  }
 }
}
void ConfigurationCancellationPreservesNonzeroLogicalHistory() {
 Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o,5);MenuDisplayInfo info;
 assert(m.Bind(build,100,&gp).ok()&&m.Activate(true,false).ok()&&m.Configure(100).ok()&&m.Enable(100).ok());
 assert(m.Submit(1,1,100).ok());Complete(m,o,1);
 assert(m.Quiesce(100,nullptr,&info).ok()&&info.displayed_sequence==1);
 assert(m.Activate(false,false).ok()&&m.Enable(100).ok());
 assert(m.Submit(1,2,100).ok());o.never_complete=true;o.ready=false;
 o.state.hardware_status&=~FesDe25ScanoutStatusDisplayReady;
 assert(m.ReadInfo(100,&info).ok()&&info.displayed_sequence==1&&info.pending);
 o.clock.now=101;assert(!m.ReadInfo(100,&info).ok());
 assert(m.Quiesce(200,nullptr,&info).ok()&&info.displayed_sequence==1);
 o.ready=true;o.never_complete=false;
 assert(m.Activate(false,false).ok()&&m.Enable(200).ok());
 assert(m.ReadInfo(200,&info).ok()&&info.displayed_sequence==1&&!info.pending);
 assert(m.Submit(1,2,200).ok());Complete(m,o,2);
 assert(o.allocations==3&&o.published.size()==3&&m.Revoke().ok());
}
void RetainedAllocationRequiresIndependentDrainAndLastPeer() {
 for(std::uint32_t state:{0U,2U,4U,6U,8U,10U,12U,14U}) {
  Operations o;o.retained=o.allocated=true;o.state.state=state;
  o.drain_waits=2;o.peer_waits=2;
  LinuxDe25Menu m(o.clock,o);auto descriptor=mister_test::De25StandardDescriptor();
  assert(m.RetireInherited(descriptor,100).ok());
  assert(o.reaps==3&&o.clock.now==4&&!o.allocated&&o.allocations==0&&o.published.empty());
  assert(m.Bind(build,100).ok()&&o.allocations==1);assert(m.Revoke().ok());
 }
 for(bool present:{false,true}) {
  Operations o;o.retained=present;o.allocated=present;o.state.state=1;o.drain_waits=0;o.peer_waits=2;
  auto descriptor=mister_test::De25StandardDescriptor();LinuxDe25Menu m(o.clock,o);
  assert(m.RetireInherited(descriptor,100).ok());
  assert(o.owner_infos==1&&o.reaps==(present?1:0)&&!o.allocated&&o.allocations==0&&o.published.empty());
  assert(m.Bind(build,100).ok()&&o.allocations==1);assert(m.Revoke().ok());
 }
 for(int fault=0;fault<12;++fault) {
  Operations o;o.retained=o.allocated=true;o.state.state=1;auto descriptor=mister_test::De25StandardDescriptor();
  if(fault==0)o.abi=2;
  if(fault==1)o.wrong_build=true;
  if(fault==2)o.state.version=2;
  if(fault==3)o.state.reserved=1;
  if(fault==4)o.state.hardware_status|=FesDe25ScanoutStatusFault;
  if(fault==5)o.state.hardware_status|=FesDe25ScanoutStatusClockFault;
  if(fault==6)o.state.underflows=1;
  if(fault==7)o.state.hardware_errors=1;
  if(fault==8)++o.state.slot_bytes;
  if(fault==9)o.info_error=true;
  if(fault==10)o.state.state=1|16; // Unknown state bits cannot authorize kernel retirement.
  if(fault==11)o.state.hardware_status|=1U<<31;
  LinuxDe25Menu m(o.clock,o);assert(!m.RetireInherited(descriptor,100).ok());
  assert(o.reaps==0&&o.allocations==0&&o.published.empty());assert(!m.Bind(build,100).ok());
 }
 for(bool peer:{false,true}) {
  Operations o;o.retained=o.allocated=true;o.state.state=1;o.drain_waits=peer?0:100;o.peers=peer?1:0;
  LinuxDe25Menu m(o.clock,o,3);auto descriptor=mister_test::De25StandardDescriptor();
  assert(!m.RetireInherited(descriptor,100).ok()&&o.clock.now==3);
  const auto reaps=o.reaps;o.drain_waits=o.peers=0;
  assert(!m.RetireInherited(descriptor,100).ok()&&o.reaps==reaps);
  assert(!m.Bind(build,100).ok()&&o.allocations==0);
 }
 for(bool late:{false,true}) {
  Operations o;o.retained=o.allocated=true;o.state.state=1|2|4|8;
  if(late)o.on_reap=[&]{o.clock.now=101;};else o.reap_error=true;
  LinuxDe25Menu m(o.clock,o);auto descriptor=mister_test::De25StandardDescriptor();
  assert(!m.RetireInherited(descriptor,100).ok()&&o.reaps==1&&o.allocations==0);
  o.on_reap={};o.reap_error=false;
  assert(!m.RetireInherited(descriptor,200).ok()&&o.reaps==1);
  assert(!m.Bind(build,200).ok()&&o.allocations==0);
 }
 Operations o;LinuxDe25Menu m(o.clock,o);Start(m,o);const auto reaps=o.reaps;
 assert(!m.RetireInherited(mister_test::De25StandardDescriptor(),100).ok()&&o.reaps==reaps);
 assert(m.Revoke().ok());
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
 assert(m.FrameMapping()&&m.FreshSlotsOnActivate()&&!m.Share(&fd).ok());Start(m,o);
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
void FailedPollReportsCachedFaultWithoutRecovery() {
 for(unsigned kind=0;kind<5;++kind) {
  Operations o;LinuxDe25Menu m(o.clock,o);Start(m,o);
  o.poll_error=true;o.state.state|=4;o.state.hardware_status|=FesDe25ScanoutStatusFault;
  o.state.hardware_errors=0x20;o.state.underflows=7;o.state.outstanding=2;
  if(kind==1)o.info_error=true;
  if(kind==2)o.wrong_build=true;
  if(kind==3)o.state.version=2;
  if(kind==4)o.poll_error_delay=1000;
  const auto infos=o.infos,polls=o.polls,closes=o.closes;
  MenuDisplayInfo info;const auto error=m.ReadInfo(o.clock.now+100,&info);
  assert(error.code==ErrorCode::io_failed&&error.phase=="menu");
  assert(error.message.find("DE25 grant poll: Input/output error")==0);
  if(kind==0) {
   assert(error.message.find("cached grant generation="+std::to_string(o.state.generation))!=std::string::npos);
   assert(error.message.find("hardware_errors=0x20 underflows=7 outstanding=2")!=std::string::npos);
  } else assert(error.message.find("cached grant")==std::string::npos);
  assert(o.infos==infos+(kind==4?0:1)&&o.polls==polls+1);
  assert(o.published.size()==2&&o.reaps==0&&o.closes==closes);
  const auto final_infos=o.infos,final_polls=o.polls;int peer=-1;
  assert(!m.ReadInfo(o.clock.now+100,&info).ok()&&!m.Share(&peer).ok());
  assert(!m.Submit(1,1,o.clock.now+100).ok()&&peer==-1);
  assert(o.infos==final_infos&&o.polls==final_polls&&o.published.size()==2&&o.reaps==0);
  assert(m.Revoke().ok());
 }
}
// Keep existing fakes on the default-unavailable diagnostic operation. This
// wrapper injects only the optional cache and its observation boundaries.
struct DiagnosticOperations final : De25MenuOperations {
 Operations base;
 De25MenuDiagnostics diagnostic;
 unsigned diagnostics=0;
 std::uint32_t info_delay=0,diagnostic_delay=0;
 bool unavailable=false,wrong_info_generation=false;
 Error OwnerAbi(std::uint32_t* out) override{return base.OwnerAbi(out);}
 Error OwnerInfo(bool* present,De25MenuGrant* out) override{return base.OwnerInfo(present,out);}
 Error Allocate(int* out) override{return base.Allocate(out);}
 Error Info(int fd,De25MenuGrant* out) override {
  const auto e=base.Info(fd,out);base.clock.now+=info_delay;
  if(wrong_info_generation)++out->generation;
  return e;
 }
 Error Poll(int fd,De25MenuGrant* out) override{return base.Poll(fd,out);}
 Error Diagnostics(int fd,std::uint64_t generation,De25MenuDiagnostics* out) override {
  assert(fd==100&&base.grant_open&&generation==base.state.generation);++diagnostics;
  base.clock.now+=diagnostic_delay;*out=diagnostic;
  return unavailable?Error{ErrorCode::io_failed,"DE25 cached diagnostics: Inappropriate ioctl for device","menu"}:Error{};
 }
 Error Publish(int fd,std::uint64_t generation,std::uint32_t sequence,std::uint32_t slot) override{return base.Publish(fd,generation,sequence,slot);}
 Error Reap(bool* out) override{return base.Reap(out);}
 Error Idle(bool* out) override{return base.Idle(out);}
 Error Duplicate(int fd,int* out) override{return base.Duplicate(fd,out);}
 void Close(int fd) override{base.Close(fd);}
 void WaitMs(std::uint32_t ms) override{base.WaitMs(ms);}
};
void FailedPollReportsOnlyVerifiedFirstFault() {
 for(unsigned kind=0;kind<23;++kind) {
  DiagnosticOperations o;LinuxDe25Menu m(o.base.clock,o);Start(m,o.base);
  MenuDisplayInfo info;assert(m.ReadInfo(o.base.clock.now+100,&info).ok()&&o.diagnostics==0);
  auto& v=o.diagnostic;v.generation=o.base.state.generation;v.version=1;v.build_id=build;v.flags=1;
  // An absent pixel at a real raster position, before the failed frame's
  // cancellation. Credit excludes commands still held under waitrequest.
  v.first_fault_state=0x80000000U|(191U<<20)|(37U<<10)|(3U<<8)|(2U<<5)|0x1aU;
  v.first_fault_occupancy=(2U<<21)|(17U<<12)|123U;
  v.first_fault_gaps=(29U<<16)|43U;
  o.base.poll_error=true;o.base.state.state|=4;
  o.base.state.hardware_status|=FesDe25ScanoutStatusFault;
  o.base.state.hardware_errors=0x20;o.base.state.underflows=7;o.base.state.outstanding=2;
  if(kind==1)o.unavailable=true; // Includes old kernels returning ENOTTY.
  if(kind==2)v.version=2;
  if(kind==3)v.reserved=1;
  if(kind==4)++v.generation;
  if(kind==5)++v.build_id[2];
  if(kind==6){v.flags=0;v.first_fault_state=v.first_fault_occupancy=v.first_fault_gaps=0;}
  if(kind==7)v.flags|=2;
  if(kind==8)v.first_fault_state&=~0x80000000U;
  if(kind==9)v.first_fault_occupancy|=0x80000000U;
  if(kind==10)v.first_fault_state=(v.first_fault_state&~(0x7ffU<<20))|(1280U<<20);
  if(kind==11)v.first_fault_state=(v.first_fault_state&~(0x3ffU<<10))|(720U<<10);
  if(kind==12)v.first_fault_state|=3U<<5;
  if(kind==13)v.first_fault_state=v.first_fault_occupancy=v.first_fault_gaps=0;
  if(kind==14)o.base.poll_error_delay=1000;
  if(kind==15)o.info_delay=1000;
  if(kind==16)o.diagnostic_delay=1000;
  if(kind==17)o.wrong_info_generation=true;
  if(kind==18)o.base.state.version=2;
  if(kind==19)o.base.wrong_build=true;
  if(kind==20)o.base.info_error=true;
  if(kind==21) {
   // All packed count/lane fields are representationally valid. Do not
   // impose a particular reader's FIFO depth on a cached diagnostic.
   v.first_fault_state=0x80000000U|(1279U<<20)|(719U<<10)|(2U<<8)|0x9fU;
   v.first_fault_occupancy=0x007fffffU;v.first_fault_gaps=0xffffffffU;
  }
  if(kind==22)v.generation=0;
  const auto infos=o.base.infos,polls=o.base.polls,closes=o.base.closes;
  const auto error=m.ReadInfo(o.base.clock.now+100,&info);
  assert(error.code==ErrorCode::io_failed&&error.phase=="menu");
  assert(error.message.find("DE25 grant poll: Input/output error")==0);
  const bool verified_info=kind!=14&&kind!=17&&kind!=18&&kind!=19&&kind!=20;
  assert((error.message.find("hardware_errors=0x20 underflows=7 outstanding=2")!=std::string::npos)==verified_info);
  assert(o.diagnostics==unsigned(verified_info&&kind!=15));
  assert(o.base.infos==infos+(kind==14?0:1)&&o.base.polls==polls+1);
  if(kind==0) {
   assert(error.message.find("cached first_underflow raw=0x8bf0975a/0x41107b/0x1d002b")!=std::string::npos);
   assert(error.message.find("h=191 v=37 video_state=display fifo=123 accepted_outstanding=17 lane=2 relation=greater")!=std::string::npos);
   assert(error.message.find("pixel_valid=0 read=1 waitrequest=1 readdatavalid=0 response=2")!=std::string::npos);
   assert(error.message.find("max_response_gap_cycles=43 max_held_command_age_cycles=29")!=std::string::npos);
  } else if(kind==21) {
   assert(error.message.find("h=1279 v=719 video_state=fetch fifo=4095 accepted_outstanding=511 lane=3 relation=less")!=std::string::npos);
   assert(error.message.find("max_response_gap_cycles=65535 max_held_command_age_cycles=65535")!=std::string::npos);
  } else assert(error.message.find("cached first_underflow")==std::string::npos);
  const auto diagnostic_calls=o.diagnostics;
  const auto final_infos=o.base.infos,final_polls=o.base.polls;int peer=-1;
  assert(!m.ReadInfo(o.base.clock.now+100,&info).ok()&&!m.Share(&peer).ok()&&peer==-1);
  assert(!m.Submit(1,1,o.base.clock.now+100).ok());
  assert(o.diagnostics==diagnostic_calls&&o.base.infos==final_infos&&o.base.polls==final_polls);
  assert(o.base.published.size()==2&&o.base.reaps==0&&o.base.closes==closes);
  assert(m.Revoke().ok());
 }
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
 assert(o.reaps==1&&o.allocated&&!o.grant_open); // Model retains storage; the real file release may attempt another guarded revoke, never an unchecked free.
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
void SelectedSessionContractBindsBeforeAnyExposure() {
 Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o,gp);
 auto d=mister_test::De25StandardDescriptor();
 for(auto minor:{0u,1u,2u,4u}) {
  auto unsupported=d;unsupported.interfaces.back().minor=minor;
  assert(!m.BindSession(unsupported,100).ok()&&o.abi_calls==0&&o.allocations==0);
 }
 auto bad=d;bad.interfaces.back()={"fes.memory.hps-ddr",1,0,true};
 assert(!m.BindSession(bad,100).ok()&&o.abi_calls==0&&o.allocations==0);
 assert(m.BindSession(d,100).ok()&&o.allocations==1&&o.published.empty());
 assert(m.Activate(true,false).ok()&&m.Configure(100).ok());
 assert(m.Activate(false,false).ok()&&o.allocations==1&&o.published.empty());
 MenuDisplayInfo info;assert(m.ReadInfo(100,&info).ok()&&!info.enabled&&info.quiesced);
 assert(m.Quiesce(100,&d,&info).ok()&&gp.submits==0);
}

void SessionReopenUsesNewStorageAndMonotonicLogicalFrames() {
 Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o);MenuDisplayInfo info;
 assert(m.Bind(build,100,&gp).ok()&&m.Activate(true,false).ok()&&m.Configure(100).ok());
 assert(o.published.empty()&&m.ReadInfo(100,&info).ok());
 assert(info.configured&&!info.enabled&&info.quiesced&&info.pixel_format==2&&info.displayed_sequence==0);
 assert(m.Enable(100).ok()&&o.published.empty());
 int peer=-1;assert(m.Share(&peer).ok());o.Close(peer);
 std::uint32_t logical=0;
 for(unsigned cycle=0;cycle<3;++cycle) {
  const auto generation=o.state.generation;
  // Odd counts force the old logical front slot to differ from the new
  // allocation's slot0. Each new grant must nevertheless publish slot1 first.
  for(std::uint32_t physical=1;physical<=3;++physical) {
   const auto slot=std::uint8_t(physical&1U);
   assert(m.Clean(slot,{}).ok()&&m.Submit(slot,++logical,100).ok());
   do {assert(m.ReadInfo(100,&info).ok());} while(info.pending);
   assert(info.displayed_sequence==logical&&info.enabled&&!info.quiesced);
   assert(o.published.back()==std::make_pair(physical,std::uint32_t(slot)));
  }
  o.drain_waits=2;assert(m.Revoke().ok());
  assert(!m.Activate(false,false).ok()); // STOP alone does not prove the plane hidden.
  assert(m.Quiesce(100,nullptr,&info).ok()&&info.displayed_sequence==logical&&info.quiesced);
  assert(!m.Share(&peer).ok()&&!m.Clean(1,{}).ok());
  if(cycle!=2) {
   assert(m.Activate(false,false).ok()&&o.state.generation>generation);
   assert(o.state.displayed_sequence==0&&o.state.back_slot==1);
   assert(m.Enable(100).ok()&&m.ReadInfo(100,&info).ok());
   assert(info.displayed_sequence==logical&&!info.quiesced);
   const auto count=o.published.size();
   assert(!m.Submit(1,logical,100).ok()&&o.published.size()==count);
  }
 }
 assert(o.allocations==3&&gp.quiesces==3&&gp.enables==3&&gp.submits==0);
}
void SessionPinnedPeerAndUncertainCloseBlockReopen() {
 for(bool pinned:{false,true}) {
  Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o,5);MenuDisplayInfo info;
  assert(m.Bind(build,100,&gp).ok()&&m.Activate(true,false).ok()&&m.Configure(100).ok()&&m.Enable(100).ok());
  int peer=-1;
  if(pinned)assert(m.Share(&peer).ok());else gp.fail_quiesce=true;
  assert(!m.Quiesce(10,nullptr,&info).ok());
  if(pinned)o.Close(peer);
  assert(!m.Activate(false,false).ok()&&o.allocations==1&&o.published.empty());
  assert(gp.quiesces==(pinned?0u:1u));
 }
}
void SessionRetirementConfirmsPendingFrameBeforeClear() {
 Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o);MenuDisplayInfo info;
 assert(m.Bind(build,100,&gp).ok()&&m.Activate(true,false).ok()&&m.Configure(100).ok()&&m.Enable(100).ok());
 assert(m.Submit(1,1,100).ok()&&(o.state.state&8));
 const auto polls=o.polls;
 assert(m.Revoke().ok()&&o.polls>polls&&o.state.displayed_sequence==0);
 assert(m.Quiesce(100,nullptr,&info).ok()&&info.displayed_sequence==1);
 assert(o.published.size()==1&&m.Activate(false,false).ok());
 assert(m.ReadInfo(100,&info).ok()&&info.displayed_sequence==1);
}
void SessionRetirementRefusesUnconfirmedOrUnclearedCounters() {
 for(bool stuck:{false,true}) {
  Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o,5);MenuDisplayInfo info;
  assert(m.Bind(build,100,&gp).ok()&&m.Activate(true,false).ok()&&m.Configure(100).ok()&&m.Enable(100).ok());
  o.never_complete=stuck;
  assert(m.Submit(1,1,100).ok());
  if(stuck) {
   assert(!m.Revoke().ok()&&o.reaps==0);
   o.never_complete=false;
   assert(!m.Revoke().ok()&&o.reaps==0&&o.published.size()==1);
  } else {
   assert(m.Revoke().ok());gp.stale_retired_counter=true;
   const auto error=m.Quiesce(100,nullptr,&info);
   assert(!error.ok());
   assert(error.message.find("observed=1 completed=1 submitted=1 base=0")!=std::string::npos);
  }
  assert(!m.Activate(false,false).ok()&&o.allocations==1);
 }
}
void SessionUncertainPublicationIsNotRetried() {
 Operations o;SessionControl gp(o);LinuxDe25Menu m(o.clock,o);MenuDisplayInfo info;
 assert(m.Bind(build,100,&gp).ok()&&m.Activate(true,false).ok()&&m.Configure(100).ok()&&m.Enable(100).ok());
 o.late_publish=true;
 assert(!m.Submit(1,1,10).ok());
 assert(!m.Submit(1,1,o.clock.now+100).ok()&&o.published.size()==1&&gp.submits==0);
 assert(!m.Activate(false,false).ok()&&o.allocations==1);
 assert(m.Revoke().ok());
}

CoreDescriptor MenuDescriptor() {
 CoreDescriptor d;d.format=2;d.core.id="fes.menu";
 d.target.platform="de25_nano";d.target.device="A5EB013BB23BE4SCS";
 d.target.programming_profile="de25-linux-menu-v1";
 d.abi={"fes.application",1,0};d.build.id="0123456789abcdeffedcba9876543210";
 d.interfaces={{"fes.video.fixed-720p60",1,0,true},{"fes.video.menu-display",1,0,true},
  {"fes.fabric.de25-scanout",1,3,true}};return d;
}
void SelectedMenuDriverUsesItsOwnReadiness() {
 Operations o;LinuxDe25Menu menu(o.clock,o);De25MenuCoreDriver driver(menu);De25VideoBringup video;
 auto d=MenuDescriptor();CoreDriverContext context;context.descriptor=&d;context.expected_core="fes.menu";
 o.abi=2;assert(!menu.CheckOwnerAbi(100).ok()&&o.allocations==0);
 o.abi=3;assert(menu.CheckOwnerAbi(100).ok()&&o.allocations==0);
 assert(!video.BringUpSelected(driver,100).error.ok()&&o.published.empty());
 assert(driver.Identify(context,100).error.ok());
 assert(o.allocations==1&&o.published.empty());
 assert(!driver.Start(context,100).error.ok());
 assert(menu.Activate(true,false).ok()&&menu.Configure(100).ok());
 assert(video.BringUpSelected(driver,100).error.ok()&&driver.Start(context,100).error.ok());
 assert(o.published.size()==2&&driver.NeutralizeButtons(context,100).error.ok());
 assert(!driver.SetButtons(context,1,100).error.ok());
 const auto polls=o.polls;d.build.id[0]='f';
 assert(!driver.Start(context,100).error.ok()&&o.polls==polls);d=MenuDescriptor();
 assert(driver.Quiesce(context,100).error.ok()&&o.reaps==1&&!o.allocated);
 driver.BeginSession();assert(!driver.Start(context,100).error.ok());
}
void AllocatedMenuContractRejectsFixedDdrAndOldIdentity() {
 auto d=MenuDescriptor();assert(CheckDe25MenuContract(d).ok());
 std::array<std::uint32_t,4> parsed{};assert(De25BuildWords(d.build.id,&parsed).ok()&&parsed==build);
 for(unsigned kind=0;kind<10;++kind) {
  auto bad=d;
  if(kind==0)bad.interfaces[2]={"fes.memory.hps-ddr",1,0,true};
  if(kind==1)bad.interfaces[2].minor=0;
  if(kind==2)bad.interfaces[2].required=false;
  if(kind==3)bad.interfaces[2]=bad.interfaces[0];
  if(kind==4)bad.build.id=std::string(32,'0');
  if(kind==5)bad.build.id[1]='z';
  if(kind==6)bad.interfaces.push_back({"fes.video.menu-blitter",1,1,true});
  if(kind==7)bad.interfaces[2].minor=1;
  if(kind==8)bad.interfaces[2].minor=2;
  if(kind==9)bad.interfaces[2].minor=4;
  assert(!CheckDe25MenuContract(bad).ok());
  Operations o;LinuxDe25Menu menu(o.clock,o);De25MenuCoreDriver driver(menu);
  CoreDriverContext context;context.descriptor=&bad;context.expected_core="fes.menu";
  assert(!driver.Identify(context,100).error.ok()&&o.allocations==0&&o.abi_calls==0);
 }
}

}
int main(){ConfigurationDeadlineDuringSessionInfoRequiresCleanRetirement();ConfigurationCancellationPreservesNonzeroLogicalHistory();ConfigurationTimeoutRequiresExactCleanRetirementReceipt();ConfigurationIntervalPreservesExactRunningGrant();RetainedAllocationRequiresIndependentDrainAndLastPeer();SessionRetirementConfirmsPendingFrameBeforeClear();SessionRetirementRefusesUnconfirmedOrUnclearedCounters();SelectedSessionContractBindsBeforeAnyExposure();SessionReopenUsesNewStorageAndMonotonicLogicalFrames();SessionPinnedPeerAndUncertainCloseBlockReopen();SessionUncertainPublicationIsNotRetried();SelectedMenuDriverUsesItsOwnReadiness();AllocatedMenuContractRejectsFixedDdrAndOldIdentity();AbiAndIdentityBeforeExposure();ConfirmBlackAndReuseOneAllocation();WaitForReadyBeforeAnyDma();RetainOnIdentityFaultAndUseKernelRetirement();FailedPollReportsCachedFaultWithoutRecovery();FailedPollReportsOnlyVerifiedFirstFault();LatePublicationDoesNotAcknowledgeOrReplay();FailedRetirementRemainsSticky();PeerReferencesCannotBecomeIdleByClosingRuntimeFd();UnsupportedPathsAndBounds();}
