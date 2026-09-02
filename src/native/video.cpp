// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/video.hpp"

#include "native/core_loader.hpp"
#include "native/hardware.hpp"
#include "native/linux/i2c.hpp"
#include "native/linux/spi.hpp"
#include "native/video_recipe.hpp"

#include <cstdint>
#include <string>
#include <vector>

namespace mister {
namespace native {
namespace {

const std::vector<std::uint16_t> kAssertedStatus = {
	0x001e, 0x0001, 0x0000, 0x0000, 0x0000,
	0x0000, 0x0000, 0x0000, 0x0000,
};

const std::vector<std::uint16_t> kReleasedStatus = {
	0x001e, 0x0000, 0x0000, 0x0000, 0x0000,
	0x0000, 0x0000, 0x0000, 0x0000,
};

// ADV7513's EDID request edge wakes the fixed-mode transmitter on a cold
// native start.  The request is deliberately not read back here: the native
// path has no need to select the EDID sub-map, and the transmitter's IRQ
// response is not part of fixed-mode bring-up.
const std::vector<RegisterWrite> kHdmiWake = {
	{0x96, 0x04}, {0xc4, 0x00}, {0xc9, 0x03},
	{0xc9, 0x13}, {0xc9, 0x03},
};

const std::vector<std::uint16_t> kNeutralButtons = {0x0001, 0x0000};

std::string HexByte(std::uint8_t value)
{
	static const char digits[] = "0123456789abcdef";
	std::string output = "0x00";
	output[2] = digits[(value >> 4) & 0x0f];
	output[3] = digits[value & 0x0f];
	return output;
}

void WritePhase(LogSink& log, const char* phase, const VideoResult& result,
	const Error& error)
{
	log.Write({"start", "", result.observed_core, phase, error});
}

void PhaseSuccess(const char* phase, VideoResult* result, LogSink& log)
{
	result->phase = phase;
	result->error = {};
	WritePhase(log, phase, *result, result->error);
}

} // namespace

MenuVideoBringup::MenuVideoBringup(CoreLoader& core, Spi& spi, I2c& i2c,
	Clock& clock, LogSink& log, const VideoRecipe& recipe)
	: core_(core), spi_(spi), i2c_(i2c), clock_(clock), log_(log),
	  recipe_(recipe) {}

VideoResult MenuVideoBringup::PhaseFailure(const char* phase,
	const Error& cause, const VideoResult& partial) const
{
	VideoResult result = partial;
	result.phase = phase;
	result.error.code = ErrorCode::io_failed;
	result.error.message = cause.message.empty() ?
		std::string(phase) + " failed" : cause.message;
	WritePhase(log_, phase, result, result.error);
	return result;
}

VideoResult MenuVideoBringup::BringUp(const std::string& expected_core,
	std::uint64_t deadline)
{
	VideoResult result;
	if (clock_.NowMs() >= deadline)
		return PhaseFailure("core_reset",
			{ErrorCode::io_failed, "deadline exceeded"}, result);

	Error error = spi_.SynchronizeCore(deadline);
	if (!error.ok()) return PhaseFailure("core_sync", error, result);
	PhaseSuccess("core_sync", &result, log_);

	error = spi_.Exchange(kUserIoTarget, kAssertedStatus, nullptr, deadline);
	if (!error.ok()) return PhaseFailure("core_reset", error, result);
	PhaseSuccess("core_reset", &result, log_);

	error = core_.Probe(&result.observed_core, deadline);
	if (!error.ok()) return PhaseFailure("core_probe", error, result);
	if (result.observed_core != expected_core || expected_core != "MENU")
		return PhaseFailure("core_probe",
			{ErrorCode::io_failed, "unexpected menu core"}, result);
	PhaseSuccess("core_probe", &result, log_);

	error = i2c_.SelectFirst(0x39, 0x41, deadline, &result.selected_bus,
		&result.power_before);
	if (!error.ok()) return PhaseFailure("hdmi_init", error, result);
	for (const RegisterWrite& write : recipe_.adv_initialization) {
		error = i2c_.WriteByte(write.address, write.value, deadline);
		if (!error.ok()) return PhaseFailure("hdmi_init", error, result);
	}
	error = i2c_.ReadByte(0x41, &result.power_after, deadline);
	if (!error.ok()) return PhaseFailure("hdmi_init", error, result);
	PhaseSuccess("hdmi_init", &result, log_);

	error = spi_.Exchange(kUserIoTarget, recipe_.timing_words, nullptr, deadline);
	if (!error.ok()) return PhaseFailure("video_timing", error, result);
	for (const RegisterWrite& write : recipe_.adv_mode) {
		error = i2c_.WriteByte(write.address, write.value, deadline);
		if (!error.ok()) return PhaseFailure("video_timing", error, result);
	}
	PhaseSuccess("video_timing", &result, log_);

	error = spi_.Exchange(kUserIoTarget, kReleasedStatus, nullptr, deadline);
	if (!error.ok()) return PhaseFailure("core_release", error, result);
	PhaseSuccess("core_release", &result, log_);

	for (const RegisterWrite& write : kHdmiWake) {
		error = i2c_.WriteByte(write.address, write.value, deadline);
		if (!error.ok()) return PhaseFailure("hdmi_wake", error, result);
	}
	PhaseSuccess("hdmi_wake", &result, log_);

	error = spi_.Exchange(kUserIoTarget, kNeutralButtons, nullptr, deadline);
	if (!error.ok()) return PhaseFailure("core_input", error, result);
	PhaseSuccess("core_input", &result, log_);

	do {
		if (clock_.NowMs() >= deadline)
			return PhaseFailure("hdmi_verify",
				{ErrorCode::io_failed, "deadline exceeded"}, result);
		error = i2c_.ReadByte(0x42, &result.link_status, deadline);
		if (!error.ok()) return PhaseFailure("hdmi_verify", error, result);
	} while ((result.link_status & 0x60) != 0x60);

	result.phase = "hdmi_verify";
	result.error = {ErrorCode::none,
		std::string("recipe=") + recipe_.identity +
		" bus=" + result.selected_bus +
		" address=0x39 power_before=" + HexByte(result.power_before) +
		" power_after=" + HexByte(result.power_after) +
		" link_status=" + HexByte(result.link_status)};
	WritePhase(log_, "hdmi_verify", result, result.error);
	return result;
}

} // namespace native
} // namespace mister
