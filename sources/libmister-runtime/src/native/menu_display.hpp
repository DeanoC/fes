// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <cstdint>
namespace mister { namespace native {
class FesGp;
class Clock;
struct MenuBlitState {
 bool busy=false,faulted=false;
 std::uint8_t fault=0;
 // Runs finished since reset, wrapping at 16 bits.
 std::uint16_t completed=0;
};
class MenuDisplayDriver {
public:
 MenuDisplayDriver(FesGp&,Clock&);
 Error ReadInfo(std::uint64_t deadline,MenuDisplayInfo*);
 Error Configure(std::uint64_t deadline);
 Error Enable(std::uint64_t deadline);
 Error Submit(std::uint8_t slot,std::uint32_t sequence,std::uint64_t deadline);
 Error Quiesce(std::uint64_t deadline,const CoreDescriptor* identity=nullptr,MenuDisplayInfo* output=nullptr);
 // Starts the blitter on count commands from first into slot. The ACK means
 // started; ReadBlit reports completion.
 Error RunBlit(std::uint32_t first,std::uint32_t count,std::uint8_t slot,std::uint64_t deadline);
 Error ReadBlit(std::uint64_t deadline,MenuBlitState*);
private:
 Error Command(std::uint8_t,std::uint8_t,std::uint16_t,std::uint64_t);
 FesGp& gp_;
};
} }
