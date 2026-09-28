// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/menu_display.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/fes_application.hpp"
#include <array>
namespace mister { namespace native {
namespace {
using namespace generated;
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
    geometry.slot_bytes!=FesApplicationMenuSlotBytes||words[7]!=FesApplicationMenuPixelFormat||
    words[8]!=FesApplicationMenuSlotCount||words[9]&~std::uint16_t(31))
  return Invalid("menu geometry or state does not match the supported fixed layout");
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
Error MenuDisplayDriver::Quiesce(std::uint64_t deadline)
{
 Error error=Command(FesApplicationOpcodeMenuControl,0,FesApplicationMenuControlQuiesce,deadline);
 if(!error.ok())return error;
 MenuDisplayInfo info;error=ReadInfo(deadline,&info);
 if(!error.ok())return error;
 if(info.enabled||info.pending||!info.quiesced||info.faulted)return Invalid("menu did not prove drained quiescence");
 return {};
}
} }
