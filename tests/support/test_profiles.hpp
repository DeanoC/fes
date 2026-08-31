// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

namespace mister_test {

inline mister::Profile CartProfile()
{
	mister::Profile profile;
	profile.system = "test_cart";
	profile.expected_core = "TESTCART";
	profile.media.push_back({"cartridge", 1, true});
	profile.settings.push_back({"region", {"auto", "pal", "ntsc"}});
	return profile;
}

inline mister::Profile BiosProfile()
{
	mister::Profile profile;
	profile.system = "test_bios";
	profile.expected_core = "TESTBIOS";
	profile.media.push_back({"bios", 0, true});
	profile.media.push_back({"cartridge", 2, true});
	return profile;
}

} // namespace mister_test
