// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <cstdint>
namespace mister {
namespace native {
// One 720p scanline. fes_menu_video counts one underflow per missing active
// pixel at 74.25 MHz, so 1280 pixels is about 17 microseconds of DDR gap.
// The frame is still displayed. A larger hole fails that present.
// FogCast ui/menudisplay.TransientUnderflowCap must stay equal to this.
constexpr std::uint32_t kMenuUnderflowPresentCap=1280;
// Three presents is 50 ms at 60 Hz: one glitch is ignored, a stuck reader is not.
constexpr unsigned kMenuUnderflowSustainPresents=3;
// Programming reloads the bitstream (the counter's initial value is 0) and
// port reset clears it again. Two such reactivations per 10 minutes, then splash.
constexpr unsigned kMenuReactivationLimit=2;
constexpr std::uint64_t kMenuReactivationWindowMs=10ull*60ull*1000ull;
}
}
