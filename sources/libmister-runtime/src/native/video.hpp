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
 virtual VideoQuiesceResult Quiesce(std::uint64_t) = 0;
};

class FixedVideoBringup final : public CoreVideoBringup {
public:
	FixedVideoBringup(I2c&, Clock&, LogSink&, const VideoRecipe&);
	bool SupportsPlatform(NativePlatform p) const override { return p == NativePlatform::de10_nano; }
	VideoResult BringUpCustom(std::uint64_t absolute_deadline_ms, bool audio = false) override;
	VideoQuiesceResult Quiesce(std::uint64_t absolute_deadline_ms) override;

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
