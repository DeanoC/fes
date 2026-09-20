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
	profile.rbf = "/cores/test.rbf";
	profile.media.push_back({"cartridge", 1, true, {".bin"},
		32u * 1024u * 1024u});
	profile.settings.push_back({"region", {"auto", "pal", "ntsc"}});
	profile.core = {0x0001, 0x0001, 0x0000,
		mister::FileWireFormat::little_endian_byte_pairs};
	profile.input = {1, 0x02, 0x0008, 0x0004, 0x0002, 0x0001,
		0x0010, 0x0020, 0x0040, 0x0080};
	return profile;
}

inline mister::Profile BiosProfile()
{
	mister::Profile profile;
	profile.system = "test_bios";
	profile.expected_core = "TESTBIOS";
	profile.rbf = "/cores/test_bios.rbf";
	profile.media.push_back({"bios", 0, true, {".bin"},
		32u * 1024u * 1024u});
	profile.media.push_back({"cartridge", 2, true, {".bin"},
		32u * 1024u * 1024u});
	profile.core = {0x0001, 0x0001, 0x0000,
		mister::FileWireFormat::little_endian_byte_pairs};
	profile.input = {1, 0x02, 0x0008, 0x0004, 0x0002, 0x0001,
		0x0010, 0x0020, 0x0040, 0x0080};
	return profile;
}

} // namespace mister_test
