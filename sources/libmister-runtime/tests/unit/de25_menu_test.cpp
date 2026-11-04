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
void RetainedAllocationRequiresIndependentDrainAndLastPeer() {
 for(bool present:{false,true}) {
  Operations o;o.retained=present;o.allocated=present;o.drain_waits=present?2:0;o.peer_waits=2;
  auto descriptor=mister_test::De25StandardDescriptor();LinuxDe25Menu m(o.clock,o);
  assert(m.RetireInherited(descriptor,100).ok());
  assert(o.owner_infos==1&&o.reaps==(present?3:0)&&!o.allocated&&o.allocations==0&&o.published.empty());
  assert(m.Bind(build,100).ok()&&o.allocations==1);assert(m.Revoke().ok());
 }
 for(int fault=0;fault<11;++fault) {
  Operations o;o.retained=o.allocated=true;auto descriptor=mister_test::De25StandardDescriptor();
  if(fault==0)o.abi=2;
  if(fault==1)o.wrong_build=true;
  if(fault==2)o.state.version=2;
  if(fault==3)o.state.reserved=1;
  if(fault==4)o.state.state=4;
  if(fault==5)o.state.hardware_status|=FesDe25ScanoutStatusFault;
  if(fault==6)o.state.hardware_status|=FesDe25ScanoutStatusClockFault;
  if(fault==7)o.state.underflows=1;
  if(fault==8)o.state.hardware_errors=1;
  if(fault==9)++o.state.slot_bytes;
  if(fault==10)o.info_error=true;
  LinuxDe25Menu m(o.clock,o);assert(!m.RetireInherited(descriptor,100).ok());
  assert(o.reaps==0&&o.allocations==0&&o.published.empty());assert(!m.Bind(build,100).ok());
 }
 for(bool peer:{false,true}) {
  Operations o;o.retained=o.allocated=true;o.drain_waits=peer?0:100;o.peers=peer?1:0;
  LinuxDe25Menu m(o.clock,o,3);auto descriptor=mister_test::De25StandardDescriptor();
  assert(!m.RetireInherited(descriptor,100).ok()&&o.clock.now==3);
  const auto reaps=o.reaps;o.drain_waits=o.peers=0;
  assert(!m.RetireInherited(descriptor,100).ok()&&o.reaps==reaps);
  assert(!m.Bind(build,100).ok()&&o.allocations==0);
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
   assert(!m.Quiesce(100,nullptr,&info).ok());
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
  {"fes.fabric.de25-scanout",1,1,true}};return d;
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
 for(unsigned kind=0;kind<7;++kind) {
  auto bad=d;
  if(kind==0)bad.interfaces[2]={"fes.memory.hps-ddr",1,0,true};
  if(kind==1)bad.interfaces[2].minor=0;
  if(kind==2)bad.interfaces[2].required=false;
  if(kind==3)bad.interfaces[2]=bad.interfaces[0];
  if(kind==4)bad.build.id=std::string(32,'0');
  if(kind==5)bad.build.id[1]='z';
  if(kind==6)bad.interfaces.push_back({"fes.video.menu-blitter",1,1,true});
  assert(!CheckDe25MenuContract(bad).ok());
  Operations o;LinuxDe25Menu menu(o.clock,o);De25MenuCoreDriver driver(menu);
  CoreDriverContext context;context.descriptor=&bad;context.expected_core="fes.menu";
  assert(!driver.Identify(context,100).error.ok()&&o.allocations==0&&o.abi_calls==0);
 }
}

}
int main(){RetainedAllocationRequiresIndependentDrainAndLastPeer();SessionRetirementConfirmsPendingFrameBeforeClear();SessionRetirementRefusesUnconfirmedOrUnclearedCounters();SelectedSessionContractBindsBeforeAnyExposure();SessionReopenUsesNewStorageAndMonotonicLogicalFrames();SessionPinnedPeerAndUncertainCloseBlockReopen();SessionUncertainPublicationIsNotRetried();SelectedMenuDriverUsesItsOwnReadiness();AllocatedMenuContractRejectsFixedDdrAndOldIdentity();AbiAndIdentityBeforeExposure();ConfirmBlackAndReuseOneAllocation();WaitForReadyBeforeAnyDma();RetainOnIdentityFaultAndUseKernelRetirement();FailedPollReportsCachedFaultWithoutRecovery();LatePublicationDoesNotAcknowledgeOrReplay();FailedRetirementRemainsSticky();PeerReferencesCannotBecomeIdleByClosingRuntimeFd();UnsupportedPathsAndBounds();}
