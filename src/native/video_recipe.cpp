// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/video_recipe.hpp"

#include "native/adv7513.hpp"

namespace mister { namespace native {

const VideoRecipe& Menu720p60Recipe()
{
	static const std::vector<std::uint16_t> timing = {
		0x0020,
		0x0500, 0x006e, 0x0028, 0x00dc,
		0x02d0, 0x0005, 0x0005, 0x0014,
		0x4004, 0x0404, 0x0000,
		0x4003, 0x0000, 0x0001,
		0x4005, 0x0303, 0x0000,
		0x4009, 0x0002, 0x0000,
		0x4008, 0x0007, 0x0000,
		0x4007, 0xc28f, 0xe8f5,
	};
	static const std::vector<RegisterWrite> initialization =
		adv7513::Menu720p60Initialization();
	static const std::vector<RegisterWrite> mode = adv7513::Menu720p60Mode();
	static const VideoRecipe recipe = {
		"menu_720p60",
		"cc5eb4bfc4cb2887dd6ab8364bff7010d7c6978c",
		timing,
		initialization,
		mode,
	};
	return recipe;
}

} }
