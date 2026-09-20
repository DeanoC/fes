// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#ifndef MISTER_VIDEO_RECIPE_HPP
#define MISTER_VIDEO_RECIPE_HPP

#include <cstdint>
#include <vector>

namespace mister { namespace native {

struct RegisterWrite {
	std::uint8_t address;
	std::uint8_t value;
};

struct VideoRecipe {
	const char* identity;
	const char* source_commit;
	const std::vector<std::uint16_t>& timing_words;
	const std::vector<RegisterWrite>& adv_initialization;
	const std::vector<RegisterWrite>& adv_mode;
};

const VideoRecipe& Menu720p60Recipe();

} }

#endif
