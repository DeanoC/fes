// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/video.hpp"

#include "native/hardware.hpp"
#include "native/linux/i2c.hpp"
#include "native/adv7513.hpp"
#include "native/video_recipe.hpp"

#include <cstdint>
#include <string>
#include <vector>

namespace mister {
namespace native {
namespace {



// ADV7513's EDID request edge wakes the fixed-mode transmitter on a cold
// native start.  The request is deliberately not read back here: the native
// path has no need to select the EDID sub-map, and the transmitter's IRQ
// response is not part of fixed-mode bring-up.
const std::vector<RegisterWrite> kHdmiWake = adv7513::HdmiWake();


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

Error InitializeAdv(I2c& i2c, const VideoRecipe& recipe,
	std::uint64_t deadline, VideoResult* result, bool custom = false)
{
	Error error = i2c.SelectFirst(adv7513::kMainMapAddress7Bit, adv7513::reg::kPower,
		deadline, &result->selected_bus, &result->power_before);
	if (!error.ok()) return error;
	for (const RegisterWrite& write : recipe.adv_initialization) {
		const auto value = custom && write.address == adv7513::reg::kPacketEnable1 ?
			adv7513::kPacketsVideoOnly : write.value;
		error = i2c.WriteByte(write.address, value, deadline);
		if (!error.ok()) return error;
	}
	return i2c.ReadByte(adv7513::reg::kPower, &result->power_after, deadline);
}

VideoQuiesceResult QuiesceAdv(I2c& i2c, Clock& clock,
	std::uint64_t deadline)
{
	if (clock.NowMs() >= deadline)
		return {{ErrorCode::io_failed, "deadline exceeded"}, false};
	std::string selected_bus;
	std::uint8_t power = 0;
	Error error = i2c.SelectFirst(adv7513::kMainMapAddress7Bit,
		adv7513::reg::kPower, deadline, &selected_bus, &power);
	if (!error.ok()) return {error, false};
	// Present a clean link loss before the FPGA changes its pixel stream. The
	// ADV7513 guide documents main power-down for this transition; preserve the
	// other power-register bits exactly as its read-modify-write rule requires.
	error = i2c.WriteByte(adv7513::reg::kPower,
		static_cast<std::uint8_t>(power | adv7513::kPowerPowerDown), deadline);
	return {error, true};
}


Error ApplyFixedMode(I2c& i2c, const VideoRecipe& recipe,
	std::uint64_t deadline)
{
	for (const RegisterWrite& write : recipe.adv_mode) {
		const Error error = i2c.WriteByte(write.address, write.value, deadline);
		if (!error.ok()) return error;
	}
	return {};
}

Error WakeAdv(I2c& i2c, std::uint64_t deadline)
{
	for (const RegisterWrite& write : kHdmiWake) {
		const Error error = i2c.WriteByte(write.address, write.value, deadline);
		if (!error.ok()) return error;
	}
	return {};
}

Error RequireLink(I2c& i2c, Clock& clock, std::uint64_t deadline,
	VideoResult* result)
{
	do {
		if (clock.NowMs() >= deadline)
			return {ErrorCode::io_failed, "deadline exceeded"};
		const Error error = i2c.ReadByte(adv7513::reg::kStatus,
			&result->link_status, deadline);
		if (!error.ok()) return error;
	} while ((result->link_status & adv7513::kStatusLinkReady) !=
		adv7513::kStatusLinkReady);
	return {};
}

void CompleteVideo(const VideoRecipe& recipe, VideoResult* result, LogSink& log)
{
	result->phase = "hdmi_verify";
	result->error = {ErrorCode::none,
		std::string("recipe=") + recipe.identity +
		" bus=" + result->selected_bus +
		" address=" + HexByte(adv7513::kMainMapAddress7Bit) +
		" power_before=" + HexByte(result->power_before) +
		" power_after=" + HexByte(result->power_after) +
		" link_status=" + HexByte(result->link_status)};
	WritePhase(log, "hdmi_verify", *result, result->error);
}

} // namespace

FixedVideoBringup::FixedVideoBringup(I2c& i2c, Clock& clock,
	LogSink& log, const VideoRecipe& recipe)
	: i2c_(i2c), clock_(clock), log_(log), recipe_(recipe) {}

VideoQuiesceResult FixedVideoBringup::Quiesce(std::uint64_t deadline)
{
	return QuiesceAdv(i2c_, clock_, deadline);
}

VideoResult FixedVideoBringup::PhaseFailure(const char* phase,
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


VideoResult FixedVideoBringup::BringUpCustom(std::uint64_t deadline, bool audio)
{
	VideoResult result;
	if (clock_.NowMs() >= deadline)
		return PhaseFailure("hdmi_init",
			{ErrorCode::io_failed, "deadline exceeded"}, result);
	Error error = InitializeAdv(i2c_, recipe_, deadline, &result, true);
	if (!error.ok()) return PhaseFailure("hdmi_init", error, result);
	PhaseSuccess("hdmi_init", &result, log_);
	if (audio) {
		for (const auto& write : adv7513::ApplicationAudio48k()) {
			error = i2c_.WriteByte(write.address, write.value, deadline);
			if (!error.ok()) return PhaseFailure("audio_setup", error, result);
		}
		PhaseSuccess("audio_setup", &result, log_);
	}
	error = ApplyFixedMode(i2c_, recipe_, deadline);
	if (!error.ok()) return PhaseFailure("video_timing", error, result);
	PhaseSuccess("video_timing", &result, log_);
	error = WakeAdv(i2c_, deadline);
	if (!error.ok()) return PhaseFailure("hdmi_wake", error, result);
	PhaseSuccess("hdmi_wake", &result, log_);
	error = RequireLink(i2c_, clock_, deadline, &result);
	if (!error.ok()) return PhaseFailure("hdmi_verify", error, result);
	if (audio) {
		// The identified core remains held and supplies zero samples until Start.
		error = i2c_.WriteByte(adv7513::reg::kPacketEnable1,
			adv7513::kPacketsVideoAudio, deadline);
		if (!error.ok()) return PhaseFailure("audio_enable", error, result);
		PhaseSuccess("audio_enable", &result, log_);
	}
	CompleteVideo(recipe_, &result, log_);
	return result;
}

SplashVideoBringup::SplashVideoBringup(I2c& i2c, Clock& clock,
 LogSink& log, const VideoRecipe& recipe)
 : i2c_(i2c), clock_(clock), log_(log), recipe_(recipe) {}
VideoQuiesceResult SplashVideoBringup::Quiesce(std::uint64_t deadline)
{ return QuiesceAdv(i2c_, clock_, deadline); }
VideoResult SplashVideoBringup::BringUp(const IdleRecipe&, std::uint64_t deadline)
{
 VideoResult result;
 Error error;
 const auto failure = [&](const char* phase) {
  result.phase = phase; result.error = error;
  result.error.code = ErrorCode::io_failed;
  WritePhase(log_, phase, result, result.error); return result;
 };
 if (clock_.NowMs() >= deadline) {
  error = {ErrorCode::io_failed, "deadline exceeded"}; return failure("hdmi_init");
 }
 error = InitializeAdv(i2c_, recipe_, deadline, &result);
 if (!error.ok()) return failure("hdmi_init");
 PhaseSuccess("hdmi_init", &result, log_);
 error = ApplyFixedMode(i2c_, recipe_, deadline);
 if (!error.ok()) return failure("video_timing");
 PhaseSuccess("video_timing", &result, log_);
 error = WakeAdv(i2c_, deadline);
 if (!error.ok()) return failure("hdmi_wake");
 PhaseSuccess("hdmi_wake", &result, log_);
 error = RequireLink(i2c_, clock_, deadline, &result);
 if (!error.ok()) return failure("hdmi_verify");
 CompleteVideo(recipe_, &result, log_); return result;
}
} // namespace native
} // namespace mister
