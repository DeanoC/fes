// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstdint>
#include <string>

namespace mister {
namespace native {

class Clock;
class Framebuffer;
class CoreLoader;
class I2c;
class Spi;
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
	virtual VideoResult BringUp(const std::string& expected_core,
		std::uint64_t absolute_deadline_ms) = 0;
};

class FixedVideoBringup final {
public:
	FixedVideoBringup(Spi&, I2c&, Clock&, LogSink&, const VideoRecipe&);
	VideoResult BringUp(std::uint64_t absolute_deadline_ms);
	VideoQuiesceResult Quiesce(std::uint64_t absolute_deadline_ms);

private:
	VideoResult PhaseFailure(const char*, const Error&,
		const VideoResult&) const;
	Spi& spi_;
	I2c& i2c_;
	Clock& clock_;
	LogSink& log_;
	const VideoRecipe& recipe_;
};

class MenuVideoBringup final : public VideoBringup {
public:
	MenuVideoBringup(CoreLoader&, Spi&, I2c&, Framebuffer&, Clock&, LogSink&,
		const VideoRecipe&);
	VideoQuiesceResult Quiesce(
		std::uint64_t absolute_deadline_ms) override;
	VideoResult BringUp(const std::string& expected_core,
		std::uint64_t absolute_deadline_ms) override;

private:
	VideoResult PhaseFailure(const char*, const Error&,
		const VideoResult&) const;
	CoreLoader& core_;
	Framebuffer& framebuffer_;
	Spi& spi_;
	I2c& i2c_;
	Clock& clock_;
	LogSink& log_;
	const VideoRecipe& recipe_;
};

} // namespace native
} // namespace mister
