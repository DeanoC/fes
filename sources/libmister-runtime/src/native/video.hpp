// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"
#include "native/idle_recipe.hpp"
#include "native/platform.hpp"

#include <cstdint>
#include <string>

namespace mister {
namespace native {

class Clock;
class CoreDriver;
class I2c;
struct VideoRecipe;

struct VideoResult {
	Error error;
	std::string phase;
	std::string observed_core;
	std::string selected_bus;
	std::uint8_t power_before = 0;
	std::uint8_t power_after = 0;
	std::uint8_t link_status = 0;
	// ADV7513 0x9e (bit 4 is pixel PLL lock) and 0x3e (the detected VIC in
	// bits 7:2), sampled behind a ready link. Bring-up requires the PLL lock
	// and records the VIC without requiring it.
	std::uint8_t pll_status = 0;
	std::uint8_t vic_status = 0;
	// Which of the three registers answered on the sample being reported.
	// They are separate because each is reported separately and a value that
	// was never read must never be printed as though it had been: the VIC
	// read may fail on its own without touching the PLL verdict, and an
	// unreadable PLL leaves the requirement unenforced rather than refused.
	bool link_read = false;
	bool pll_read = false;
	bool vic_read = false;
};

// The live transmitter state behind a running picture, read without writing
// anything. status is 0x42, pll is 0x9e and vic_raw is 0x3e.
struct VideoLinkHealth {
	std::uint8_t status = 0;
	std::uint8_t pll = 0;
	std::uint8_t vic_raw = 0;
};

struct VideoQuiesceResult {
	Error error;
	bool mutation_attempted = false;
};

class VideoBringup {
public:
	virtual ~VideoBringup() {}
	virtual VideoQuiesceResult Quiesce(
		std::uint64_t absolute_deadline_ms) = 0;
	virtual VideoResult BringUp(const IdleRecipe& idle,
		std::uint64_t absolute_deadline_ms) = 0;
};

// Core video setup follows the selected board's ownership of the transmitter.
class CoreVideoBringup {
public:
 virtual ~CoreVideoBringup() {}
 virtual bool SupportsPlatform(NativePlatform) const = 0;
 virtual VideoResult BringUpCustom(std::uint64_t, bool audio = false) = 0;
 virtual VideoResult BringUpSelected(CoreDriver&, std::uint64_t deadline, bool audio = false)
 { return BringUpCustom(deadline, audio); }
 virtual VideoQuiesceResult Quiesce(std::uint64_t) = 0;
 // Reads the transmitter's live link, PLL lock and detected mode. Boards
 // that do not own the transmitter report unsupported_interface, and the
 // caller then has no transmitter evidence either way.
 virtual Error ReadLinkHealth(std::uint64_t, VideoLinkHealth*)
 { return {ErrorCode::unsupported_interface, "transmitter health is unavailable", "video"}; }
};

class FixedVideoBringup final : public CoreVideoBringup {
public:
	FixedVideoBringup(I2c&, Clock&, LogSink&, const VideoRecipe&);
	bool SupportsPlatform(NativePlatform p) const override { return p == NativePlatform::de10_nano; }
	VideoResult BringUpCustom(std::uint64_t absolute_deadline_ms, bool audio = false) override;
	VideoQuiesceResult Quiesce(std::uint64_t absolute_deadline_ms) override;
	Error ReadLinkHealth(std::uint64_t absolute_deadline_ms, VideoLinkHealth*) override;

private:
	VideoResult PhaseFailure(const char*, const Error&,
		const VideoResult&) const;
	I2c& i2c_;
	Clock& clock_;
	LogSink& log_;
	const VideoRecipe& recipe_;
};

class SplashVideoBringup final : public VideoBringup {
public:
	SplashVideoBringup(I2c&, Clock&, LogSink&,
		const VideoRecipe&);
	VideoQuiesceResult Quiesce(
		std::uint64_t absolute_deadline_ms) override;
	VideoResult BringUp(const IdleRecipe& idle,
		std::uint64_t absolute_deadline_ms) override;

private:
	VideoResult PhaseFailure(const char*, const Error&,
		const VideoResult&) const;
	I2c& i2c_;
	Clock& clock_;
	LogSink& log_;
	const VideoRecipe& recipe_;
};

} // namespace native
} // namespace mister
