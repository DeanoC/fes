// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/menu_display.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/fes_application.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/generated/fes_display_plane.hpp"
#include <array>
namespace mister { namespace native {
namespace {
using namespace generated;
static_assert(FesDisplayPlaneInfoPictureHeightIndex==FesDisplayPlaneInfoPictureLeftIndex+3,
 "picture indices are consecutive");
static_assert(FesSimpleComputerOpcodeMenuInfo==FesApplicationOpcodeMenuInfo&&
 FesSimpleComputerOpcodeMenuConfigure==FesApplicationOpcodeMenuConfigure&&
 FesSimpleComputerOpcodeMenuControl==FesApplicationOpcodeMenuControl&&
 FesSimpleComputerOpcodeMenuSubmit==FesApplicationOpcodeMenuSubmit&&
 FesSimpleComputerMenuWidth==FesApplicationMenuWidth&&
 FesSimpleComputerMenuHeight==FesApplicationMenuHeight&&
 FesSimpleComputerMenuStride==FesApplicationMenuStride&&
 FesSimpleComputerMenuFrameBytes==FesApplicationMenuFrameBytes&&
 FesSimpleComputerMenuSlotBytes==FesApplicationMenuSlotBytes&&
 FesSimpleComputerMenuLayout==FesApplicationMenuLayout,
 "idle and session displays share one physical framebuffer layout and mailbox");
Error Invalid(const char* text) {return {ErrorCode::io_failed,text,"menu"};}
}
MenuDisplayDriver::MenuDisplayDriver(FesGp& gp,Clock&) : gp_(gp) {}
Error MenuDisplayDriver::Command(std::uint8_t op,std::uint8_t index,std::uint16_t argument,std::uint64_t deadline)
{
 std::uint16_t response=0;
 return gp_.Exchange(op,index,argument,deadline,&response);
}
Error MenuDisplayDriver::ReadInfo(std::uint64_t deadline,MenuDisplayInfo* output)
{
 if(!output)return Invalid("missing menu information output");
 std::array<std::uint16_t,14> words{};
 for(std::uint8_t i=0;i<words.size();++i){
  Error error=gp_.Exchange(FesApplicationOpcodeMenuInfo,i,0,deadline,&words[i]);
  if(!error.ok())return error;
 }
 MenuDisplayInfo info;
 info.geometry={words[0],words[1],words[2],std::uint32_t(words[3])|(std::uint32_t(words[4])<<16),std::uint32_t(words[5])|(std::uint32_t(words[6])<<16)};
 const auto& geometry=info.geometry;
 if(geometry.width!=FesApplicationMenuWidth||geometry.height!=FesApplicationMenuHeight||
    geometry.stride!=FesApplicationMenuStride||geometry.frame_bytes!=FesApplicationMenuFrameBytes||
    geometry.slot_bytes!=FesApplicationMenuSlotBytes||
    (words[7]!=FesApplicationMenuPixelFormat&&words[7]!=FesDisplayPlanePixelFormatPremultipliedRgba)||
    words[8]!=FesApplicationMenuSlotCount||words[9]&~std::uint16_t(31))
  return Invalid("menu geometry or state does not match the supported fixed layout");
 info.pixel_format=words[7];
 if(info.pixel_format==FesDisplayPlanePixelFormatPremultipliedRgba) {
  std::array<std::uint16_t,4> picture{};
  for(std::uint8_t i=0;i<picture.size();++i){
   Error error=gp_.Exchange(FesApplicationOpcodeMenuInfo,std::uint8_t(FesDisplayPlaneInfoPictureLeftIndex+i),0,deadline,&picture[i]);
   if(!error.ok())return error;
  }
  info.picture={picture[0],picture[1],picture[2],picture[3]};
  if(!picture[2]||!picture[3]||std::uint32_t(picture[0])+picture[2]>geometry.width||
     std::uint32_t(picture[1])+picture[3]>geometry.height)
   return Invalid("session display picture is outside the fixed layout");
 }
 info.configured=words[9]&FesApplicationMenuStateConfigured;
 info.enabled=words[9]&FesApplicationMenuStateEnabled;
 info.pending=words[9]&FesApplicationMenuStatePending;
 info.quiesced=words[9]&FesApplicationMenuStateQuiesced;
 info.faulted=words[9]&FesApplicationMenuStateFaulted;
 info.displayed_sequence=std::uint32_t(words[10])|(std::uint32_t(words[11])<<16);
 info.underflows=std::uint32_t(words[12])|(std::uint32_t(words[13])<<16);
 *output=info;return {};
}
Error MenuDisplayDriver::Configure(std::uint64_t deadline)
{
 MenuDisplayInfo info;Error error=ReadInfo(deadline,&info);
 if(!error.ok())return error;
 if(info.enabled||!info.quiesced||info.faulted)return Invalid("menu configuration requires a drained disabled display");
 return Command(FesApplicationOpcodeMenuConfigure,0,FesApplicationMenuLayout,deadline);
}
Error MenuDisplayDriver::Enable(std::uint64_t deadline)
{return Command(FesApplicationOpcodeMenuControl,0,FesApplicationMenuControlEnable,deadline);}
Error MenuDisplayDriver::Submit(std::uint8_t slot,std::uint32_t sequence,std::uint64_t deadline)
{
 if(slot>=FesApplicationMenuSlotCount||!sequence)return Invalid("invalid menu slot or sequence");
 Error error=Command(FesApplicationOpcodeMenuSubmit,FesApplicationMenuSubmitSequenceLoIndex,sequence&65535,deadline);
 if(error.ok())error=Command(FesApplicationOpcodeMenuSubmit,FesApplicationMenuSubmitSequenceHiIndex,sequence>>16,deadline);
 if(error.ok())error=Command(FesApplicationOpcodeMenuSubmit,FesApplicationMenuSubmitCommitIndex,slot,deadline);
 return error;
}
Error MenuDisplayDriver::RunBlit(std::uint32_t first,std::uint32_t count,std::uint8_t slot,std::uint64_t deadline)
{
 if(slot>=FesApplicationMenuSlotCount||!count||first>=FesApplicationMenuBlitCommandCount||
  count>FesApplicationMenuBlitCommandCount-first)return Invalid("invalid menu blit list or slot");
 Error error=Command(FesApplicationOpcodeMenuBlit,FesApplicationMenuBlitFirstIndex,std::uint16_t(first),deadline);
 if(error.ok())error=Command(FesApplicationOpcodeMenuBlit,FesApplicationMenuBlitCountIndex,std::uint16_t(count),deadline);
 if(error.ok())error=Command(FesApplicationOpcodeMenuBlit,FesApplicationMenuBlitRunIndex,slot,deadline);
 return error;
}
Error MenuDisplayDriver::ReadBlit(std::uint64_t deadline,MenuBlitState* output)
{
 if(!output)return Invalid("missing menu blit state output");
 // The count advances in the same clock the run goes idle, so reading it
 // first means a new count is never paired with a stale busy.
 std::uint16_t state=0,completed=0;
 Error error=gp_.Exchange(FesApplicationOpcodeMenuBlit,FesApplicationMenuBlitCompletedIndex,0,deadline,&completed);
 if(error.ok())error=gp_.Exchange(FesApplicationOpcodeMenuBlit,FesApplicationMenuBlitStateIndex,0,deadline,&state);
 if(!error.ok())return error;
 constexpr std::uint16_t known=FesApplicationMenuBlitStateBusy|FesApplicationMenuBlitStateFaulted|
  (7u<<FesApplicationMenuBlitStateFaultShift);
 const std::uint8_t fault=std::uint8_t((state>>FesApplicationMenuBlitStateFaultShift)&7u);
 // A fault code appears exactly when the blitter is faulted.
 if((state&~known)||bool(state&FesApplicationMenuBlitStateFaulted)!=(fault!=0))
  return Invalid("menu blit state does not match the blitter contract");
 MenuBlitState blit;
 blit.busy=state&FesApplicationMenuBlitStateBusy;blit.faulted=state&FesApplicationMenuBlitStateFaulted;
 blit.fault=fault;blit.completed=completed;
 *output=blit;return {};
}
Error MenuDisplayDriver::Quiesce(std::uint64_t deadline,const CoreDescriptor* identity,MenuDisplayInfo* output)
{
 // An ambiguous frame submission is never repeated. Sample the live ACK and
 // re-identify the same core before the distinct command that disables it.
 if(identity&&gp_.Poisoned()) {
  Error recovered=gp_.Realign(deadline);
  if(recovered.ok())recovered=gp_.Identify(*identity,deadline);
  if(!recovered.ok())return recovered;
 }
 Error error=Command(FesApplicationOpcodeMenuControl,0,FesApplicationMenuControlQuiesce,deadline);
 if(!error.ok())return error;
 MenuDisplayInfo info;error=ReadInfo(deadline,&info);
 if(!error.ok())return error;
 if(info.enabled||info.pending||!info.quiesced||(!identity&&info.faulted))return Invalid("menu did not prove drained quiescence");
 if(output)*output=info;
 return {};
}
} }
