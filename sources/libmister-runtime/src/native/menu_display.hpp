// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <cstdint>
namespace mister { namespace native {
class FesGp;
class Clock;
struct MenuGeometry {
 std::uint32_t width=0,height=0,stride=0,frame_bytes=0,slot_bytes=0;
};
struct MenuDisplayInfo {
 MenuGeometry geometry;
 bool configured=false,enabled=false,pending=false,quiesced=false,faulted=false;
 std::uint32_t displayed_sequence=0,underflows=0;
};
class MenuDisplayDriver {
public:
 MenuDisplayDriver(FesGp&,Clock&);
 Error ReadInfo(std::uint64_t deadline,MenuDisplayInfo*);
 Error Configure(std::uint64_t deadline);
 Error Enable(std::uint64_t deadline);
 Error Submit(std::uint8_t slot,std::uint32_t sequence,std::uint64_t deadline);
 Error Quiesce(std::uint64_t deadline);
private:
 Error Command(std::uint8_t,std::uint8_t,std::uint16_t,std::uint64_t);
 FesGp& gp_;
};
} }
