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
// The lifecycle consumes a presentation contract, not a particular register
// transport. Board adapters must report observed completion and quiescence.
class MenuDisplay {
public:
 virtual ~MenuDisplay() {}
 virtual Error ReadInfo(std::uint64_t deadline,MenuDisplayInfo*)=0;
 virtual Error Configure(std::uint64_t deadline)=0;
 virtual Error Enable(std::uint64_t deadline)=0;
 virtual Error Submit(std::uint8_t slot,std::uint32_t sequence,std::uint64_t deadline)=0;
 virtual Error Quiesce(std::uint64_t deadline,const CoreDescriptor* identity=nullptr,MenuDisplayInfo* output=nullptr)=0;
 virtual Error RunBlit(std::uint32_t first,std::uint32_t count,std::uint8_t slot,std::uint64_t deadline)=0;
 virtual Error ReadBlit(std::uint64_t deadline,MenuBlitState*)=0;
};
class MenuDisplayDriver final : public MenuDisplay {
public:
 MenuDisplayDriver(FesGp&,Clock&);
 Error ReadInfo(std::uint64_t deadline,MenuDisplayInfo*) override;
 Error Configure(std::uint64_t deadline) override;
 Error Enable(std::uint64_t deadline) override;
 Error Submit(std::uint8_t slot,std::uint32_t sequence,std::uint64_t deadline) override;
 Error Quiesce(std::uint64_t deadline,const CoreDescriptor* identity=nullptr,MenuDisplayInfo* output=nullptr) override;
 // Starts the blitter on count commands from first into slot. The ACK means
 // started; ReadBlit reports completion.
 Error RunBlit(std::uint32_t first,std::uint32_t count,std::uint8_t slot,std::uint64_t deadline) override;
 Error ReadBlit(std::uint64_t deadline,MenuBlitState*) override;
private:
 Error Command(std::uint8_t,std::uint8_t,std::uint16_t,std::uint64_t);
 FesGp& gp_;
};
} }
