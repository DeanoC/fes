// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/core_package.hpp"

// The union of the interfaces CheckMenu admits for the two explicit boards,
// each at the newest accepted minor. They need not coexist in one package.
// Kit software packing reads them from this
// runtime's .fes_menu_interfaces section and lists them in the bundle manifest
// as menu.<id> keys. An image's supervisor starts that bundle only when they
// cover every interface its sealed menu package declares, so admit and list a
// new menu interface in the same change.
#define MISTER_MENU_INTERFACES(X) \
	X("fes.video.fixed-720p60", 1, 0) \
	X("fes.memory.hps-ddr", 1, 0) \
	X("fes.video.menu-display", 1, 0) \
	X("fes.fabric.de25-scanout", 1, 1) \
	X("fes.video.menu-blitter", 1, 1)

namespace mister {
namespace native {

// Minor 1 adds EXPAND, the first menu-blitter version suitable for grants.
constexpr std::uint16_t kMenuBlitterMinimumMinor = 1;

// MISTER_MENU_INTERFACES as consecutive NUL-terminated "id=major.minor"
// records, ending with an empty record.
extern const char kMenuInterfaceRecords[];

// Admission of the described idle menu package.
Error CheckMenu(const CoreDescriptor& descriptor,
	NativePlatform platform = NativePlatform::de10_nano);

} // namespace native
} // namespace mister
