// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/menu_display.hpp"
#include "native/fes_gp.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include "native/generated/fes_application.hpp"
#include <cassert>
#include <cstdio>
#include <array>
#include <vector>
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
class TestClock : public Clock { public: std::uint64_t NowMs() const override {return now++;} mutable std::uint64_t now=0; };
class Endpoint : public Mmio {
public:
 std::array<std::uint16_t,16> identity{{0x4546,0x3153,1,0,2,1,0,775,0x1100,0x3322,0x5544,0x7766,0x9988,0xbbaa,0xddcc,0xffee}};
 // Indices 14-17 are the picture rectangle a format-2 session plane reports.
 std::array<std::uint16_t,18> words{{1280,720,5120,16384,56,0,64,1,2,8,0x5678,0x1234,7,0,312,90,656,540}};
 std::vector<std::array<unsigned,3>> commands;
 std::uint32_t response=0xf5000000;
 bool toggle=false;
 bool fail_commit_read=false;
 std::uint16_t blit_state=0,blit_completed=0;
 Error Read32(std::uint32_t,std::uint32_t* out) override {
  if(fail_commit_read&&!commands.empty()&&commands.back()[0]==21&&commands.back()[1]==2){fail_commit_read=false;return {ErrorCode::io_failed,"lost submit ACK"};}
  *out=response;return {};
 }
 Error Write32(std::uint32_t,std::uint32_t word) override {
  bool next=bool(word&0x80000000u);if(next==toggle)return {};toggle=next;
  unsigned op=(word>>24)&127,index=(word>>16)&255,arg=word&65535;commands.push_back({{op,index,arg}});
  unsigned data=0;bool error=false;
  if(op==1){if(index>=identity.size()||arg){data=3;error=true;}else data=identity[index];}
  if(op==18){if(index>=words.size()||arg){data=3;error=true;}else data=words[index];}
  if(op==19) words[9]=9;
  if(op==20) words[9]=(arg?3:9)|(words[9]&16);
  if(op==22&&index==3)data=blit_state;
  if(op==22&&index==4)data=blit_completed;
  response=0xf5000000u|unsigned(toggle)*0x800000u|unsigned(error)*0x400000u|data;return {};
 }
};
void TestAmbiguousSessionSubmitOnlyRealignsAndQuiesces()
{
 Endpoint e;TestClock clock;FesGp gp(e,clock);MenuDisplayDriver display(gp,clock);
 CoreDescriptor identity;identity.abi={"fes.simple-computer",1,0};identity.build.id="00112233445566778899aabbccddeeff";
 identity.interfaces={{"fes.keyboard",1,0,true},{"fes.video.fixed-720p60",1,0,true},
  {"fes.media.blob",1,0,true},{"fes.memory.hps-ddr",1,0,true},{"fes.video.session-display",1,0,true}};
 e.words[9]=3;e.fail_commit_read=true;
 assert(!display.Submit(1,1,1000).ok()&&gp.Poisoned());
 const auto before=e.commands.size();e.words[9]=23;MenuDisplayInfo info;
 assert(display.Quiesce(1000,&identity,&info).ok()&&!gp.Poisoned());
 assert(info.quiesced&&!info.enabled&&!info.pending&&info.faulted);
 for(std::size_t i=before;i<e.commands.size();++i) {
  const auto opcode=e.commands[i][0];assert(opcode==1||opcode==18||opcode==20);
 }
 assert(e.commands[before+16][0]==20&&e.commands[before+16][2]==0);
}
void TestBlitRunAndState()
{
 Endpoint e;TestClock clock;FesGp gp(e,clock);MenuDisplayDriver display(gp,clock);
 // A run stages first, count and slot in order; its ACK means started.
 assert(display.RunBlit(32760,8,1,1000).ok());
 assert((e.commands==std::vector<std::array<unsigned,3>>{{{22,0,32760}},{{22,1,8}},{{22,2,1}}}));
 // The whole region is one list; anything outside it, or another slot, never reaches the GP.
 assert(display.RunBlit(0,32768,0,1000).ok());
 const auto before=e.commands.size();
 for(const auto& bad:std::vector<std::array<std::uint32_t,3>>{{{0,0,0}},{{32768,1,0}},{{32760,9,0}},{{0,1,2}}})
  assert(!display.RunBlit(bad[0],bad[1],std::uint8_t(bad[2]),1000).ok());
 assert(e.commands.size()==before);
 // The count is read before the state: it advances in the clock the run goes idle.
 MenuBlitState state;e.blit_state=1;e.blit_completed=0xffff;
 assert(display.ReadBlit(1000,&state).ok()&&state.busy&&!state.faulted&&state.completed==0xffff);
 assert(e.commands[e.commands.size()-2]==(std::array<unsigned,3>{{22,4,0}}));
 assert(e.commands.back()==(std::array<unsigned,3>{{22,3,0}}));
 e.blit_state=0x0402;
 assert(display.ReadBlit(1000,&state).ok()&&!state.busy&&state.faulted&&state.fault==4);
 // A fault code exactly when faulted, and no other bits.
 for(const std::uint16_t bad:{std::uint16_t(0x0002),std::uint16_t(0x0300),std::uint16_t(0x0004),std::uint16_t(0x0800)}) {
  e.blit_state=bad;assert(!display.ReadBlit(1000,&state).ok());
 }
 assert(!display.ReadBlit(1000,nullptr).ok());
}
void TestSessionPlaneReportsFormatAndPicture()
{
 Endpoint e;TestClock clock;FesGp gp(e,clock);MenuDisplayDriver display(gp,clock);MenuDisplayInfo info;
 assert(display.ReadInfo(1000,&info).ok()&&info.pixel_format==1&&!info.picture.w);
 assert(e.commands.size()==14);
 e.words[7]=2;e.commands.clear();
 assert(display.ReadInfo(1000,&info).ok()&&info.pixel_format==2);
 assert(info.picture.x==312&&info.picture.y==90&&info.picture.w==656&&info.picture.h==540);
 assert(e.commands.size()==18&&e.commands[14][1]==14&&e.commands[17][1]==17);
 e.words[16]=1000;assert(!display.ReadInfo(1000,&info).ok());
 e.words[16]=0;assert(!display.ReadInfo(1000,&info).ok());
 e.words[16]=656;e.words[7]=3;assert(!display.ReadInfo(1000,&info).ok());
}
int main(){
 TestAmbiguousSessionSubmitOnlyRealignsAndQuiesces();
 TestBlitRunAndState();
 TestSessionPlaneReportsFormatAndPicture();
 Endpoint e;TestClock clock;FesGp gp(e,clock);MenuDisplayDriver display(gp,clock);MenuDisplayInfo info;
 assert(display.ReadInfo(1000,&info).ok());assert(info.geometry.frame_bytes==3686400&&info.geometry.slot_bytes==4194304);
 assert(info.displayed_sequence==0x12345678u&&info.underflows==7&&info.quiesced&&!info.enabled);
 assert(display.Configure(1000).ok());assert(display.Enable(1000).ok());
 auto before=e.commands.size();assert(!display.Submit(2,1,1000).ok());assert(!display.Submit(0,0,1000).ok());assert(e.commands.size()==before);
 assert(display.Submit(1,0x12345678,1000).ok());
 auto n=e.commands.size();assert(e.commands[n-3]==(std::array<unsigned,3>{{21,0,0x5678}}));assert(e.commands[n-2]==(std::array<unsigned,3>{{21,1,0x1234}}));assert(e.commands[n-1]==(std::array<unsigned,3>{{21,2,1}}));
 assert(display.Quiesce(1000).ok());e.words[0]=640;before=e.commands.size();assert(!display.Configure(1000).ok());
 for(auto i=before;i<e.commands.size();++i)assert(e.commands[i][0]!=19);
 e.words[0]=1280;e.words[9]=0x8000;assert(!display.ReadInfo(1000,&info).ok());
 before=e.commands.size();assert(!display.ReadInfo(1000,nullptr).ok());assert(e.commands.size()==before);
 puts("menu driver geometry/counters/ordered submission/invalid-state/blit tests passed");
}
