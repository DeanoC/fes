// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/core_driver.hpp"

#include <string>

namespace mister {
namespace native {

// Defined startup/Stop idle. Identity, FPGA programming profile, Probe, and
// HPS framebuffer are recipe parameters rather than hardcoded MENU chrome.
// Production uses TransitionalMenuIdle() until a splash bitstream exists.
// Callers must not infer identity from the idle RBF path.
struct IdleRecipe {
	// Empty: idle does not require a probed core identity.
	std::string expected_core;
	ProgrammingProfile programming_profile = ProgrammingProfile::mister_v1;
	// False: skip user-io Probe (command 0x0014). A later splash may have none.
	bool probe_core = true;
	// False: skip HPS framebuffer SPI 0x002f. Idle still succeeds.
	bool enable_hps_framebuffer = false;
};

inline IdleRecipe TransitionalMenuIdle()
{
	IdleRecipe recipe;
	recipe.expected_core = "MENU";
	recipe.programming_profile = ProgrammingProfile::mister_v1;
	recipe.probe_core = true;
	recipe.enable_hps_framebuffer = true;
	return recipe;
}

} // namespace native
} // namespace mister
